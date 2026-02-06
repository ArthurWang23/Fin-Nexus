package http

import (
	"go-nexus/internal/domain"

	"github.com/gin-gonic/gin"
)

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
	c.JSON(200, domain.AvailableModels)
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
