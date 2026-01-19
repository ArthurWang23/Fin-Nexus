package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"go-nexus/internal/usecase/gateway"
	"net/http"
	"time"
)

const (
	JinaAPIURL    = "https://api.jina.ai/v1/rerank"
	JinaModelName = "jina-reranker-v2-base-multilingual"
)

type JinaReranker struct {
	apiKey     string
	httpClient *http.Client
}

func NewJinaReranker(apiKey string) *JinaReranker {
	return &JinaReranker{
		apiKey: apiKey,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

type jinaRequest struct {
	Model     string   `json:"model"`
	Query     string   `json:"query"`
	Documents []string `json:"documents"`
	TopN      int      `json:"top_n"`
}

type jinaResponse struct {
	Results []struct {
		Index          int     `json:"index"`
		RelevanceScore float64 `json:"relevance_score"`
	} `json:"results"`
	Usage struct {
		TotalTokens int `json:"total_tokens"`
	} `json:"usage"`
}

func (j *JinaReranker) Rerank(ctx context.Context, query string, documents []string, topN int) ([]gateway.RerankResult, error) {
	reqBody := jinaRequest{
		Model:     JinaModelName,
		Query:     query,
		Documents: documents,
		TopN:      topN,
	}
	jsonBody, _ := json.Marshal(reqBody)

	req, err := http.NewRequestWithContext(ctx, "POST", JinaAPIURL, bytes.NewBuffer(jsonBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+j.apiKey)

	resp, err := j.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("jina api request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("jina api returned status: %d", resp.StatusCode)
	}
	var jinaResp jinaResponse
	if err := json.NewDecoder(resp.Body).Decode(&jinaResp); err != nil {
		return nil, fmt.Errorf("failed to decode jina response: %w", err)
	}
	var results []gateway.RerankResult
	for _, item := range jinaResp.Results {
		if item.Index < len(documents) {
			results = append(results, gateway.RerankResult{
				Index:    item.Index,
				Score:    item.RelevanceScore,
				Document: documents[item.Index], // 填回原文
			})
		}
	}
	return results, nil
}
