package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/sashabaranov/go-openai"
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

func (a *OpenAIAdapter) ChatWithTools(ctx context.Context, history []domain.Message, tools []gateway.ToolDefinition) (*gateway.LLMResponse, error) {
	var messages []openai.ChatCompletionMessage

	for _, msg := range history {
		role := string(msg.Role)
		// 注意：如果是 Tool 类型的消息（Agent loop 回传结果时），
		// OpenAI 要求必须带上 ToolCallID，这里 Sprint 4 简化演示，
		// 我们主要关注 User -> Assistant 的第一轮调用。
		// 在完整 Agent 中，这里需要更复杂的转换逻辑。
		messages = append(messages, openai.ChatCompletionMessage{
			Role:    string(role),
			Content: msg.Content,
		})
	}

	// 转换工具定义 Go struct -> Openai tool
	var openaiTools []openai.Tool
	for _, t := range tools {
		openaiTools = append(openaiTools, openai.Tool{
			Type: openai.ToolTypeFunction,
			Function: &openai.FunctionDefinition{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  json.RawMessage(t.Parameters),
			},
		})
	}

	resp, err := a.client.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
		Model:    a.config.Model,
		Messages: messages,
		Tools:    openaiTools,
	})
	if err != nil {
		return nil, err
	}
	if len(resp.Choices) == 0 {
		return nil, fmt.Errorf("empty response from llm")
	}

	msg := resp.Choices[0].Message

	result := &gateway.LLMResponse{
		Content: msg.Content,
	}
	if len(msg.ToolCalls) > 0 {
		for _, call := range msg.ToolCalls {
			result.ToolCalls = append(result.ToolCalls, gateway.ToolCall{
				ID:   call.ID,
				Name: call.Function.Name,
				Args: call.Function.Arguments,
			})
		}
	}
	return result, nil
}
