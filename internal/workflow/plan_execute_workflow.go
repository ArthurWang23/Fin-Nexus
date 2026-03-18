package workflow

import (
	"encoding/json"
	"fmt"
	"go-nexus/internal/domain"
	"go-nexus/internal/usecase"
	"go-nexus/internal/workflow/activities"
	"time"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

// PlanExecuteWorkflow implements the Plan-Execute pattern:
//
//	Phase 1: One LLM call generates a complete execution plan
//	Phase 2: Steps are executed in parallel where dependencies allow
//	Phase 3: Results are aggregated into a final reply
func PlanExecuteWorkflow(ctx workflow.Context, userQuery string, streamID string, sessionID string, userID string) (string, error) {
	ao := workflow.ActivityOptions{
		StartToCloseTimeout: 5 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval: time.Second,
			MaximumAttempts: 3,
		},
	}
	ctx = workflow.WithActivityOptions(ctx, ao)

	// ── Load chat history ──
	var contextHistory []domain.Message
	if sessionID != "" {
		err := workflow.ExecuteActivity(ctx, "LoadChatHistory", sessionID).Get(ctx, &contextHistory)
		if err != nil {
			workflow.GetLogger(ctx).Error("Failed to load history", "error", err)
			contextHistory = []domain.Message{}
		}
	}

	fullHistory := append([]domain.Message{}, contextHistory...)
	fullHistory = append(fullHistory, domain.Message{Role: domain.RoleUser, Content: userQuery})

	// ── Phase 1: Plan ──
	input := activities.SupervisorInput{Query: userQuery, History: fullHistory, UserID: userID}
	var plan usecase.ExecutionPlan
	if err := workflow.ExecuteActivity(ctx, "PlannerDecide", input, streamID).Get(ctx, &plan); err != nil {
		return "", fmt.Errorf("planning failed: %w", err)
	}

	// Handle direct reply (chitchat, no agents needed)
	if plan.DirectReply != "" && len(plan.Steps) == 0 {
		_ = publishEvent(ctx, streamID, "step", "直接回复（无需调度下属）")
		promptForFinal := append(fullHistory, domain.Message{
			Role:    domain.RoleSystem,
			Content: fmt.Sprintf("请直接回复用户: %s", plan.DirectReply),
		})
		var finalAnswer string
		err := workflow.ExecuteActivity(ctx, "FinalReplyStream", promptForFinal, streamID, userID).Get(ctx, &finalAnswer)
		saveTurn(ctx, sessionID, userID, userQuery, finalAnswer)
		return finalAnswer, err
	}

	// Publish the plan to frontend
	planJSON, _ := json.Marshal(plan)
	_ = publishEvent(ctx, streamID, string(usecase.EventPlan), string(planJSON))
	_ = publishEvent(ctx, streamID, "step", fmt.Sprintf("📋 执行计划已生成，共 %d 个步骤", len(plan.Steps)))

	// ── Phase 2: Execute (parallel where possible) ──
	results := make(map[int]string) // stepID → result

	// Build levels: group steps by execution order based on dependencies
	levels := buildLevels(plan.Steps)

	for levelIdx, level := range levels {
		workflow.GetLogger(ctx).Info("Executing level", "level", levelIdx, "steps", len(level))

		// Mark all steps in this level as "running"
		for _, step := range level {
			_ = publishEvent(ctx, streamID, string(usecase.EventStepComplete),
				fmt.Sprintf(`{"id":%d,"status":"running"}`, step.ID))
		}

		// Launch all steps in this level concurrently
		futures := make(map[int]workflow.Future)
		for _, step := range level {
			s := step // capture
			_ = publishEvent(ctx, streamID, "step",
				fmt.Sprintf("▶ Step %d [%s]: %s", s.ID, s.Agent, s.Instruction))

			switch s.Agent {
			case "Coder":
				futures[s.ID] = workflow.ExecuteActivity(ctx, "CoderGenerateCode", s.Instruction, userID)
			case "Researcher":
				futures[s.ID] = workflow.ExecuteActivity(ctx, "WorkerRunStream", "Researcher", s.Instruction, streamID, userID)
			default:
				_ = publishEvent(ctx, streamID, "step", fmt.Sprintf("⚠ Unknown agent: %s", s.Agent))
			}
		}

		// Collect results: non-Coder steps first (they never block on user input),
		// then Coder steps (which may wait for human approval).
		// This prevents Coder approval from blocking the collection of already-finished results.
		var coderSteps []usecase.PlanStep
		for _, step := range level {
			if step.Agent == "Coder" {
				coderSteps = append(coderSteps, step)
				continue
			}
			f, ok := futures[step.ID]
			if !ok {
				continue
			}
			stepStatus := "done"
			var result string
			if err := f.Get(ctx, &result); err != nil {
				results[step.ID] = fmt.Sprintf("Error: %v", err)
				stepStatus = "error"
			} else {
				results[step.ID] = result
			}
			_ = publishEvent(ctx, streamID, string(usecase.EventStepComplete),
				fmt.Sprintf(`{"id":%d,"status":"%s"}`, step.ID, stepStatus))
		}

		for _, step := range coderSteps {
			f, ok := futures[step.ID]
			if !ok {
				continue
			}
			stepStatus := "done"
			var generatedCode string
			if err := f.Get(ctx, &generatedCode); err != nil {
				results[step.ID] = fmt.Sprintf("Code generation failed: %v", err)
				stepStatus = "error"
			} else if generatedCode == "" {
				results[step.ID] = "No code needed."
			} else {
				_ = publishEvent(ctx, streamID, "approval_required", generatedCode)
				approvalCh := workflow.GetSignalChannel(ctx, SignalApprove)
				var signal ApprovalSignal
				approvalCh.Receive(ctx, &signal)

				if signal.Approved {
					codeToRun := generatedCode
					if signal.ModifiedCode != "" {
						codeToRun = signal.ModifiedCode
					}
					_ = publishEvent(ctx, streamID, "step", "✅ 代码已批准，执行中...")
					var execResult string
					if err := workflow.ExecuteActivity(ctx, "CoderExecuteCode", codeToRun).Get(ctx, &execResult); err != nil {
						results[step.ID] = fmt.Sprintf("Execution failed: %v", err)
						stepStatus = "error"
					} else {
						results[step.ID] = execResult
					}
				} else {
					results[step.ID] = "User REJECTED: " + signal.Reason
					_ = publishEvent(ctx, streamID, "step", "❌ 用户拒绝了代码执行")
					stepStatus = "error"
				}
			}
			_ = publishEvent(ctx, streamID, string(usecase.EventStepComplete),
				fmt.Sprintf(`{"id":%d,"status":"%s"}`, step.ID, stepStatus))
		}
	}

	// ── Phase 3: Final summary ──
	var summaryHistory []domain.Message
	summaryHistory = append(summaryHistory, fullHistory...)
	for _, step := range plan.Steps {
		summaryHistory = append(summaryHistory, domain.Message{
			Role:    domain.RoleUser,
			Content: fmt.Sprintf("【%s 汇报 (Step %d)】:\n%s", step.Agent, step.ID, results[step.ID]),
		})
	}
	summaryHistory = append(summaryHistory, domain.Message{
		Role:    domain.RoleSystem,
		Content: "所有步骤已完成。请综合以上所有汇报，给用户一个完整、友好的最终回复。重要：如果汇报中包含 Markdown 图片标签（如 ![描述](/images/xxx.png)），你必须在回复中原样保留这些图片标签。",
	})

	var finalAnswer string
	err := workflow.ExecuteActivity(ctx, "FinalReplyStream", summaryHistory, streamID, userID).Get(ctx, &finalAnswer)
	saveTurn(ctx, sessionID, userID, userQuery, finalAnswer)
	return finalAnswer, err
}

