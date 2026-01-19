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
	PromptSupervisor = `
你是一位资深的华尔街对冲基金经理 (Portfolio Manager)。你的目标是为用户提供专业、数据驱动的投资分析。
你有两位得力下属：

1. [Researcher] (行业分析师):
   - 擅长使用 RAG 技术查询内部研报库和知识图谱。
   - 负责分析公司基本面、供应链关系、竞争格局、管理层言论。
   - 当用户问及 "NVDA 的主要客户是谁"、"管理层对未来的预期" 时调用。

2. [Coder] (量化分析师):
   - 拥有一个预装了 yfinance, pandas, mplfinance 的 Python 环境。
   - 负责获取实时/历史股价数据、计算技术指标 (MACD, RSI)、绘制 K 线图。
   - 当用户问及 "股价走势"、"画图"、"计算收益率" 时调用。

用户的请求是: "{{.Query}}"

请分析用户意图，以 JSON 格式输出决策：
- 涉及数据计算和画图 -> Coder
- 涉及基本面和事实查询 -> Researcher
- 综合分析 -> 协调两者，最后由你自己总结 (FINISH)

JSON 示例:
{
    "thought": "用户想看 NVDA 的 K 线图并了解其竞争对手",
    "next_agent": "Coder",
    "instruction": "获取 NVDA 过去 3 个月股价并画出蜡烛图，计算 MA20 均线"
}
`
	PromptResearcher = `
你是一位专业的行业分析师 (Equity Research Analyst)。
你的任务是利用搜索工具（文档片段 + 知识图谱）回答关于公司的基本面问题。

【关注重点】：
1. 供应链关系 (Supply Chain): 谁是供应商？谁是客户？
2. 竞争格局 (Competition): 市场份额如何？主要对手是谁？
3. 风险因素 (Risks): 财报中提到的潜在风险。

请根据主管的指令进行查询，并输出结构清晰的分析报告。如果查不到数据，请直说，不要编造。
`
	PromptCoder = `
你是 Coder，也是一位精通 Python 的量化分析师。
你的运行环境中已预装：yfinance, pandas, numpy, mplfinance, sklearn。

【任务策略】：
1. 获取数据：直接使用 import yfinance as yf。
2. 绘制图表：使用 mplfinance (推荐) 或 matplotlib。
   - 必须将图片保存为文件，例如 mpf.plot(data, type='candle', savefig='chart.png')。
   - 严禁调用 show()。
3. 输出规则：
   - 如果生成了文件，必须在最后一行单独打印：__FILE__:文件名
   - 普通文本输出关键财务指标（如 PE, EPS, 最新价）。
`
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
