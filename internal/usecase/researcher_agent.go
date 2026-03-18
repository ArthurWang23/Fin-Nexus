package usecase

import (
	"bytes"
	"context"
	"fmt"
	"go-nexus/internal/domain"
	"go-nexus/internal/infrastructure/llm"
	"go-nexus/internal/infrastructure/search"
	"text/template"

	"golang.org/x/sync/errgroup"
)

// ResearcherAgent implements domain.Agent for research tasks using
// GraphRAG (internal knowledge) and web search (external intelligence).
type ResearcherAgent struct {
	ragUC        *RAGUseCase
	searchClient *search.TavilyClient
	llmFactory   *llm.LLMFactory
	configRepo   domain.ConfigRepository
}

func NewResearcherAgent(ragUC *RAGUseCase, searchClient *search.TavilyClient, factory *llm.LLMFactory, configRepo domain.ConfigRepository) *ResearcherAgent {
	return &ResearcherAgent{ragUC: ragUC, searchClient: searchClient, llmFactory: factory, configRepo: configRepo}
}

func (r *ResearcherAgent) Card() domain.AgentCard {
	return domain.AgentCard{
		Name: "Researcher",
		Description: "首席行业分析师，掌握 GraphRAG 知识图谱(Neo4j)、研报库(Vector DB)" +
			"及联网搜索(Tavily)。擅长分析复杂商业关系、深度解读财报风险、追踪最新市场动态。不能获取实时股价数据。",
		Skills:           []string{"graphrag", "web_search", "financial_analysis", "relationship_mining"},
		ProducesOutput:   []string{"text"},
		RequiresApproval: false,
	}
}

func (r *ResearcherAgent) Execute(ctx context.Context, input domain.AgentInput) (*domain.AgentResult, error) {
	instruction := EnrichInstruction(input)
	fmt.Printf("Researcher is searching and summarizing: %s\n", instruction)

	var internalResult, webResult string
	g, gCtx := errgroup.WithContext(ctx)

	g.Go(func() error {
		client := GetLLMClient(r.llmFactory, r.configRepo, input.UserID, domain.AgentResearcher)
		var err error
		internalResult, err = r.ragUC.SearchAndChat(gCtx, instruction, input.UserID, client)
		if err != nil {
			fmt.Printf("Internal search failed: %v\n", err)
			internalResult = "内部数据库检索失败或无数据。"
		}
		return nil
	})

	g.Go(func() error {
		var err error
		webResult, err = r.searchClient.Search(gCtx, instruction)
		if err != nil {
			fmt.Printf("Web search failed: %v\n", err)
			webResult = "联网搜索失败或无结果。"
		}
		return nil
	})

	_ = g.Wait()

	tmpl := template.Must(template.New("researcher").Parse(PromptResearcher))
	var buf bytes.Buffer
	tmpl.Execute(&buf, map[string]string{
		"Query":          instruction,
		"InternalReport": internalResult,
		"WebReport":      webResult,
	})

	client := GetLLMClient(r.llmFactory, r.configRepo, input.UserID, domain.AgentResearcher)
	finalReport, err := client.Chat(ctx, []domain.Message{
		{Role: domain.RoleUser, Content: buf.String()},
	})
	if err != nil {
		return nil, err
	}

	return &domain.AgentResult{
		AgentName: "Researcher",
		Summary:   fmt.Sprintf("【研究员报告】:\n%s", finalReport),
	}, nil
}
