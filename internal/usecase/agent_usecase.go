package usecase

import (
	"context"
	"encoding/json"
	"fmt"
	"go-nexus/internal/domain"
	"go-nexus/internal/usecase/gateway"
	"go-nexus/internal/usecase/tools"
	"go.opentelemetry.io/otel"
)

// Reasoning + Acting 循环
type AgentUseCase struct {
	llm gateway.LLMClient
}

func NewAgentUseCase(llm gateway.LLMClient) *AgentUseCase {
	return &AgentUseCase{llm: llm}
}

func (uc *AgentUseCase) ChatWithAgent(ctx context.Context, userQuery string) (string, error) {
	tracer := otel.Tracer("go-nexus")
	ctx, span := tracer.Start(ctx, "AgentUseCase.ChatWithAgent")
	defer span.End()

	toolDefs := []gateway.ToolDefinition{
		{
			Name:        "get_order_info",
			Description: "查询电商订单的物流状态和详情",
			Parameters:  tools.GetOrderToolSchema(),
		},
	}

	history := []domain.Message{
		{Role: domain.RoleSystem, Content: "你是一个电商智能助手。如果用户查询订单，请调用工具。其他问题直接回答。"},
		{Role: domain.RoleUser, Content: userQuery},
	}

	fmt.Println("Agent: Thinking...")
	_, spanLLM := tracer.Start(ctx, "LLM.Thinking")
	resp, err := uc.llm.ChatWithTools(ctx, history, toolDefs)
	spanLLM.End()
	if err != nil {
		return "", err
	}

	// 没有处罚工具
	if len(resp.ToolCalls) == 0 {
		return resp.Content, nil
	}

	// 模拟为了保持上下文，我们需要把 LLM 的这次回复(包含ToolCall)加入历史
	// 这里简化处理：我们不把复杂的 ToolCall 结构回传，而是直接进入下一轮观察
	for _, call := range resp.ToolCalls {
		if call.Name == "get_order_info" {
			_, spanTool := tracer.Start(ctx, "Tool.GetOrderInfo")
			var args struct {
				OrderID string `json:"order_id"`
			}
			if err := json.Unmarshal([]byte(call.Args), &args); err != nil {
				return "", fmt.Errorf("invalid tool args: %v", err)
			}
			fmt.Printf("Agent: Calling Tool [GetOrderInfo] with ID: %s\n", args.OrderID)
			toolResult := tools.GetOrderInfo(args.OrderID)
			spanTool.End()
			// 将工具结果作为一条新消息告诉 LLM
			// 提示词工程技巧：明确告诉 LLM 这是工具执行的结果
			toolFeedback := fmt.Sprintf("【工具执行结果】: %s\n请根据以上结果回答用户。", toolResult)
			history = append(history, domain.Message{
				Role:    domain.RoleUser, // 这里用 User 伪装工具结果最通用，不用担心模型对 RoleTool 的兼容性
				Content: toolFeedback,
			})
		}
	}
	fmt.Println("Agent: Summarizing result...")
	_, spanFinal := tracer.Start(ctx, "LLM.Summarize")
	finalAnswer, err := uc.llm.Chat(ctx, history)
	spanFinal.End()
	return finalAnswer, err
}
