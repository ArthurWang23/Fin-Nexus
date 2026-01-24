package http

import (
	"context"
	"fmt"
	"go-nexus/internal/workflow"
	"log"
	"net/http"
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

type WSHandler struct {
	rdb     *redis.Client
	tClient client.Client
}

func NewWSHandler(rdb *redis.Client, tClient client.Client) *WSHandler {
	return &WSHandler{rdb: rdb, tClient: tClient}
}

// 全双工聊天
func (h *WSHandler) HandleWS(c *gin.Context) {
	sessionID := c.Query("session_id")
	if sessionID == "" {
		sessionID = uuid.New().String()
	}
	ws, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}
	defer ws.Close()
	// 开一个协程，来数据了就显示
	go h.writePump(ws, sessionID)
	// 主线程调用 ReadMessage。每收到一条，就启动一个新的 Temporal Workflow
	h.readPump(c, ws, sessionID)
}

func (h *WSHandler) readPump(c *gin.Context, ws *websocket.Conn, sessionID string) {
	for {
		_, message, err := ws.ReadMessage()
		if err != nil {
			// 连接断开 (正常关闭或异常)
			log.Printf("WS Read Error: %v", err)
			break
		}
		userQuery := string(message)
		if userQuery == "" {
			continue
		}

		// WorkflowID 必须唯一，否则 Temporal 会报错 "Already Started"。
		// 每次对话都是一个新的 Run。
		workflowID := fmt.Sprintf("chat-%s-%d", sessionID, time.Now().UnixNano())
		options := client.StartWorkflowOptions{
			ID:        workflowID,
			TaskQueue: "agent-task-queue",
		}
		// streamID 使用 sessionID 把消息推送到同一个 Redis
		_, err = h.tClient.ExecuteWorkflow(context.Background(), options, workflow.StreamMultiAgentWorkflow, userQuery, sessionID, sessionID)
		if err != nil {
			log.Printf("Failed to start workflow: %v", err)
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
