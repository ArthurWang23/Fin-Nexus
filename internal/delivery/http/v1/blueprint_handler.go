package v1

import (
	"go-nexus/internal/domain"
	"go-nexus/internal/usecase"
	"net/http"

	"github.com/gin-gonic/gin"
)

// BlueprintHandler Blueprint API 处理器
type BlueprintHandler struct {
	blueprintUC *usecase.BlueprintUseCase
}

// NewBlueprintHandler 创建 Blueprint Handler
func NewBlueprintHandler(blueprintUC *usecase.BlueprintUseCase) *BlueprintHandler {
	return &BlueprintHandler{blueprintUC: blueprintUC}
}

// CreateBlueprintRequest 创建 Blueprint 请求
type CreateBlueprintRequest struct {
	Name        string                    `json:"name" binding:"required"`
	Description string                    `json:"description"`
	StartNodeID string                    `json:"start_node_id" binding:"required"`
	Nodes       []domain.GraphNode        `json:"nodes" binding:"required,min=1"`
	Edges       []domain.Edge             `json:"edges"`
	LLMConfig   domain.BlueprintLLMConfig `json:"llm_config"`
	IsPublic    bool                      `json:"is_public"`
}

// UpdateBlueprintRequest 更新 Blueprint 请求
type UpdateBlueprintRequest struct {
	Name        string                    `json:"name"`
	Description string                    `json:"description"`
	StartNodeID string                    `json:"start_node_id"`
	Nodes       []domain.GraphNode        `json:"nodes"`
	Edges       []domain.Edge             `json:"edges"`
	LLMConfig   domain.BlueprintLLMConfig `json:"llm_config"`
	IsPublic    bool                      `json:"is_public"`
}

// Create 创建新的 Blueprint
// POST /api/v1/blueprints
func (h *BlueprintHandler) Create(c *gin.Context) {
	userID := c.MustGet("userID").(string)

	var req CreateBlueprintRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	bp := &domain.WorkflowBlueprint{
		UserID:      userID,
		Name:        req.Name,
		Description: req.Description,
		StartNodeID: req.StartNodeID,
		Nodes:       req.Nodes,
		Edges:       req.Edges,
		LLMConfig:   req.LLMConfig,
		IsPublic:    req.IsPublic,
	}

	if err := h.blueprintUC.Create(bp); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, bp)
}

// Update 更新 Blueprint
// PUT /api/v1/blueprints/:id
func (h *BlueprintHandler) Update(c *gin.Context) {
	userID := c.MustGet("userID").(string)
	blueprintID := c.Param("id")

	var req UpdateBlueprintRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// 获取现有的 Blueprint
	existing, err := h.blueprintUC.GetByID(blueprintID, userID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "blueprint not found"})
		return
	}

	// 更新字段
	if req.Name != "" {
		existing.Name = req.Name
	}
	if req.Description != "" {
		existing.Description = req.Description
	}
	if req.StartNodeID != "" {
		existing.StartNodeID = req.StartNodeID
	}
	if len(req.Nodes) > 0 {
		existing.Nodes = req.Nodes
	}
	if len(req.Edges) > 0 {
		existing.Edges = req.Edges
	}
	if req.LLMConfig.Provider != "" {
		existing.LLMConfig = req.LLMConfig
	}
	existing.IsPublic = req.IsPublic

	if err := h.blueprintUC.Update(existing, userID); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, existing)
}

// Delete 删除 Blueprint
// DELETE /api/v1/blueprints/:id
func (h *BlueprintHandler) Delete(c *gin.Context) {
	userID := c.MustGet("userID").(string)
	blueprintID := c.Param("id")

	if err := h.blueprintUC.Delete(blueprintID, userID); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "blueprint deleted"})
}

// Get 获取单个 Blueprint
// GET /api/v1/blueprints/:id
func (h *BlueprintHandler) Get(c *gin.Context) {
	userID := c.MustGet("userID").(string)
	blueprintID := c.Param("id")

	bp, err := h.blueprintUC.GetByID(blueprintID, userID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, bp)
}

// List 列出用户的所有 Blueprint
// GET /api/v1/blueprints
func (h *BlueprintHandler) List(c *gin.Context) {
	userID := c.MustGet("userID").(string)

	blueprints, err := h.blueprintUC.ListByUser(userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, blueprints)
}

// ListPublic 列出所有公开的 Blueprint
// GET /api/v1/blueprints/public
func (h *BlueprintHandler) ListPublic(c *gin.Context) {
	blueprints, err := h.blueprintUC.ListPublic()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, blueprints)
}

// Clone 克隆一个 Blueprint
// POST /api/v1/blueprints/:id/clone
func (h *BlueprintHandler) Clone(c *gin.Context) {
	userID := c.MustGet("userID").(string)
	blueprintID := c.Param("id")

	cloned, err := h.blueprintUC.Clone(blueprintID, userID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, cloned)
}

// Validate 验证 Blueprint（不保存）
// POST /api/v1/blueprints/validate
func (h *BlueprintHandler) Validate(c *gin.Context) {
	var req CreateBlueprintRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	bp := &domain.WorkflowBlueprint{
		Name:        req.Name,
		StartNodeID: req.StartNodeID,
		Nodes:       req.Nodes,
		Edges:       req.Edges,
	}

	result := h.blueprintUC.Validate(bp)
	c.JSON(http.StatusOK, result)
}
