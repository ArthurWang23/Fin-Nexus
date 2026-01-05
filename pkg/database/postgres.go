package database

import (
	"github.com/pgvector/pgvector-go"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"log"
)

func NewPostgresDB(dsn string) *gorm.DB {
	db, err := gorm.Open(postgres.Open(dsn))
	if err != nil {
		log.Fatalf("failed to connect to database: %v", err)
	}
	// 创建 vector 扩展
	db.Exec("CREATE EXTENSION IF NOT EXISTS vector")
	return db
}

type DocumentModel struct {
	ID        string `gorm:"primaryKey"`
	Name      string
	Type      string
	Status    string
	CreatedAt int64 `gorm:"autoCreateTime"`
}

func (DocumentModel) TableName() string {
	return "documents"
}

type DocumentChunkModel struct {
	ID         string `gorm:"primaryKey"`
	DocumentID string `gorm:"index"`
	Content    string
	PageNumber int
	Vector     pgvector.Vector `gorm:"type:vector(1024)"`
}

func (DocumentChunkModel) TableName() string {
	return "document_chunks"
}
