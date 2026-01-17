package workflow

import (
	"go-nexus/internal/domain"
	"go-nexus/internal/usecase"
	"go-nexus/internal/workflow/activities"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
	"time"
)

const SignalApprove = "APPROVE_SIGNAL"

func MultiAgentWorkflow(ctx workflow.Context, userQuery string) (string, error) {
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
		}
		// ExecuteActivity 替代了普通的函数调用
		// Temporal 会记录这一步有没有完成。如果 Crash 了，重启后会直接跳过已完成的步骤。
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
			err = workflow.ExecuteActivity(ctx, "ResearcherSearch", decision.Instruction).Get(ctx, &workerResult)
		case "Coder":
			workflow.GetLogger(ctx).Info(" Code execution requested. Waiting for approval...", "code", decision.Instruction)
			selector := workflow.NewSelector(ctx)
			approverSignal := workflow.GetSignalChannel(ctx, SignalApprove)

			var approved bool
			var manualFeedback string // 允许管理员拒绝时附带理由

			selector.AddReceive(approverSignal, func(c workflow.ReceiveChannel, more bool) {
				var signalVal struct {
					Approved bool
					Reason   string
				}
				c.Receive(ctx, &signalVal)
				approved = signalVal.Approved
				manualFeedback = signalVal.Reason
			})

			selector.Select(ctx)
			if approved {
				err = workflow.ExecuteActivity(ctx, "CoderRun", decision.Instruction).Get(ctx, &workerResult)
			} else {
				workerResult = "User REJECTED the code execution request. Reason: " + manualFeedback
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
