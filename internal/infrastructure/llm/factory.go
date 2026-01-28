package llm

import "go-nexus/internal/usecase/gateway"

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
	adapterConfig := &Config{
		APIKey:  config.APIKey,
		BaseURL: config.BaseURL,
		Model:   config.ModelName,
		// EmbeddingModel 可以留空，或者是默认的，Agent 动作一般不需要 Embedding
	}
	return NewOpenAIAdapter(adapterConfig)
}
