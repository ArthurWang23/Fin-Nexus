package gateway

import (
	"context"
	"go-nexus/internal/domain"
)

// 传给llm的工具描述
type ToolDefinition struct {
	Name        string
	Description string
	Parameters  string
}

// 模型想要调用的具体动作
type ToolCall struct {
	ID   string
	Name string
	Args string
}

// 封装模型响应
type LLMResponse struct {
	Content   string
	ToolCalls []ToolCall
}

// 底层大模型接口
type LLMClient interface {
	// 发送对话历史，获取流式或非流式响应
	// 非流式 for now
	Chat(ctx context.Context, history []domain.Message) (string, error)
	// 将文本列表转化为向量列表
	// 批量处理
	Embed(ctx context.Context, texts []string) ([][]float32, error)

	ChatWithTools(ctx context.Context, history []domain.Message, tools []ToolDefinition) (*LLMResponse, error)

	StreamChat(ctx context.Context, history []domain.Message, onToken func(string)) (string, error)
}
