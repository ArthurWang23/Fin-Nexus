package v1

import (
	"go-nexus/internal/usecase"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type ChatHandler struct {
	ragUc *usecase.RAGUseCase
}

func NewChatHandler(ragUc *usecase.RAGUseCase) *ChatHandler {
	return &ChatHandler{ragUc: ragUc}
}

type ChatRequest struct {
	Message   string `json:"message" binding:"required"`
	SessionID string `json:"session_id"`
}

func (h *ChatHandler) Chat(c *gin.Context) {
	var req ChatRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": err.Error(),
		})
		return
	}
	if req.SessionID == "" {
		req.SessionID = uuid.New().String()
	}
	userID := c.MustGet("userID").(string)
	answer, err := h.ragUc.SearchAndChat(c.Request.Context(), req.Message, userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"response":   answer,
		"session_id": req.SessionID,
	})
}

// 测试 handler
func (h *ChatHandler) AddKnowledge(c *gin.Context) {
	userID := c.MustGet("userID").(string)
	type Req struct {
		Text string `json:"text"`
	}
	var req Req
	if err := c.ShouldBindJSON(&req); err != nil {
		return
	}
	err := h.ragUc.AddDocumentText(c.Request.Context(), req.Text, "test_text", userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"status": "indexed",
	})
}
