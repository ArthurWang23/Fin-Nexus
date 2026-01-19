package usecase

type StreamEventType string

const (
	EventStep   StreamEventType = "step"   // 步骤更新 (e.g., "Supervisor Thinking")
	EventToken  StreamEventType = "token"  // LLM 生成的字
	EventError  StreamEventType = "error"  // 错误
	EventFinish StreamEventType = "finish" // 结束
)

type StreamMessage struct {
	Type    StreamEventType `json:"type"`
	Content string          `json:"content"`
}
