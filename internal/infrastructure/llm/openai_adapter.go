package llm

import (
	"context"
	"fmt"

	"github.com/sashabaranov/g
	"go-nexus/internal/domain"
	"go-nexus/internal/usecase/gateway"
)

type OpenAIAdapter struct {
	client *openai.Client
	config *Config
}

type Config struct {
	APIKey         string
	BaseURL        string
	Model          string
	EmbeddingModel string
}

func NewOpenAIAdapter(cfg *Config) *OpenAIAdapter {
	// 1. 配置 ClientConfig
	c := openai.DefaultConfig(cfg.APIKey)
	if cfg.BaseURL != "" {
		c.BaseURL = cfg.BaseURL
	}
	client := openai.NewClientWithConfig(c)
	return &OpenAIAdapter{
		client: client,
		config: cfg,
	}
}

var _ gateway.LLMClient = (*OpenAIAdapter)(nil)

func (a *OpenAIAdapter) Chat(ctx context.Context, history []domain.Message) (string, error) {
	var messages []openai.ChatCompletionMessage
	for _, msg := range history {
		messages = append(messages, openai.ChatCompletionMessage{
			Role:    string(msg.Role),
			Content: msg.Content,
		})
	}
	resp, err := a.client.CreateChatCompletion(
		ctx,
		openai.ChatCompletionRequest{
			Model:    a.config.Model,
			Messages: messages,
			// Temperature: 0.7, // 可以根据需要配置
		},
	)
	if err != nil {
		return "", err
	}
	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("empty response from llm")
	}
	return resp.Choices[0].Message.Content, nil
}

// Embed 实现向量化
func (a *OpenAIAdapter) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	const maxBatchSize = 10
	var allResults [][]float32
	// 将文本列表分成多个批次
	for i := 0; i < len(texts); i += maxBatchSize {
		end := i + maxBatchSize
		if end > len(texts) {
			end = len(texts)
		}
		batch := texts[i:end]
		resp, err := a.client.CreateEmbeddings(
			ctx,
			openai.EmbeddingRequest{
				Input: batch,
				Model: openai.EmbeddingModel(a.config.EmbeddingModel), // 默认用这个，如果是 DeepSeek 需查阅文档是否支持
			},
		)
		if err != nil {
			return nil, err
		}
		for _, data := range resp.Data {
			allResults = append(allResults, data.Embedding)
		}
	}
	return allResults, nil
}

func (a *OpenAIAdapter) ChatWithTools(ctx context.Context, history []domain.Message, tools gateway.ToolDefinition) (*gateway.LLMResponse, error) {

}
