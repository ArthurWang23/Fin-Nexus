package http

import (
	"go-nexus/internal/workflow"
	"net/http"

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

func (h *WSHandler) HandleWS(c *gin.Context) {
	sessionID := c.Query("session_id")
	if sessionID == "" {
		sessionID = "default-session"
	}
	ws, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}
	defer ws.Close()

	_, msgBytes, err := ws.ReadMessage()
	if err != nil {
		return
	}
	userQuery := string(msgBytes)
	requestID := "req-" + uuid.New().String()
	options := client.StartWorkflowOptions{
		ID:        "stream-flow-" + requestID,
		TaskQueue: "agent-task-queue",
	}
	_, err = h.tClient.ExecuteWorkflow(c.Request.Context(), options, workflow.StreamMultiAgentWorkflow, userQuery, requestID, sessionID)
	if err != nil {
		ws.WriteMessage(websocket.TextMessage, []byte("Error starting workflow"))
		return
	}
	// 订阅 Redis 频道
	pubsub := h.rdb.Subscribe(c.Request.Context(), "stream:"+requestID)
	defer pubsub.Close()
	ch := pubsub.Channel()
	for msg := range ch {
		// msg.Payload 是 JSON 字符串 {"type":"token", "content":"..."}
		if err := ws.WriteMessage(websocket.TextMessage, []byte(msg.Payload)); err != nil {
			break
		}
	}
}
