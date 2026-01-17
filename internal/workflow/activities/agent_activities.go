package activities

import (
	"context"
	"go-nexus/internal/domain"
	"go-nexus/internal/usecase"
)

type AgentActivities struct {
	agentUC *usecase.AgentUseCase
}

type SupervisorInput struct {
	Query   string
	History []domain.Message // 需确保 domain.Message 可序列化
}

func NewAgentActivities(agentUC *usecase.AgentUseCase) *AgentActivities {
	return &AgentActivities{agentUC}
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
