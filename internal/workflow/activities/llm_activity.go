package activities

import (
	"context"
	"go-nexus/internal/domain"
	"go-nexus/internal/usecase/gateway"
)

type LLMActivity struct {
	llm gateway.LLMClient
}

func NewLLMActivity(llm gateway.LLMClient) *LLMActivity {
	return &LLMActivity{
		llm: llm,
	}
}

func (a *LLMActivity) Chat(ctx context.Context, history []domain.Message) (string, error) {
	return a.llm.Chat(ctx, history)
}
