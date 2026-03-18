package usecase

type StreamEventType string

const (
	EventStep             StreamEventType = "step"              // 步骤更新 (e.g., "Supervisor Thinking")
	EventToken            StreamEventType = "token"             // LLM 生成的字
	EventError            StreamEventType = "error"             // 错误
	EventDone             StreamEventType = "done"              // 工作流完成，通知前端重置状态
	EventApprovalRequired StreamEventType = "approval_required" // 需要人工审批（含待审批内容）
	EventPlan             StreamEventType = "plan"              // 完整执行计划（Plan-Execute 模式）
	EventStepComplete     StreamEventType = "step_complete"     // 计划中某步骤完成
	EventArtifacts        StreamEventType = "artifacts"         // 结构化产出物（图片、代码等）
)

type StreamMessage struct {
	Type    StreamEventType `json:"type"`
	Content string          `json:"content"`
}
