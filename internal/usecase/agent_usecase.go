package usecase

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"go-nexus/internal/domain"
	"go-nexus/internal/usecase/gateway"
	"go-nexus/internal/usecase/tools"
	"go.opentelemetry.io/otel"
	"strings"
	"text/template"
)

const (
	// 主管：负责路由
	PromptSupervisor = `你是一个以结果为导向的项目主管。你有两个得力下属：
1. [Researcher]: 擅长查阅知识库文档，回答基于事实的问题。
2. [Coder]: 擅长编写 Python 代码进行数学计算、数据分析、绘图或字符串处理。

用户的请求是：{{.Query}}

请分析请求，严格按照 JSON 格式输出下一步决策。
- 如果需要查资料，NextAgent 填 "Researcher"。
- 如果需要计算或写代码，NextAgent 填 "Coder"。
- 如果任务已完成或可以直接回答，NextAgent 填 "FINISH"，并在 FinalAnswer 中回复用户。

JSON 示例:
{
    "thought": "用户想画图，但我还没有数据，需要先查数据",
    "next_agent": "Researcher",
    "instruction": "查询 VLA-0 论文中的性能数据",
    "final_answer": ""
}
`
	PromptResearcher = `你是 Researcher。你的唯一任务是利用工具查阅文档库。
请根据主管的指令行动。不要自己编造事实。查不到就说查不到。`
	PromptCoder = `你是 Coder。你的唯一任务是编写正确的 Python 代码解决问题。
代码将在沙箱中运行，请确保代码包含 print() 输出结果。`
)

// Reasoning + Acting 循环
type AgentUseCase struct {
	llm   gateway.LLMClient
	ragUC *RAGUseCase
}

type SupervisorDecision struct {
	Thought     string `json:"thought"`     // 思考过程
	NextAgent   string `json:"next_agent"`  // 下一步交给谁
	Instruction string `json:"instruction"` // 给出的下一步指令
	FinalAnswer string `json:"final_answer"`
}

func NewAgentUseCase(llm gateway.LLMClient, ragUC *RAGUseCase) *AgentUseCase {
	return &AgentUseCase{llm: llm, ragUC: ragUC}
}

func (uc *AgentUseCase) ChatWithAgent(ctx context.Context, userQuery string) (string, error) {
	tracer := otel.Tracer("go-nexus")
	ctx, span := tracer.Start(ctx, "AgentUseCase.ChatWithAgent")
	defer span.End()

	toolDefs := []gateway.ToolDefinition{
		{
			Name:        "get_order_info",
			Description: "查询电商订单的物流状态和详情",
			Parameters:  tools.GetOrderToolSchema(),
		},
		{
			Name:        "run_python_code",
			Description: "Execute Python code to perform calculations, loop logic, or text processing.",
			Parameters:  tools.GetPythonToolSchema(),
		},
	}
	history := []domain.Message{
		{Role: domain.RoleSystem, Content: "你是一个智能助手。你可以调用工具查看电商订单状态，或者编写 Python 脚本运行并解决各种问题。其他问题直接回答。"},
		{Role: domain.RoleUser, Content: userQuery},
	}

	fmt.Println("Agent: Thinking...")
	_, spanLLM := tracer.Start(ctx, "LLM.Thinking")
	resp, err := uc.llm.ChatWithTools(ctx, history, toolDefs)
	spanLLM.End()
	if err != nil {
		return "", err
	}

	// 没有处罚工具
	if len(resp.ToolCalls) == 0 {
		return resp.Content, nil
	}

	// 模拟为了保持上下文，我们需要把 LLM 的这次回复(包含ToolCall)加入历史
	// 这里简化处理：我们不把复杂的 ToolCall 结构回传，而是直接进入下一轮观察
	for _, call := range resp.ToolCalls {
		if call.Name == "get_order_info" {
			_, spanTool := tracer.Start(ctx, "Tool.GetOrderInfo")
			var args struct {
				OrderID string `json:"order_id"`
			}
			if err := json.Unmarshal([]byte(call.Args), &args); err != nil {
				return "", fmt.Errorf("invalid tool args: %v", err)
			}
			fmt.Printf("Agent: Calling Tool [GetOrderInfo] with ID: %s\n", args.OrderID)
			toolResult := tools.GetOrderInfo(args.OrderID)
			fmt.Printf(" Agent: Docker Output: [%s]\n", toolResult)
			spanTool.End()
			// 将工具结果作为一条新消息告诉 LLM
			// 提示词工程技巧：明确告诉 LLM 这是工具执行的结果
			toolFeedback := fmt.Sprintf("【工具执行结果】: %s\n请根据以上结果回答用户。", toolResult)
			history = append(history, domain.Message{
				Role:    domain.RoleUser, // 这里用 User 伪装工具结果最通用，不用担心模型对 RoleTool 的兼容性
				Content: toolFeedback,
			})
		}
		if call.Name == "run_python_code" {
			_, spanTool := tracer.Start(ctx, "Tool.RunPythonCode")
			var args struct {
				Code string `json:"code"`
			}
			// JSON Unmarshal 处理代码换行符
			if err := json.Unmarshal([]byte(call.Args), &args); err != nil {
				return "", fmt.Errorf("invalid tool args: %v", err)
			}

			// 执行 Docker 沙箱
			toolResult := tools.RunPythonCode(args.Code)
			fmt.Printf("Agent: Docker Output: [%s]\n", toolResult)
			spanTool.End()
			toolFeedback := fmt.Sprintf("【代码执行结果】:\n%s", toolResult)
			history = append(history, domain.Message{
				Role:    domain.RoleUser, // 用 User 角色注入结果最兼容
				Content: toolFeedback,
			})
		}
	}
	fmt.Println("Agent: Summarizing result...")
	_, spanFinal := tracer.Start(ctx, "LLM.Summarize")
	finalAnswer, err := uc.llm.Chat(ctx, history)
	spanFinal.End()
	return finalAnswer, err
}

