package usecase

import (
	"fmt"
	"go-nexus/internal/domain"
	"go-nexus/internal/infrastructure/llm"
	"go-nexus/internal/usecase/gateway"
	"strings"
)

// AgentRegistry holds all registered agents and provides dynamic discovery.
type AgentRegistry struct {
	agents map[string]domain.Agent
}

func NewAgentRegistry() *AgentRegistry {
	return &AgentRegistry{agents: make(map[string]domain.Agent)}
}

func (r *AgentRegistry) Register(agent domain.Agent) {
	r.agents[agent.Card().Name] = agent
}

func (r *AgentRegistry) Get(name string) (domain.Agent, bool) {
	a, ok := r.agents[name]
	return a, ok
}

func (r *AgentRegistry) AllCards() []domain.AgentCard {
	cards := make([]domain.AgentCard, 0, len(r.agents))
	for _, a := range r.agents {
		cards = append(cards, a.Card())
	}
	return cards
}

// BuildAgentDescriptions generates a prompt fragment listing all registered
// agents and their capabilities, for dynamic injection into the Planner prompt.
func (r *AgentRegistry) BuildAgentDescriptions() string {
	var sb strings.Builder
	for i, card := range r.AllCards() {
		sb.WriteString(fmt.Sprintf("%d. **%s**: %s\n", i+1, card.Name, card.Description))
		if len(card.ProducesOutput) > 0 {
			sb.WriteString(fmt.Sprintf("   可产出: %s\n", strings.Join(card.ProducesOutput, ", ")))
		}
	}
	return sb.String()
}

// GetLLMClient creates an LLM client with user-specific config if available.
func GetLLMClient(factory *llm.LLMFactory, configRepo domain.ConfigRepository, userID string, agentType domain.AgentType) gateway.LLMClient {
	userConfig, err := configRepo.GetConfig(userID, agentType)
	var cfg *gateway.LLMConfig
	if err == nil && userConfig != nil {
		cfg = &gateway.LLMConfig{
			APIKey:    userConfig.APIKey,
			BaseURL:   userConfig.BaseURL,
			ModelName: userConfig.ModelName,
		}
	}
	return factory.CreateClient(cfg)
}

// EnrichInstruction appends prior step results to the instruction
// so dependent agents can reference earlier work without re-fetching data.
func EnrichInstruction(input domain.AgentInput) string {
	if len(input.PriorResults) == 0 {
		return input.Instruction
	}
	var sb strings.Builder
	sb.WriteString(input.Instruction)
	sb.WriteString("\n\n【前置步骤结果（供参考，请基于这些数据工作，避免重复获取）】:\n")
	for _, r := range input.PriorResults {
		sb.WriteString(fmt.Sprintf("─── %s (Step %d) ───\n%s\n\n", r.AgentName, r.StepID, r.Summary))
	}
	return sb.String()
}
