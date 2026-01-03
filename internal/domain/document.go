package domain

import "time"

type DocumentStatus string

const (
	StatusPending    DocumentStatus = "pending"
	StatusProcessing DocumentStatus = "processing"
	StatusIndexed    DocumentStatus = "indexed"
	StatusFailed     DocumentStatus = "failed"
)

// 用户上传的原始文件
type Document struct {
	ID        string         `json:"id"`
	Name      string         `json:"name"`
	Type      string         `json:"type"` // pdf,markdown
	Path      string         `json:"path"`
	Status    DocumentStatus `json:"status"`
	CreatedAt time.Time      `json:"created_at"`
}

// 分块后的文件
type DocumentChunk struct {
	ID         string    `json:"id"`
	DocumentID string    `json:"document_id"`
	Content    string    `json:"content"`
	PageNumber int       `json:"page_number"` // 在原文档中的页数
	Vector     []float32 `json:"-"`           // 向量数据 （ 不传JSON ）
}
