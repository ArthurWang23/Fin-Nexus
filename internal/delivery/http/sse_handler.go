package http

import (
	"context"
	"encoding/json"
	"fmt"
	"go-nexus/internal/usecase"
	"go-nexus/internal/workflow" // for SignalApprove, ApprovalSignal
	"go-nexus/pkg/auth"
	"io"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"go.temporal.io/sdk/client"
)

// activeWorkflows tracks the current active workflow ID for each session
var activeWorkflows = sync.Map{} // sessionID -> workflowID

type SSEHandler struct {
	rdb         *redis.Client
	tClient     client.Client
	blueprintUC *usecase.BlueprintUseCase
}

func NewSSEHandler(rdb *redis.Client, tClient client.Client, blueprintUC *usecase.BlueprintUseCase) *SSEHandler {
	return &SSEHandler{rdb: rdb, tClient: tClient, blueprintUC: blueprintUC}
}

// HandleStream establishes an SSE connection that streams workflow events for a session.
// The client subscribes once per session; all workflow events arrive through this single channel.
func (h *SSEHandler) HandleStream(c *gin.Context) {
	tokenStr := c.Query("token")
	if tokenStr == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Token required"})
		return
	}
	if _, err := auth.ParseToken(tokenStr); err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid token"})
		return
	}

	sessionID := c.Query("session_id")
	if sessionID == "" {
		sessionID = uuid.New().String()
	}

	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	c.Writer.Header().Set("X-Accel-Buffering", "no")
	c.Writer.Header().Set("Access-Control-Allow-Origin", "*")

	connectedMsg, _ := json.Marshal(map[string]string{"type": "connected", "content": sessionID})
	fmt.Fprintf(c.Writer, "data: %s\n\n", connectedMsg)
	c.Writer.Flush()

	ctx := c.Request.Context()
	pubsub := h.rdb.Subscribe(ctx, "stream:"+sessionID)
	defer pubsub.Close()

	ch := pubsub.Channel()
	c.Stream(func(w io.Writer) bool {
		select {
		case msg, ok := <-ch:
			if !ok {
				return false
			}
			fmt.Fprintf(w, "data: %s\n\n", msg.Payload)
			return true
		case <-ctx.Done():
			return false
		}
	})
}

// ChatRequest is the request body for sending a chat message.
type ChatRequest struct {
	SessionID string `json:"session_id" binding:"required"`
	Content   string `json:"content" binding:"required"`
}

// HandleChat starts the multi-agent workflow for a user message.
func (h *SSEHandler) HandleChat(c *gin.Context) {
	userID := c.GetString("userID")

	var req ChatRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	workflowID := fmt.Sprintf("chat-%s-%d", req.SessionID, time.Now().UnixNano())
	options := client.StartWorkflowOptions{
		ID:        workflowID,
		TaskQueue: "agent-task-queue",
	}
	activeWorkflows.Store(req.SessionID, workflowID)

	_, err := h.tClient.ExecuteWorkflow(
		context.Background(),
		options,
		workflow.StreamMultiAgentWorkflow,
		req.Content,
		req.SessionID,
		req.SessionID,
		userID,
	)
	if err != nil {
		log.Printf("Failed to start workflow: %v", err)
		activeWorkflows.Delete(req.SessionID)
		errorMsg := fmt.Sprintf(`{"type":"error","content":"Failed to start workflow: %v"}`, err)
		h.rdb.Publish(context.Background(), "stream:"+req.SessionID, errorMsg)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to start workflow"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"workflow_id": workflowID})
}

// BlueprintRunRequest is the request body for running a blueprint workflow.
type BlueprintRunRequest struct {
	SessionID   string `json:"session_id" binding:"required"`
	BlueprintID string `json:"blueprint_id" binding:"required"`
	Content     string `json:"content" binding:"required"`
}

// HandleBlueprintRun fetches the blueprint definition and starts the graph execution workflow.
func (h *SSEHandler) HandleBlueprintRun(c *gin.Context) {
	userID := c.GetString("userID")

	var req BlueprintRunRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	bp, err := h.blueprintUC.GetForExecution(req.BlueprintID, userID)
	if err != nil {
		errorMsg := fmt.Sprintf(`{"type":"error","content":"Failed to get blueprint: %v"}`, err)
		h.rdb.Publish(c.Request.Context(), "stream:"+req.SessionID, errorMsg)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get blueprint"})
		return
	}

	workflowID := fmt.Sprintf("graph-%s-%d", req.SessionID, time.Now().UnixNano())
	options := client.StartWorkflowOptions{
		ID:        workflowID,
		TaskQueue: "graph-task-queue",
	}
	activeWorkflows.Store(req.SessionID, workflowID)

	_, err = h.tClient.ExecuteWorkflow(
		c.Request.Context(),
		options,
		"GraphExecutionWorkflow",
		bp,
		req.Content,
		req.SessionID,
		req.SessionID,
		userID,
	)
	if err != nil {
		log.Printf("Failed to start GraphExecutionWorkflow: %v", err)
		activeWorkflows.Delete(req.SessionID)
		errorMsg := fmt.Sprintf(`{"type":"error","content":"Failed to start workflow: %v"}`, err)
		h.rdb.Publish(c.Request.Context(), "stream:"+req.SessionID, errorMsg)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to start workflow"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"workflow_id": workflowID})
}

// CancelRequest is the request body for cancelling an active workflow.
type CancelRequest struct {
	SessionID string `json:"session_id" binding:"required"`
}

// ApprovalRequest is the request body for approving or rejecting code execution.
type ApprovalRequest struct {
	SessionID    string `json:"session_id" binding:"required"`
	Approved     bool   `json:"approved"`
	Reason       string `json:"reason"`
	ModifiedCode string `json:"modified_code,omitempty"`
}

// HandleApproval sends an approval/rejection Signal to the running Temporal workflow.
// This resumes the workflow from its human-in-the-loop pause point.
func (h *SSEHandler) HandleApproval(c *gin.Context) {
	var req ApprovalRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	wfID, ok := activeWorkflows.Load(req.SessionID)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "No active workflow for this session"})
		return
	}

	signal := workflow.ApprovalSignal{
		Approved:     req.Approved,
		Reason:       req.Reason,
		ModifiedCode: req.ModifiedCode,
	}
	if err := h.tClient.SignalWorkflow(context.Background(), wfID.(string), "", workflow.SignalApprove, signal); err != nil {
		log.Printf("Failed to signal workflow %s: %v", wfID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to send approval signal"})
		return
	}

	action := "approved"
	if !req.Approved {
		action = "rejected"
	}
	log.Printf("Workflow %s %s by user", wfID, action)
	c.JSON(http.StatusOK, gin.H{"status": action})
}

// HandleCancel cancels the active workflow for a given session.
func (h *SSEHandler) HandleCancel(c *gin.Context) {
	var req CancelRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	wfID, ok := activeWorkflows.Load(req.SessionID)
	if !ok {
		c.JSON(http.StatusOK, gin.H{"status": "no active workflow"})
		return
	}

	if err := h.tClient.CancelWorkflow(context.Background(), wfID.(string), ""); err != nil {
		log.Printf("Failed to cancel workflow: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to cancel workflow"})
		return
	}

	log.Printf("Workflow %s cancelled", wfID)
	cancelMsg := `{"type":"done","content":"Workflow cancelled by user"}`
	h.rdb.Publish(context.Background(), "stream:"+req.SessionID, cancelMsg)
	activeWorkflows.Delete(req.SessionID)

	c.JSON(http.StatusOK, gin.H{"status": "cancelled"})
}