func (uc *AgentUseCase) MultiAgentChat(ctx context.Context, userQuery string) (string, error) {
	history := []domain.Message{}

	maxSteps := 5
	for i := 0; i < maxSteps; i++ {
		fmt.Printf("\n--- Step %d: Supervisor is thinking ---\n", i+1)
		// make decision
		decision, err := uc.CallSupervisor(ctx, userQuery, history)
		if err != nil {
			return "", err
		}
		fmt.Printf(" Supervisor Decision: %s -> %s\n", decision.NextAgent, decision.Instruction)

		if decision.NextAgent == "FINISH" {
			return decision.FinalAnswer, nil
		}

		var workResult string
		switch decision.NextAgent {
		case "Researcher":
			workResult, err = uc.RunResearcher(ctx, decision.Instruction)
		case "Coder":
			workResult, err = uc.RunCoder(ctx, decision.Instruction)
		default:
			return "", fmt.Errorf("unknown agent: %s", decision.NextAgent)
		}

		if err != nil {
			workResult = fmt.Sprintf("Error executing task: %v", err)
		}
		history = append(history, domain.Message{
			Role:    domain.RoleUser, // 对主管来说，下属的汇报也可以看作一种输入
			Content: fmt.Sprintf("【%s 的汇报】:\n%s", decision.NextAgent, workResult),
		})
	}
	return "任务执行步骤过多，已被强制终止。", nil
}

func (uc *AgentUseCase) CallSupervisor(ctx context.Context, query string, history []domain.Message) (*SupervisorDecision, error) {
	tmpl, _ := template.New("sys").Parse(PromptSupervisor)
	var buf bytes.Buffer
	tmpl.Execute(&buf, map[string]string{"Query": query})

	msgs := []domain.Message{
		{Role: domain.RoleSystem, Content: buf.String()},
	}
	msgs = append(msgs, history...)
	// supervisor 不使用tool
	resp, err := uc.llm.Chat(ctx, msgs)
	if err != nil {
		return nil, err
	}
	cleanJSON := CleanJSONBlock(resp)

	var decision SupervisorDecision
	if err := json.Unmarshal([]byte(cleanJSON), &decision); err != nil {
		fmt.Printf("JSON Parse Error: %s\nOriginal: %s\n", err, resp)
		return nil, fmt.Errorf("supervisor output format error")
	}
	return &decision, nil
}

func (uc *AgentUseCase) RunResearcher(ctx context.Context, instruction string) (string, error) {
	fmt.Printf(" Researcher is searching and summarizing: %s\n", instruction)
	answer, err := uc.ragUC.SearchAndChat(ctx, instruction)
	if err != nil {
		return "", fmt.Errorf("researcher failed: %w", err)
	}
	return fmt.Sprintf("【研究员报告】:\n%s", answer), nil
}

func (uc *AgentUseCase) RunCoder(ctx context.Context, instruction string) (string, error) {
	fmt.Printf("Coder is working on: %s\n", instruction)
	toolsDefs := []gateway.ToolDefinition{
		{
			Name:        "run_python_code",
			Description: "Execute Python code.",
			Parameters:  tools.GetPythonToolSchema(),
		},
	}
	msgs := []domain.Message{
		{Role: domain.RoleSystem, Content: PromptCoder},
		{Role: domain.RoleUser, Content: instruction},
	}

	resp, err := uc.llm.ChatWithTools(ctx, msgs, toolsDefs)
	if err != nil {
		return "", err
	}
	if len(resp.ToolCalls) == 0 {
		return resp.Content, nil // 可能不需要写代码
	}
	// 执行工具
	var sb strings.Builder
	for _, call := range resp.ToolCalls {
		if call.Name == "run_python_code" {
			var args struct {
				Code string `json:"code"`
			}
			_ = json.Unmarshal([]byte(call.Args), &args)

			// 真正执行
			output := tools.RunPythonCode(args.Code)
			sb.WriteString(fmt.Sprintf("Code:\n%s\nOutput:\n%s\n", args.Code, output))
		}
	}
	return sb.String(), nil
}

func (uc *AgentUseCase) StreamChat(ctx context.Context, history []domain.Message, onToken func(string)) (string, error) {
	return uc.llm.StreamChat(ctx, history, onToken)
}

func CleanJSONBlock(text string) string {
	text = strings.TrimSpace(text)
	text = strings.TrimPrefix(text, "```json")
	text = strings.TrimPrefix(text, "```")
	text = strings.TrimSuffix(text, "```")
	return strings.TrimSpace(text)
}
