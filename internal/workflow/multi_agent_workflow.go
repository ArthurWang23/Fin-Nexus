package workflow

import (
	"go-nexus/internal/domain"
	"go-nexus/internal/usecase"
	"go-nexus/internal/workflow/activities"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
	"time"
)

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
			err = workflow.ExecuteActivity(ctx, "CoderRun", decision.Instruction).Get(ctx, &workerResult)
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
