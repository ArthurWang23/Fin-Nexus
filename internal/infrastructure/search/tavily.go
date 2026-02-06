package search

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

const TavilyURL = "https://api.tavily.com/search"

type TavilyClient struct {
	apiKey string
}

func NewTavilyClient(apiKey string) *TavilyClient {
	return &TavilyClient{apiKey: apiKey}
}

type tavilyRequest struct {
	APIKey      string `json:"api_key"`
	Query       string `json:"query"`
	SearchDepth string `json:"search_depth"` // "basic" or "advanced"
	MaxResults  int    `json:"max_results"`
}

type tavilyResponse struct {
	Results []struct {
		Title   string `json:"title"`
		URL     string `json:"url"`
		Content string `json:"content"`
	} `json:"results"`
}

// Search 执行联网搜索并返回摘要字符串
func (c *TavilyClient) Search(ctx context.Context, query string) (string, error) {
	reqBody := tavilyRequest{
		APIKey:      c.apiKey,
		Query:       query,
		SearchDepth: "basic", // basic 速度快，advanced 内容全
		MaxResults:  5,
	}
	jsonBody, _ := json.Marshal(reqBody)

	req, _ := http.NewRequestWithContext(ctx, "POST", TavilyURL, bytes.NewBuffer(jsonBody))
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var res tavilyResponse
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return "", err
	}

	// 组装成 LLM 易读的格式
	var resultStr string
	for _, item := range res.Results {
		resultStr += fmt.Sprintf("Title: %s\nURL: %s\nContent: %s\n---\n", item.Title, item.URL, item.Content)
	}

	if resultStr == "" {
		return "No online results found.", nil
	}

	return resultStr, nil
}
