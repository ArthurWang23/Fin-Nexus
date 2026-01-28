package http

import (
	"go-nexus/internal/domain"

	"github.com/gin-gonic/gin"
)

// ModelOption 定义可选的模型配置
type ModelOption struct {
	Provider    string `json:"provider"`
	ModelName   string `json:"model_name"`
	DisplayName string `json:"display_name"`
	Description string `json:"description"`
	BaseURL     string `json:"base_url"`
}

// AvailableModels 预定义的模型列表
var AvailableModels = []ModelOption{
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

type ConfigHandler struct {
	repo domain.ConfigRepository
}

func NewConfigHandler(repo domain.ConfigRepository) *ConfigHandler {
	return &ConfigHandler{repo: repo}
}

type UpdateConfigRequest struct {
	AgentType string `json:"agent_type" binding:"required,oneof=Supervisor Researcher Coder"`
	Provider  string `json:"provider"`
	APIKey    string `json:"api_key"`
	ModelName string `json:"model_name"`
	BaseURL   string `json:"base_url"`
}

// GetAvailableModels 返回可选模型列表
func (h *ConfigHandler) GetAvailableModels(c *gin.Context) {
	c.JSON(200, AvailableModels)
}

// UpdateModelConfig 更新用户的模型配置
func (h *ConfigHandler) UpdateModelConfig(c *gin.Context) {
	userID := c.MustGet("userID").(string)
	var req UpdateConfigRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	cfg := &domain.UserModelConfig{
		UserID:    userID,
		AgentType: domain.AgentType(req.AgentType),
		Provider:  req.Provider,
		APIKey:    req.APIKey,
		ModelName: req.ModelName,
		BaseURL:   req.BaseURL,
	}

	if err := h.repo.SaveConfig(cfg); err != nil {
		c.JSON(500, gin.H{"error": "failed to save config"})
		return
	}
	c.JSON(200, gin.H{"status": "updated"})
}

// GetModelConfigs 获取用户当前的模型配置
func (h *ConfigHandler) GetModelConfigs(c *gin.Context) {
	userID := c.MustGet("userID").(string)
	configs, err := h.repo.GetAllConfigs(userID)
	if err != nil {
		c.JSON(500, gin.H{"error": "failed to get configs"})
		return
	}
	c.JSON(200, configs)
}

// DeleteModelConfig 删除用户的模型配置（恢复默认）
func (h *ConfigHandler) DeleteModelConfig(c *gin.Context) {
	userID := c.MustGet("userID").(string)
	agentType := c.Query("agent_type")
	if agentType == "" {
		c.JSON(400, gin.H{"error": "agent_type is required"})
		return
	}

	if err := h.repo.DeleteConfig(userID, domain.AgentType(agentType)); err != nil {
		c.JSON(500, gin.H{"error": "failed to delete config"})
		return
	}
	c.JSON(200, gin.H{"status": "deleted"})
}
