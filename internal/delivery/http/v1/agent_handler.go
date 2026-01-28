package v1

import (
	"go-nexus/internal/domain"
	"go-nexus/internal/usecase"
	"net/http"

	"github.com/gin-gonic/gin"
	"go.temporal.io/sdk/client"
)

type AgentHandler struct {
	agentUC     *usecase.AgentUseCase
	tClient     client.Client
	sessionRepo domain.SessionRepository
}

type ApprovalRequest struct {
	WorkflowID string `json:"workflow_id" binding:"required"`
	Approved   bool   `json:"approved"`
	Reason     string `json:"reason"`
}

func NewAgentHandler(agentUC *usecase.AgentUseCase, tClient client.Client, sessionRepo domain.SessionRepository) *AgentHandler {
	return &AgentHandler{agentUC: agentUC, tClient: tClient, sessionRepo: sessionRepo}
}

func (h *AgentHandler) Approve(c *gin.Context) {
	var req ApprovalRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	signalVal := struct {
		Approved bool
		Reason   string
	}{
		Approved: req.Approved,
		Reason:   req.Reason,
	}
	err := h.tClient.SignalWorkflow(c.Request.Context(), req.WorkflowID, "", "APPROVE_SIGNAL", signalVal)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	status := "Approved"
	if !req.Approved {
		status = "Rejected"
	}
	c.JSON(200, gin.H{"message": "Signal sent: " + status})
}

func (h *AgentHandler) ListSessions(c *gin.Context) {
	userID := c.MustGet("userID").(string) // 从 JWT 中间件获取
	sessions, err := h.sessionRepo.ListSessions(userID)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, sessions)
}

func (h *AgentHandler) GetSessionHistory(c *gin.Context) {
	userID := c.MustGet("userID").(string)
	sessionID := c.Param("id")
	sess, err := h.sessionRepo.GetSessionByID(sessionID)
	if err != nil || sess.UserID != userID {
		c.JSON(403, gin.H{"error": "Access denied or session not found"})
		return
	}

	messages, err := h.sessionRepo.GetMessages(sessionID)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, messages)
}

// CancelWorkflow 取消正在执行的工作流
func (h *AgentHandler) CancelWorkflow(c *gin.Context) {
	var req struct {
		WorkflowID string `json:"workflow_id" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	err := h.tClient.CancelWorkflow(c.Request.Context(), req.WorkflowID, "")
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"message": "Workflow cancelled"})
}