// buildLevels groups plan steps into execution levels based on dependencies.
// Steps in the same level have no mutual dependencies and can run in parallel.
// Uses slice index (not step ID) to track progress, preventing infinite loops
// when duplicate step IDs are present.
func buildLevels(steps []usecase.PlanStep) [][]usecase.PlanStep {
	if len(steps) == 0 {
		return nil
	}

	n := len(steps)
	placedIdx := make([]bool, n)       // tracks by slice index
	completedIDs := make(map[int]bool) // tracks completed step IDs for dependency resolution
	var levels [][]usecase.PlanStep
	totalPlaced := 0

	for totalPlaced < n {
		var level []usecase.PlanStep
		var levelIdxs []int
		for i, s := range steps {
			if placedIdx[i] {
				continue
			}
			ready := true
			for _, dep := range s.DependsOn {
				if !completedIDs[dep] {
					ready = false
					break
				}
			}
			if ready {
				level = append(level, s)
				levelIdxs = append(levelIdxs, i)
			}
		}
		if len(level) == 0 {
			for i, s := range steps {
				if !placedIdx[i] {
					level = append(level, s)
					levelIdxs = append(levelIdxs, i)
				}
			}
		}
		levels = append(levels, level)
		for _, idx := range levelIdxs {
			placedIdx[idx] = true
			completedIDs[steps[idx].ID] = true
		}
		totalPlaced += len(levelIdxs)
	}
	return levels
}

func publishEvent(ctx workflow.Context, streamID, eventType, content string) error {
	return workflow.ExecuteActivity(ctx, "PublishStreamEvent", streamID, eventType, content).Get(ctx, nil)
}

func saveTurn(ctx workflow.Context, sessionID, userID, query, answer string) {
	if sessionID == "" || answer == "" {
		return
	}
	saveInput := activities.SaveChatInput{
		SessionID:   sessionID,
		UserID:      userID,
		UserQuery:   query,
		FinalAnswer: answer,
	}
	_ = workflow.ExecuteActivity(ctx, "SaveChatTurn", saveInput).Get(ctx, nil)
}
