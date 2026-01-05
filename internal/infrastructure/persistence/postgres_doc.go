package persistence

import (
	"context"
	"github.com/pgvector/pgvector-go"
	"go-nexus/internal/domain"
	"go-nexus/internal/usecase/repo"
	"go-nexus/pkg/database"
	"gorm.io/gorm"
)

type PostgresRepo struct {
	db *gorm.DB
}

func NewPostgresRepo(db *gorm.DB) *PostgresRepo {
	return &PostgresRepo{db: db}
}

var _ repo.DocumentRepository = (*PostgresRepo)(nil)
var _ repo.VectorRepository = (*PostgresRepo)(nil)

func (r *PostgresRepo) Create(ctx context.Context, doc *domain.Document) error {
	model := database.DocumentModel{
		ID:     doc.ID,
		Name:   doc.Name,
		Type:   doc.Type,
		Status: string(doc.Status),
	}
	return r.db.WithContext(ctx).Create(&model).Error
}

func (r *PostgresRepo) GetByID(ctx context.Context, id string) (*domain.Document, error) {
	var model database.DocumentModel
	if err := r.db.WithContext(ctx).First(&model, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &domain.Document{
		ID:     model.ID,
		Name:   model.Name,
		Status: domain.DocumentStatus(model.Status),
	}, nil
}

func (r *PostgresRepo) UpdateStatus(ctx context.Context, id string, status domain.DocumentStatus) error {
	return r.db.WithContext(ctx).Model(&database.DocumentModel{}).Where("id = ?", id).Update("status", status).Error
}

func (r *PostgresRepo) StoreChunks(ctx context.Context, chunks []*domain.DocumentChunk) error {
	var models []database.DocumentChunkModel
	for _, chunk := range chunks {
		models = append(models, database.DocumentChunkModel{
			ID:         chunk.ID,
			DocumentID: chunk.DocumentID,
			Content:    chunk.Content,
			PageNumber: chunk.PageNumber,
			Vector:     pgvector.NewVector(chunk.Vector),
		})
	}
	return r.db.WithContext(ctx).Create(&models).Error
}

func (r *PostgresRepo) SearchSimilar(ctx context.Context, vector []float32, topK int) ([]*domain.DocumentChunk, error) {
	var models []database.DocumentChunkModel
	vec := pgvector.NewVector(vector)

	err := r.db.WithContext(ctx).Order(gorm.Expr("vector <=> ?", vec)).Limit(topK).Find(&models).Error
	if err != nil {
		return nil, err
	}
	var results []*domain.DocumentChunk
	for _, model := range models {
		results = append(results, &domain.DocumentChunk{
			ID:         model.ID,
			DocumentID: model.DocumentID,
			Content:    model.Content,
			PageNumber: model.PageNumber,
		})
	}
	return results, nil
}
