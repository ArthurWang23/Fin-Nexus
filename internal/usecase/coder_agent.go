package usecase

import (
	"context"
	"encoding/json"
	"fmt"
	"go-nexus/internal/domain"
	"go-nexus/internal/infrastructure/llm"
	"go-nexus/internal/usecase/gateway"
	"go-nexus/internal/usecase/skills"
	"go-nexus/internal/usecase/tools"
	"strings"
)

// CoderAgent implements domain.ApprovableAgent with a two-phase flow:
// Preview (generate code) → human review → ExecuteApproved (run code).
type CoderAgent struct {
	llmFactory *llm.LLMFactory
	configRepo domain.ConfigRepository
}

func NewCoderAgent(factory *llm.LLMFactory, configRepo domain.ConfigRepository) *CoderAgent {
	return &CoderAgent{llmFactory: factory, configRepo: configRepo}
}

func (c *CoderAgent) Card() domain.AgentCard {
	return domain.AgentCard{
		Name: "Coder",
		Description: "首席量化工程师，拥有 Python 沙箱（yfinance, yahooquery, mplfinance, " +
			"GoogleNews, textblob, pandas, numpy, matplotlib, scipy, scikit-learn）。" +
			"擅长获取实时数据、绘图、计算技术指标、舆情分析。不能查 GraphRAG 知识图谱。",
		Skills:           []string{"python", "yfinance", "charting", "calculation", "sentiment"},
		ProducesOutput:   []string{"image", "code", "text"},
		RequiresApproval: true,
	}
}

// Execute runs the full generate + execute cycle (used in non-approval flows).
func (c *CoderAgent) Execute(ctx context.Context, input domain.AgentInput) (*domain.AgentResult, error) {
	code, err := c.Preview(ctx, input)
	if err != nil {
		return nil, err
	}
	if code == "" {
		return &domain.AgentResult{AgentName: "Coder", Summary: "No code needed for this task."}, nil
	}
	return c.ExecuteApproved(ctx, code)
}

// Preview asks the LLM to generate Python code without executing it.
func (c *CoderAgent) Preview(ctx context.Context, input domain.AgentInput) (string, error) {
	instruction := EnrichInstruction(input)
	fmt.Printf("Coder is generating code for: %s\n", instruction)

	matched := skills.MatchSkills(instruction, 2)
	systemPrompt := PromptCoder
	if len(matched) > 0 {
		names := make([]string, len(matched))
		for i, s := range matched {
			names[i] = s.Name
		}
		fmt.Printf("Matched skills: %v\n", names)
		systemPrompt += skills.FormatForPrompt(matched)
	}

	toolsDefs := []gateway.ToolDefinition{
		{Name: "run_python_code", Description: "Execute Python code.", Parameters: tools.GetPythonToolSchema()},
	}
	msgs := []domain.Message{
		{Role: domain.RoleSystem, Content: systemPrompt},
		{Role: domain.RoleUser, Content: instruction},
	}

	client := GetLLMClient(c.llmFactory, c.configRepo, input.UserID, domain.AgentCoder)
	resp, err := client.ChatWithTools(ctx, msgs, toolsDefs)
	if err != nil {
		return "", err
	}
	if len(resp.ToolCalls) == 0 {
		return "", nil
	}
	for _, call := range resp.ToolCalls {
		if call.Name == "run_python_code" {
			var args struct {
				Code string `json:"code"`
			}
			if err := json.Unmarshal([]byte(call.Args), &args); err == nil && args.Code != "" {
				return args.Code, nil
			}
		}
	}
	return "", nil
}

// ExecuteApproved runs pre-approved Python code in the Docker sandbox
// and returns structured results with separate Artifacts.
func (c *CoderAgent) ExecuteApproved(ctx context.Context, approvedContent string) (*domain.AgentResult, error) {
	fmt.Printf("Coder is executing approved code...\n")
	output, files := tools.RunPythonCode(approvedContent)

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Code:\n%s\nOutput:\n%s\n", approvedContent, output))
	if len(files) > 0 {
		sb.WriteString("\n📊 Generated Files:\n")
		for _, f := range files {
			sb.WriteString(fmt.Sprintf("![chart](%s)\n", f))
		}
	}

	result := &domain.AgentResult{
		AgentName: "Coder",
		Summary:   sb.String(),
	}
	for _, f := range files {
		result.Artifacts = append(result.Artifacts, domain.Artifact{
			Type:     "image",
			FilePath: f,
		})
	}
	return result, nil
}
