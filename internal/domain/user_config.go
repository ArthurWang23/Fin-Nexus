package domain

// AgentType 定义 Agent 类型
type AgentType string

const (
	AgentSupervisor AgentType = "Supervisor"
	AgentResearcher AgentType = "Researcher"
	AgentCoder      AgentType = "Coder"
	AgentDynamic    AgentType = "Dynamic" // 用于 Blueprint 中的动态 LLM 节点
)

// UserModelConfig 用户模型配置
type UserModelConfig struct {
	UserID    string    `gorm:"primaryKey;index:idx_user_agent" json:"user_id"`
	AgentType AgentType `gorm:"primaryKey;index:idx_user_agent" json:"agent_type"` // 复合主键

	Provider  string `json:"provider"`   // "openai", "qwen", "deepseek"
	APIKey    string `json:"api_key"`    // 加密存储
	BaseURL   string `json:"base_url"`   // 例如 https://api.openai.com/v1
	ModelName string `json:"model_name"` // 例如 "gpt-4-turbo"
}

// ConfigRepository 配置仓库接口
type ConfigRepository interface {
	SaveConfig(config *UserModelConfig) error
	GetConfig(userID string, agentType AgentType) (*UserModelConfig, error)
	GetAllConfigs(userID string) ([]UserModelConfig, error)
	DeleteConfig(userID string, agentType AgentType) error
}

// LLMConfig 统一的 LLM 配置结构（用于运行时）
type LLMConfig struct {
	Provider  string `json:"provider"`
	APIKey    string `json:"api_key"`
	BaseURL   string `json:"base_url"`
	ModelName string `json:"model_name"`
}

// GetLLMConfigFromBlueprint 从 Blueprint 配置创建 LLMConfig
func GetLLMConfigFromBlueprint(bpConfig BlueprintLLMConfig) *LLMConfig {
	return &LLMConfig{
		Provider:  bpConfig.Provider,
		APIKey:    bpConfig.APIKey,
		BaseURL:   bpConfig.BaseURL,
		ModelName: bpConfig.ModelName,
	}
}

// GetLLMConfigFromUserConfig 从 UserModelConfig 创建 LLMConfig
func GetLLMConfigFromUserConfig(cfg *UserModelConfig) *LLMConfig {
	if cfg == nil {
		return nil
	}
	return &LLMConfig{
		Provider:  cfg.Provider,
		APIKey:    cfg.APIKey,
		BaseURL:   cfg.BaseURL,
		ModelName: cfg.ModelName,
	}
}
