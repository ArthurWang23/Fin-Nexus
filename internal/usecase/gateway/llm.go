package gateway

import (
	"context"
	"go-nexus/internal/domain"
)

// 底层大模型接口
type LLMClient interface {
	// 发送对话历史，获取流式或非流式响应
	// 非流式 for now
	Chat(ctx context.Context, history []domain.Message) (string, error)
	// 将文本列表转化为向量列表
	// 批量处理
	Embed(ctx context.Context, texts []string) ([][]float32, error)
}
