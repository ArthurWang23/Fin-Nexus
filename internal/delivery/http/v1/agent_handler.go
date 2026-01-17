package v1

import (
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go-nexus/internal/usecase"
	"go-nexus/internal/workflow"
	"go.opentelemetry.io/otel"
	"go.temporal.io/sdk/client"
	"net/http"
)

type AgentHandler struct {
	agentUC *usecase.AgentUseCase
	tClient client.Client
}

func NewAgentHandler(agentUC *usecase.AgentUseCase, tClient client.Client) *AgentHandler {
	return &AgentHandler{agentUC: agentUC, tClient: tClient}
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

func (h *AgentHandler) MultiChat(c *gin.Context) {
	var req ChatRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	tracer := otel.Tracer("http-handler")
	ctx, span := tracer.Start(c.Request.Context(), "HTTP POST /agent")
	defer span.End()
	answer, err := h.agentUC.MultiAgentChat(ctx, req.Message)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"response": answer})
}

func (h *AgentHandler) AsyncChat(c *gin.Context) {
	var req ChatRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	options := client.StartWorkflowOptions{
		ID:        "agent-workflow-" + uuid.New().String(),
		TaskQueue: "agent-task-queue",
	}
	we, err := h.tClient.ExecuteWorkflow(c.Request.Context(), options, workflow.MultiAgentWorkflow, req.Message)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	var result string
	err = we.Get(c.Request.Context(), &result)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"response": result, "workflow_id": we.GetID()})
}
