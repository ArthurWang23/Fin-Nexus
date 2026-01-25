package usecase

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"go-nexus/internal/domain"
	"go-nexus/internal/usecase/gateway"
	"go-nexus/internal/usecase/repo"
	"go-nexus/internal/usecase/tools"
	"strings"
	"text/template"
	"time"

	"go.opentelemetry.io/otel"
)

// Reasoning + Acting 循环
type AgentUseCase struct {
	llm      gateway.LLMClient
	ragUC    *RAGUseCase
	chatRepo repo.ChatHistoryRepository
}

type SupervisorDecision struct {
	Thought     string `json:"thought"`     // 思考过程
	NextAgent   string `json:"next_agent"`  // 下一步交给谁
	Instruction string `json:"instruction"` // 给出的下一步指令
	FinalAnswer string `json:"final_answer"`
}

func NewAgentUseCase(llm gateway.LLMClient, ragUC *RAGUseCase, chatRepo repo.ChatHistoryRepository) *AgentUseCase {
	return &AgentUseCase{llm: llm, ragUC: ragUC, chatRepo: chatRepo}
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
			toolResult, _ := tools.RunPythonCode(args.Code)
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

func (uc *AgentUseCase) MultiAgentChat(ctx context.Context, userQuery string, sessionID string, userID string) (string, error) {
	var contextHistory []domain.Message
	if sessionID != "" {
		var err error
		contextHistory, err = uc.chatRepo.GetHistory(ctx, sessionID, 10)
		if err != nil {
			fmt.Printf(" Failed to load history: %v\n", err)
		}
	}
	currentExecutionHistory := make([]domain.Message, len(contextHistory))
	copy(currentExecutionHistory, contextHistory)

	maxSteps := 5
	finalAnswer := "任务执行步骤过多，已被强制终止。"
	for i := 0; i < maxSteps; i++ {
		fmt.Printf("\n--- Step %d: Supervisor is thinking ---\n", i+1)
		// make decision
		decision, err := uc.CallSupervisor(ctx, userQuery, currentExecutionHistory)
		if err != nil {
			return "", err
		}
		fmt.Printf(" Supervisor Decision: %s -> %s\n", decision.NextAgent, decision.Instruction)

		if decision.NextAgent == "FINISH" {
			finalAnswer = decision.FinalAnswer
			break
		}

		var workResult string
		switch decision.NextAgent {
		case "Researcher":
			workResult, err = uc.RunResearcher(ctx, decision.Instruction, userID)
		case "Coder":
			workResult, err = uc.RunCoder(ctx, decision.Instruction)
		default:
			return "", fmt.Errorf("unknown agent: %s", decision.NextAgent)
		}

		if err != nil {
			workResult = fmt.Sprintf("Error executing task: %v", err)
		}

		currentExecutionHistory = append(currentExecutionHistory, domain.Message{
			Role:    domain.RoleUser,
			Content: fmt.Sprintf("【%s 汇报】:\n%s", decision.NextAgent, workResult),
		})
	}
	if sessionID != "" {
		// 保存问题与回答
		uc.chatRepo.AddMessage(ctx, sessionID, domain.Message{Role: domain.RoleUser, Content: userQuery})
		uc.chatRepo.AddMessage(ctx, sessionID, domain.Message{Role: domain.RoleAssistant, Content: finalAnswer})
	}
	return finalAnswer, nil
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

func (uc *AgentUseCase) RunResearcher(ctx context.Context, instruction string, userID string) (string, error) {
	fmt.Printf(" Researcher is searching and summarizing: %s\n", instruction)
	answer, err := uc.ragUC.SearchAndChat(ctx, instruction, userID, PromptResearcher)
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
			output, _ := tools.RunPythonCode(args.Code)
			sb.WriteString(fmt.Sprintf("Code:\n%s\nOutput:\n%s\n", args.Code, output))
		}
	}
	return sb.String(), nil
}

func (uc *AgentUseCase) StreamChat(ctx context.Context, history []domain.Message, onToken func(string)) (string, error) {
	return uc.llm.StreamChat(ctx, history, onToken)
}

func (uc *AgentUseCase) IngestKnowledge(ctx context.Context, text, filename string) error {
	// RAG部分全部存
	if err := uc.ragUC.AddDocumentText(ctx, text, filename, "system"); err != nil {
		return err
	}
	// 提取出长期信息存入图谱
	graphText := extractGraphSafeContent(text)
	if graphText != "" {
		if len(graphText) > 4000 {
			graphText = graphText[:4000]
		}
		if err := uc.ragUC.BuildGraphFromText(ctx, graphText, "system"); err != nil {
			fmt.Printf("Graph extraction warning: %v\n", err)
		}
	}
	return nil
}

func (uc *AgentUseCase) GenerateMorningBrief(ctx context.Context, ticker, rawData string) (string, error) {
	tmpl, _ := template.New("brief").Parse(PromptGenerateBrief)
	var buf bytes.Buffer

	// 注入当前日期，解决“5分钟前”的问题
	today := time.Now().Format("2006-01-02")

	tmpl.Execute(&buf, map[string]string{
		"Date":    today,
		"Ticker":  ticker,
		"RawData": rawData,
	})
	history := []domain.Message{
		{Role: domain.RoleSystem, Content: "你是一个金融主编。"},
		{Role: domain.RoleUser, Content: buf.String()},
	}
	return uc.llm.Chat(ctx, history)
}

func CleanJSONBlock(text string) string {
	text = strings.TrimSpace(text)
	text = strings.TrimPrefix(text, "```json")
	text = strings.TrimPrefix(text, "```")
	text = strings.TrimSuffix(text, "```")
	return strings.TrimSpace(text)
}

func extractGraphSafeContent(text string) string {
	startMarker := "=== [GRAPH_SAFE_START] ==="
	endMarker := "=== [GRAPH_SAFE_END] ==="

	startIndex := strings.Index(text, startMarker)
	endIndex := strings.Index(text, endMarker)

	if startIndex != -1 && endIndex != -1 && endIndex > startIndex {
		// 返回标记中间的内容
		return text[startIndex+len(startMarker) : endIndex]
	}
	// 找不到标记就不存graph防止污染
	return ""
}
