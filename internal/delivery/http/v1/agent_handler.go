package v1

import (
	"github.com/gin-gonic/gin"
	"go-nexus/internal/usecase"
	"go.opentelemetry.io/otel"
	"net/http"
)

type AgentHandler struct {
	agentUC *usecase.AgentUseCase
}

func NewAgentHandler(agentUC *usecase.AgentUseCase) *AgentHandler {
	return &AgentHandler{agentUC: agentUC}
}

func (h *AgentHandler) Chat(c *gin.Context) {
	var req ChatRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	tracer := otel.Tracer("http-handler")
	ctx, span := tracer.Start(c.Request.Context(), "HTTP POST /agent")
	defer span.End()
	answer, err := h.agentUC.ChatWithAgent(ctx, req.Message)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"response": answer})
}
