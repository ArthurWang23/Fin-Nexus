package workflow

import (
	"go.temporal.io/sdk/workflow"
	"time"
)

// 永不丢失状态的对话流程
func DurableChatWorkflow(ctx workflow.Context, userQuery string) (string, error) {
	options := workflow.ActivityOptions{
		StartToCloseTimeout: time.Minute * 2, // 如果2分钟没反应，自动重试
	}
	ctx = workflow.WithActivityOptions(ctx, options)
	var result string
	err := workflow.ExecuteActivity(ctx, "LLMActivity.Chat", userQuery).Get(ctx, &result)
	return result, err
}
