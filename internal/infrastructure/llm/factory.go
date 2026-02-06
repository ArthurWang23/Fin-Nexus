package llm

import (
	"go-nexus/internal/domain"
	"go-nexus/internal/usecase/gateway"
)

type LLMFactory struct {
	// 系统默认配置 (兜底用)
	defaultClient gateway.LLMClient
}

func NewLLMFactory(defaultClient gateway.LLMClient) *LLMFactory {
	return &LLMFactory{defaultClient: defaultClient}
}

func (f *LLMFactory) CreateClient(config *gateway.LLMConfig) gateway.LLMClient {
	if config == nil || config.APIKey == "" {
		return f.defaultClient
	}

	baseURL := config.BaseURL
	modelName := config.ModelName

	// 如果没有提供 BaseURL，根据 model_name 自动查找
	if baseURL == "" && modelName != "" {
		if info := domain.LookupModelInfo(modelName); info != nil {
			baseURL = info.BaseURL
		}
	}

	adapterConfig := &Config{
		APIKey:  config.APIKey,
		BaseURL: baseURL,
		Model:   modelName,
		// EmbeddingModel 可以留空，或者是默认的，Agent 动作一般不需要 Embedding
	}
	return NewOpenAIAdapter(adapterConfig)
}
