package http

import (
	"context"
	"fmt"
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
	CheckOrigin: func(r *http.Request) bool { return true }, // 允许入市
}

// activeWorkflows 跟踪每个 session 当前活动的工作流 ID
var activeWorkflows = sync.Map{} // sessionID -> workflowID

type WSHandler struct {
	rdb     *redis.Client
	tClient client.Client
}

func NewWSHandler(rdb *redis.Client, tClient client.Client) *WSHandler {
	return &WSHandler{rdb: rdb, tClient: tClient}
}

// 全双工聊天
func (h *WSHandler) HandleWS(c *gin.Context) {
	// 先检查 Token
	tokenStr := c.Query("token")
	if tokenStr == "" {
		// 尝试从 Header 获取 (有些 WebSocket 客户端支持，如 Postman)
		// 但浏览器原生不支持
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
			// 连接断开 (正常关闭或异常)
			log.Printf("WS Read Error: %v", err)
			// 清理工作流跟踪
			activeWorkflows.Delete(sessionID)
			break
		}
		userQuery := string(message)
		if userQuery == "" {
			continue
		}

		// 处理取消指令
		if userQuery == "CANCEL" {
			if wfID, ok := activeWorkflows.Load(sessionID); ok {
				err := h.tClient.CancelWorkflow(context.Background(), wfID.(string), "")
				if err != nil {
					log.Printf("Failed to cancel workflow: %v", err)
				} else {
					log.Printf("Workflow %s cancelled", wfID)
					// 发送取消确认消息
					cancelMsg := `{"type":"done","content":"Workflow cancelled by user"}`
					h.rdb.Publish(context.Background(), "stream:"+sessionID, cancelMsg)
				}
				activeWorkflows.Delete(sessionID)
			}
			continue
		}

		// WorkflowID 必须唯一，否则 Temporal 会报错 "Already Started"。
		// 每次对话都是一个新的 Run。
		workflowID := fmt.Sprintf("chat-%s-%d", sessionID, time.Now().UnixNano())
		options := client.StartWorkflowOptions{
			ID:        workflowID,
			TaskQueue: "agent-task-queue",
		}
		// 存储活动工作流 ID
		activeWorkflows.Store(sessionID, workflowID)
		// streamID 使用 sessionID 把消息推送到同一个 Redis
		_, err = h.tClient.ExecuteWorkflow(context.Background(), options, workflow.StreamMultiAgentWorkflow, userQuery, sessionID, sessionID, userID)
		if err != nil {
			log.Printf("Failed to start workflow: %v", err)
			activeWorkflows.Delete(sessionID)
			continue
		}
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
