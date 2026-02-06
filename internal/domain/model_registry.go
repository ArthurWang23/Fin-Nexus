package domain

// ModelInfo 定义模型的元信息
type ModelInfo struct {
	Provider    string `json:"provider"`
	ModelName   string `json:"model_name"`
	DisplayName string `json:"display_name"`
	Description string `json:"description"`
	BaseURL     string `json:"base_url"`
}

// AvailableModels 预定义的模型列表（全局注册表）
var AvailableModels = []ModelInfo{
	// OpenAI
	{Provider: "openai", ModelName: "gpt-5.2", DisplayName: "GPT-5.2", Description: "OpenAI最新旗舰模型", BaseURL: "https://api.openai.com/v1"},
	{Provider: "openai", ModelName: "gpt-5-mini", DisplayName: "GPT-5 Mini", Description: "轻量快速，性价比高", BaseURL: "https://api.openai.com/v1"},
	{Provider: "openai", ModelName: "gpt-4.1", DisplayName: "GPT-4.1", Description: "Non-Reasoning", BaseURL: "https://api.openai.com/v1"},

	// DeepSeek
	{Provider: "deepseek", ModelName: "deepseek-chat", DisplayName: "DeepSeek V3.2 Non-Reasoning", Description: "DeepSeek V3.2", BaseURL: "https://api.deepseek.com"},
	{Provider: "deepseek", ModelName: "deepseek-reasoner", DisplayName: "DeepSeek V3.2 Reasoning", Description: "推理增强模型", BaseURL: "https://api.deepseek.com"},

	// Qwen (通义千问)
	{Provider: "qwen", ModelName: "qwen3-max", DisplayName: "通义千问 Max", Description: "阿里最强模型，默认配置", BaseURL: "https://dashscope.aliyuncs.com/compatible-mode/v1"},

	// Claude (Anthropic)
	{Provider: "anthropic", ModelName: "claude-sonnet-4-5", DisplayName: "Claude Sonnet 4.5", Description: "适合Coding", BaseURL: "https://api.anthropic.com/v1"},
	{Provider: "anthropic", ModelName: "claude-opus-4-5", DisplayName: "Claude Opus 4.5", Description: "高级推理，无需多言", BaseURL: "https://api.anthropic.com/v1"},
}

// LookupModelInfo 根据 model_name 查找模型信息
// 返回 nil 表示未找到
func LookupModelInfo(modelName string) *ModelInfo {
	for _, m := range AvailableModels {
		if m.ModelName == modelName {
			return &m
		}
	}
	return nil
}
