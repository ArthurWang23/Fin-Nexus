package domain

type AgentType string

const (
	AgentSupervisor AgentType = "Supervisor"
	AgentResearcher AgentType = "Researcher"
	AgentCoder      AgentType = "Coder"
)

type UserModelConfig struct {
	UserID    string    `gorm:"primaryKey;index:idx_user_agent" json:"user_id"`
	AgentType AgentType `gorm:"primaryKey;index:idx_user_agent" json:"agent_type"` // 复合主键

	Provider  string `json:"provider"`   // "openai", "qwen", "deepseek"
	APIKey    string `json:"api_key"`    // 建议生产环境加密存储
	BaseURL   string `json:"base_url"`   // 例如 https://api.openai.com/v1
	ModelName string `json:"model_name"` // 例如 "gpt-4-turbo"
}

type ConfigRepository interface {
	SaveConfig(config *UserModelConfig) error
	GetConfig(userID string, agentType AgentType) (*UserModelConfig, error)
	GetAllConfigs(userID string) ([]UserModelConfig, error)
	DeleteConfig(userID string, agentType AgentType) error
}
