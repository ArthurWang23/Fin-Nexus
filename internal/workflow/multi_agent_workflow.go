package workflow

import (
	"fmt"
	"go-nexus/internal/domain"
	"go-nexus/internal/usecase"
	"go-nexus/internal/workflow/activities"
	"time"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

const SignalApprove = "APPROVE_SIGNAL"

// ApprovalSignal is the payload sent by the frontend when approving/rejecting code execution.
type ApprovalSignal struct {
	Approved     bool   `json:"approved"`
	Reason       string `json:"reason"`
	ModifiedCode string `json:"modified_code,omitempty"` // user may edit the code before approving
}

func MultiAgentWorkflow(ctx workflow.Context, userQuery string, userID string) (string, error) {
	ao := workflow.ActivityOptions{
		StartToCloseTimeout: 2 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval: time.Second,
			MaximumAttempts: 3,
		},
	}
	ctx = workflow.WithActivityOptions(ctx, ao)

	maxSteps := 5
	history := []domain.Message{}
	for i := 0; i < maxSteps; i++ {
		var decision usecase.SupervisorDecision
		input := activities.SupervisorInput{
			Query:   userQuery,
			History: history,
			UserID:  userID,
		}
		err := workflow.ExecuteActivity(ctx, "SupervisorDecide", input).Get(ctx, &decision)
		if err != nil {
			return "", err
		}
		if decision.NextAgent == "FINISH" {
			return decision.FinalAnswer, nil
		}

		var workerResult string
		switch decision.NextAgent {
		case "Researcher":
			err = workflow.ExecuteActivity(ctx, "ResearcherSearch", decision.Instruction, userID).Get(ctx, &workerResult)
		case "Coder":
			workflow.GetLogger(ctx).Info("Code execution requested. Waiting for approval...", "code", decision.Instruction)
			approvalCh := workflow.GetSignalChannel(ctx, SignalApprove)
			var signal ApprovalSignal
			approvalCh.Receive(ctx, &signal)
			if signal.Approved {
				err = workflow.ExecuteActivity(ctx, "CoderRun", decision.Instruction, userID).Get(ctx, &workerResult)
			} else {
				workerResult = "User REJECTED the code execution request. Reason: " + signal.Reason
			}
		}
		if err != nil {
			workerResult = "Error: " + err.Error()
		}
		history = append(history, domain.Message{
			Role:    domain.RoleUser,
			Content: "【" + decision.NextAgent + "汇报】: " + workerResult,
		})
	}
	return "任务超时，步骤过多", nil
}

