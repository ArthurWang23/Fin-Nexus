package activities

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/redis/go-redis/v9"
	"go-nexus/internal/domain"
	"go-nexus/internal/usecase"
)

type AgentActivities struct {
	agentUC *usecase.AgentUseCase
	rdb     *redis.Client
}

type SupervisorInput struct {
	Query   string
	History []domain.Message // 需确保 domain.Message 可序列化
}

func NewAgentActivities(agentUC *usecase.AgentUseCase, rdb *redis.Client) *AgentActivities {
	return &AgentActivities{agentUC, rdb}
}

func (a *AgentActivities) SupervisorDecide(ctx context.Context, input SupervisorInput) (*usecase.SupervisorDecision, error) {
	return a.agentUC.CallSupervisor(ctx, input.Query, input.History)
}

func (a *AgentActivities) ResearcherSearch(ctx context.Context, instruction string) (string, error) {
	return a.agentUC.RunResearcher(ctx, instruction)
}

func (a *AgentActivities) CoderRun(ctx context.Context, instruction string) (string, error) {
	return a.agentUC.RunCoder(ctx, instruction)
}

func (a *AgentActivities) publish(ctx context.Context, streamID string, message usecase.StreamMessage) {
	bytes, _ := json.Marshal(message)
	a.rdb.Publish(ctx, "stream:"+streamID, bytes)
}

// SupervisorDecideStream 支持流式的决策 Activity
func (a *AgentActivities) SupervisorDecideStream(ctx context.Context, input SupervisorInput, streamID string) (*usecase.SupervisorDecision, error) {
	// 发送状态更新
	a.publish(ctx, streamID, usecase.StreamMessage{
		Type:    usecase.EventStep,
		Content: " 主管正在分析需求...",
	})
	// 2. 这里 Supervisor 主要是输出 JSON，通常不需要逐字流式展示给用户
	// 直接复用原来的 CallSupervisor 即可
	return a.agentUC.CallSupervisor(ctx, input.Query, input.History)
}

func (a *AgentActivities) WorkerRunStream(ctx context.Context, agentName, instruction, streamID string) (string, error) {
	a.publish(ctx, streamID, usecase.StreamMessage{
		Type:    usecase.EventStep,
		Content: fmt.Sprintf(" %s 正在执行: %s", agentName, instruction),
	})
	if agentName == "Researcher" {
		// 研究员可以复用 RAG，但如果我们想把 RAG 的思考过程也流式出来，
		// 需要改造 RAGUseCase。这里简单起见，Researcher 还是返回一次性结果，
		// 但我们在执行期间发个 "Searching..." 的状态
		return a.agentUC.RunResearcher(ctx, instruction)
	}
	if agentName == "Coder" {
		// Coder 类似
		return a.agentUC.RunCoder(ctx, instruction)
	}
	return "", fmt.Errorf("unknown agent")
}

func (a *AgentActivities) FinalReplyStream(ctx context.Context, history []domain.Message, streamID string) (string, error) {
	tokenCallback := func(token string) {
		a.publish(ctx, streamID, usecase.StreamMessage{Type: usecase.EventToken, Content: token})
	}
	return a.agentUC.StreamChat(ctx, history, tokenCallback)
}
