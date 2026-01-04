package http

import (
	"github.com/gin-gonic/gin"
	"go-nexus/internal/usecase"
	"net/http"
)

type ChatHandler struct {
	ragUc *usecase.RAGUseCase
}

func NewChatHandler(ragUc *usecase.RAGUseCase) *ChatHandler {
	return &ChatHandler{ragUc: ragUc}
}

type ChatRequest struct {
	Message string `json:"message" binding:"required"`
}

func (h *ChatHandler) Chat(c *gin.Context) {
	var req ChatRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": err.Error(),
		})
		return
	}

	answer, err := h.ragUc.Chat(c.Request.Context(), req.Message)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"response": answer,
	})
}