func StreamMultiAgentWorkflow(ctx workflow.Context, userQuery string, streamID string, sessionID string, userID string) (string, error) {
	ao := workflow.ActivityOptions{
		StartToCloseTimeout: time.Minute * 5,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval: time.Second,
			MaximumAttempts: 3,
		},
	}
	ctx = workflow.WithActivityOptions(ctx, ao)

	// --- Workflow Versioning ---
	// Version "coder-approval-gate": DefaultVersion = no approval, 1 = with approval
	// This allows safe hot-deployment: running workflows continue with old logic,
	// new workflows get the approval gate.
	approvalVersion := workflow.GetVersion(ctx, "coder-approval-gate", workflow.DefaultVersion, 1)

	var contextHistory []domain.Message
	if sessionID != "" {
		err := workflow.ExecuteActivity(ctx, "LoadChatHistory", sessionID).Get(ctx, &contextHistory)
		if err != nil {
			workflow.GetLogger(ctx).Error("Failed to load chat history", "error", err)
			contextHistory = []domain.Message{}
		}
	}

	executionSteps := []domain.Message{}
	var finalAnswer string
	maxSteps := 20
	for i := 0; i < maxSteps; i++ {
		fullHistory := append([]domain.Message{}, contextHistory...)
		fullHistory = append(fullHistory, domain.Message{
			Role:    domain.RoleUser,
			Content: userQuery,
		})
		fullHistory = append(fullHistory, executionSteps...)

		var decision usecase.SupervisorDecision
		input := activities.SupervisorInput{
			Query:   userQuery,
			History: fullHistory,
			UserID:  userID,
		}
		err := workflow.ExecuteActivity(ctx, "SupervisorDecideStream", input, streamID).Get(ctx, &decision)
		if err != nil {
			return "", err
		}
		if decision.NextAgent == "FINISH" {
			finalAnswer = decision.FinalAnswer
			promptForFinal := append(fullHistory, domain.Message{
				Role:    domain.RoleSystem,
				Content: fmt.Sprintf("任务已完成。主管的总结思路是: %s。请据此给用户一个完整、友好的最终回复。重要：如果对话历史中包含 Markdown 图片标签（如 ![描述](/images/xxx.png)），你必须在回复中原样保留这些图片标签，不要将其转为纯文本。", decision.FinalAnswer),
			})

			err := workflow.ExecuteActivity(ctx, "FinalReplyStream", promptForFinal, streamID, userID).Get(ctx, &finalAnswer)
			if sessionID != "" && err == nil {
				saveInput := activities.SaveChatInput{
					SessionID:   sessionID,
					UserID:      userID,
					UserQuery:   userQuery,
					FinalAnswer: finalAnswer,
				}
				_ = workflow.ExecuteActivity(ctx, "SaveChatTurn", saveInput).Get(ctx, nil)
			}
			return finalAnswer, err
		}

		var workerResult string

		// --- Human-in-the-loop for Coder: generate code → review → execute ---
		if decision.NextAgent == "Coder" && approvalVersion >= 1 {
			// Phase 1: Ask LLM to generate code (no execution yet)
			_ = workflow.ExecuteActivity(ctx, "PublishStreamEvent", streamID, "step",
				fmt.Sprintf("Coder 正在生成代码: %s", decision.Instruction)).Get(ctx, nil)

			var generatedCode string
			err = workflow.ExecuteActivity(ctx, "CoderGenerateCode", decision.Instruction, userID).Get(ctx, &generatedCode)
			if err != nil {
				workerResult = fmt.Sprintf("Code generation failed: %v", err)
			} else if generatedCode == "" {
				_ = workflow.ExecuteActivity(ctx, "PublishStreamEvent", streamID, "step", "Coder 认为此任务无需编写代码").Get(ctx, nil)
				workerResult = "No code needed for this task."
			} else {
				// Phase 2: Send actual code to frontend for review
				_ = workflow.ExecuteActivity(ctx, "PublishStreamEvent", streamID, "approval_required", generatedCode).Get(ctx, nil)

				workflow.GetLogger(ctx).Info("Waiting for user to review generated code...")
				approvalCh := workflow.GetSignalChannel(ctx, SignalApprove)
				var signal ApprovalSignal
				approvalCh.Receive(ctx, &signal)

				if signal.Approved {
					codeToRun := generatedCode
					if signal.ModifiedCode != "" {
						codeToRun = signal.ModifiedCode
						_ = workflow.ExecuteActivity(ctx, "PublishStreamEvent", streamID, "step", "✅ 用户已批准执行（使用修改后的代码）").Get(ctx, nil)
					} else {
						_ = workflow.ExecuteActivity(ctx, "PublishStreamEvent", streamID, "step", "✅ 用户已批准代码执行").Get(ctx, nil)
					}
					// Phase 3: Execute the approved code
					err = workflow.ExecuteActivity(ctx, "CoderExecuteCode", codeToRun).Get(ctx, &workerResult)
				} else {
					rejectMsg := "用户拒绝了代码执行"
					if signal.Reason != "" {
						rejectMsg += "，理由: " + signal.Reason
					}
					_ = workflow.ExecuteActivity(ctx, "PublishStreamEvent", streamID, "step", "❌ "+rejectMsg).Get(ctx, nil)
					workerResult = "User REJECTED: " + rejectMsg
				}
			}
		} else {
			err = workflow.ExecuteActivity(ctx, "WorkerRunStream", decision.NextAgent, decision.Instruction, streamID, userID).Get(ctx, &workerResult)
		}

		if err != nil {
			workerResult = fmt.Sprintf("Error executing task: %v", err)
		}
		executionSteps = append(executionSteps, domain.Message{
			Role:    domain.RoleUser,
			Content: fmt.Sprintf("【%s 汇报】:\n%s", decision.NextAgent, workerResult),
		})
	}

	return "任务执行步骤过多，已被系统强制终止。", nil
}
