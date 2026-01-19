package gateway

import "context"

type RerankResult struct {
	Index    int     `json:"index"`    // 原文档在列表中的索引
	Score    float64 `json:"score"`    // 相关性得分
	Document string  `json:"document"` // 文档内容
}

type RerankClient interface {
	Rerank(ctx context.Context, query string, documents []string, topN int) ([]RerankResult, error)
}
