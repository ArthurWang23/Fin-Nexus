package domain

import "context"

// Artifact represents a structured output produced by an agent.
type Artifact struct {
	Type     string `json:"type"`                // "image", "code", "table", "text"
	Content  string `json:"content,omitempty"`   // text content / code / table markdown
	FilePath string `json:"file_path,omitempty"` // file path for images etc.
	Title    string `json:"title,omitempty"`     // display title
}

// AgentResult is the structured output from any agent execution.
type AgentResult struct {
	AgentName string     `json:"agent_name"`
	StepID    int        `json:"step_id,omitempty"`
	Summary   string     `json:"summary"`
	Artifacts []Artifact `json:"artifacts,omitempty"`
	Error     string     `json:"error,omitempty"`
}

// AgentCard declares an agent's identity and capabilities.
// Used by the Planner to dynamically discover available agents.
type AgentCard struct {
	Name             string   `json:"name"`
	Description      string   `json:"description"`
	Skills           []string `json:"skills"`
	ProducesOutput   []string `json:"produces_output"`
	RequiresApproval bool     `json:"requires_approval"`
}

// AgentInput is the standardized input for agent execution.
type AgentInput struct {
	Instruction  string        `json:"instruction"`
	PriorResults []AgentResult `json:"prior_results,omitempty"`
	UserID       string        `json:"user_id"`
}

// Agent is the core interface that all worker agents implement.
type Agent interface {
	Card() AgentCard
	Execute(ctx context.Context, input AgentInput) (*AgentResult, error)
}

// ApprovableAgent extends Agent with a two-phase execution:
// Preview generates content (e.g., code) for human review,
// ExecuteApproved runs the approved content.
type ApprovableAgent interface {
	Agent
	Preview(ctx context.Context, input AgentInput) (string, error)
	ExecuteApproved(ctx context.Context, approvedContent string) (*AgentResult, error)
}
