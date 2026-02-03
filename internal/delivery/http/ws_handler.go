package http

import (
	"context"
	"encoding/json"
	"fmt"
	"go-nexus/internal/usecase"
	"go-nexus/internal/workflow"
	"go-nexus/pkg/auth"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/redis/go-redis/v9"
	"go.temporal.io/sdk/client"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

// activeWorkflows 跟踪每个 session 当前活动的工作流 ID
var activeWorkflows = sync.Map{} // sessionID -> workflowID

// WSMessage WebSocket 消息格式
type WSMessage struct {
	Type        string `json:"type"`                   // "chat" | "blueprint" | "cancel"
	Content     string `json:"content,omitempty"`      // 用户消息内容
	BlueprintID string `json:"blueprint_id,omitempty"` // Blueprint ID（可选）
}

type WSHandler struct {
	rdb         *redis.Client
	tClient     client.Client
	blueprintUC *usecase.BlueprintUseCase
}

func NewWSHandler(rdb *redis.Client, tClient client.Client, blueprintUC *usecase.BlueprintUseCase) *WSHandler {
	return &WSHandler{rdb: rdb, tClient: tClient, blueprintUC: blueprintUC}
}

// HandleWS 全双工聊天
func (h *WSHandler) HandleWS(c *gin.Context) {
	// 先检查 Token
	tokenStr := c.Query("token")
	if tokenStr == "" {
		tokenStr = c.GetHeader("Sec-WebSocket-Protocol")
	}
	if tokenStr == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Token required"})
		return
	}
	userID, err := auth.ParseToken(tokenStr)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid token"})
		return
	}
	sessionID := c.Query("session_id")
	if sessionID == "" {
		sessionID = uuid.New().String()
	}
	// 升级连接
	ws, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}
	defer ws.Close()
	// 开一个协程，来数据了就显示
	go h.writePump(ws, sessionID)
	// 主线程调用 ReadMessage。每收到一条，就启动一个新的 Temporal Workflow
	h.readPump(c, ws, sessionID, userID)
}

func (h *WSHandler) readPump(c *gin.Context, ws *websocket.Conn, sessionID string, userID string) {
	for {
		_, message, err := ws.ReadMessage()
		if err != nil {
			log.Printf("WS Read Error: %v", err)
			activeWorkflows.Delete(sessionID)
			break
		}

		rawMessage := string(message)
		if rawMessage == "" {
			continue
		}

		// 尝试解析为 JSON
		var wsMsg WSMessage
		if err := json.Unmarshal(message, &wsMsg); err != nil {
			// 如果不是 JSON，当作纯文本处理（兼容旧版）
			wsMsg = WSMessage{
				Type:    "chat",
				Content: rawMessage,
			}
		}

		// 处理不同类型的消息
		switch wsMsg.Type {
		case "cancel", "CANCEL":
			h.handleCancel(sessionID)

		case "blueprint":
			// 使用自定义 Blueprint 执行
			h.handleBlueprintExecution(c.Request.Context(), sessionID, userID, wsMsg)

		default:
			// 默认使用原有的 Multi-Agent 工作流
			h.handleDefaultChat(sessionID, userID, wsMsg.Content)
		}
	}
}

// handleCancel 处理取消请求
func (h *WSHandler) handleCancel(sessionID string) {
	if wfID, ok := activeWorkflows.Load(sessionID); ok {
		err := h.tClient.CancelWorkflow(context.Background(), wfID.(string), "")
		if err != nil {
			log.Printf("Failed to cancel workflow: %v", err)
		} else {
			log.Printf("Workflow %s cancelled", wfID)
			cancelMsg := `{"type":"done","content":"Workflow cancelled by user"}`
			h.rdb.Publish(context.Background(), "stream:"+sessionID, cancelMsg)
		}
		activeWorkflows.Delete(sessionID)
	}
}

// handleDefaultChat 处理默认聊天（使用 StreamMultiAgentWorkflow）
func (h *WSHandler) handleDefaultChat(sessionID, userID, userQuery string) {
	if userQuery == "" {
		return
	}

	// 兼容旧版纯文本 "CANCEL"
	if userQuery == "CANCEL" {
		h.handleCancel(sessionID)
		return
	}

	workflowID := fmt.Sprintf("chat-%s-%d", sessionID, time.Now().UnixNano())
	options := client.StartWorkflowOptions{
		ID:        workflowID,
		TaskQueue: "agent-task-queue",
	}
	activeWorkflows.Store(sessionID, workflowID)

	_, err := h.tClient.ExecuteWorkflow(
		context.Background(),
		options,
		workflow.StreamMultiAgentWorkflow,
		userQuery,
		sessionID,
		sessionID,
		userID,
	)
	if err != nil {
		log.Printf("Failed to start workflow: %v", err)
		activeWorkflows.Delete(sessionID)
		errorMsg := fmt.Sprintf(`{"type":"error","content":"Failed to start workflow: %v"}`, err)
		h.rdb.Publish(context.Background(), "stream:"+sessionID, errorMsg)
	}
}

// handleBlueprintExecution 处理 Blueprint 执行
func (h *WSHandler) handleBlueprintExecution(ctx context.Context, sessionID, userID string, wsMsg WSMessage) {
	if wsMsg.BlueprintID == "" {
		errorMsg := `{"type":"error","content":"blueprint_id is required"}`
		h.rdb.Publish(ctx, "stream:"+sessionID, errorMsg)
		return
	}

	// 获取 Blueprint（带解密配置）
	bp, err := h.blueprintUC.GetForExecution(wsMsg.BlueprintID, userID)
	if err != nil {
		errorMsg := fmt.Sprintf(`{"type":"error","content":"Failed to get blueprint: %v"}`, err)
		h.rdb.Publish(ctx, "stream:"+sessionID, errorMsg)
		return
	}

	workflowID := fmt.Sprintf("graph-%s-%d", sessionID, time.Now().UnixNano())
	options := client.StartWorkflowOptions{
		ID:        workflowID,
		TaskQueue: "graph-task-queue",
	}
	activeWorkflows.Store(sessionID, workflowID)

	// 使用 TypeScript 的 GraphExecutionWorkflow
	// 注意：这个工作流在 TS Worker 中定义
	_, err = h.tClient.ExecuteWorkflow(
		ctx,
		options,
		"GraphExecutionWorkflow", // TS 工作流名称
		bp,                       // Blueprint 结构
		wsMsg.Content,            // 用户输入
		sessionID,                // streamId
		userID,                   // userId
	)
	if err != nil {
		log.Printf("Failed to start GraphExecutionWorkflow: %v", err)
		activeWorkflows.Delete(sessionID)
		errorMsg := fmt.Sprintf(`{"type":"error","content":"Failed to start workflow: %v"}`, err)
		h.rdb.Publish(ctx, "stream:"+sessionID, errorMsg)
	}
}

func (h *WSHandler) writePump(ws *websocket.Conn, sessionID string) {
	pubsub := h.rdb.Subscribe(context.Background(), "stream:"+sessionID)
	defer pubsub.Close()

	ch := pubsub.Channel()
	for msg := range ch {
		err := ws.WriteMessage(websocket.TextMessage, []byte(msg.Payload))
		if err != nil {
			log.Printf("WS Write Error: %v", err)
			break
		}
	}
}
