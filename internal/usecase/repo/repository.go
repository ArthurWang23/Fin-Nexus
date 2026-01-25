package repo

import (
	"context"
	"go-nexus/internal/domain"
)

// 文档元数据 CRUD
type DocumentRepository interface {
	Create(ctx context.Context, doc *domain.Document) error
	GetByID(ctx context.Context, id string) (*domain.Document, error)
	UpdateStatus(ctx context.Context, id string, status domain.DocumentStatus) error
}

// 向量数据的存储与检索
type VectorRepository interface {
	StoreChunks(ctx context.Context, chunks []*domain.DocumentChunk) error
	// 语义搜索，根据查询向量返回最相似 topK 个切片
	SearchSimilar(ctx context.Context, vector []float32, topK int, userID string) ([]*domain.DocumentChunk, error)
}
