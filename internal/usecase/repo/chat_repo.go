package repo

import (
	"context"
	"go-nexus/internal/domain"
)

type ChatHistoryRepository interface {
	// GetHistory 获取最近的 N 条消息
	GetHistory(ctx context.Context, sessionID string, limit int) ([]domain.Message, error)

	// AddMessage 追加一条消息
	AddMessage(ctx context.Context, sessionID string, msg domain.Message) error

	// Clear 清空会话
	Clear(ctx context.Context, sessionID string) error
}
