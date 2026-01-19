package usecase

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"go-nexus/internal/domain"
	"go-nexus/internal/infrastructure/graph"
	"go-nexus/internal/usecase/gateway"
	"go-nexus/internal/usecase/repo"
	"golang.org/x/sync/errgroup"
	"strings"
	"text/template"

	"github.com/google/uuid"
)

type RAGUseCase struct {
	docRepo    repo.DocumentRepository
	vectorRepo repo.VectorRepository
	llm        gateway.LLMClient
	graphRepo  *graph.Neo4jRepo
	reranker   gateway.RerankClient
}
type GraphExtractionResult struct {
	Entities  []graph.Entity   `json:"entities"`
	Relations []graph.Relation `json:"relations"`
}

func NewRAGUseCase(
	docRepo repo.DocumentRepository,
	vecRepo repo.VectorRepository,
	llm gateway.LLMClient,
	graphRepo *graph.Neo4jRepo,
	reranker gateway.RerankClient,
) *RAGUseCase {
	return &RAGUseCase{
		docRepo:    docRepo,
		vectorRepo: vecRepo,
		llm:        llm,
		graphRepo:  graphRepo,
		reranker:   reranker,
	}
}

// 简单的对话
func (uc *RAGUseCase) Chat(ctx context.Context, msg string) (string, error) {
	// 构建消息历史，for now 只发一条，后续对接 redis 历史
	history := []domain.Message{
		{Role: domain.RoleUser, Content: msg},
	}

	response, err := uc.llm.Chat(ctx, history)
	if err != nil {
		return "", fmt.Errorf("llm chat error: %w", err)
	}
	return response, nil
}

// 带有知识检索的对话
func (uc *RAGUseCase) SearchAndChat(ctx context.Context, query string) (string, error) {
	var g errgroup.Group
	var finalContexts []string
	var entities []string

	g.Go(func() error {
		var chunks []*domain.DocumentChunk
		vectors, err := uc.llm.Embed(ctx, []string{query})
		if err != nil {
			return fmt.Errorf("llm embed error: %w", err)
		}
		chunks, err = uc.vectorRepo.SearchSimilar(ctx, vectors[0], 5)
		if err != nil {
			return fmt.Errorf("vector search error: %w", err)
		}
		var candidates []string
		for _, chunk := range chunks {
			candidates = append(candidates, chunk.Content)
		}
		fmt.Printf(" Reranking %d documents using Jina...\n", len(chunks))
		rankedResults, err := uc.reranker.Rerank(ctx, query, candidates, 2)
		if err != nil {
			fmt.Printf("Jina Rerank failed: %v, fallback to vector sort\n", err)
			// 降级：直接取向量前5
			for i, t := range candidates {
				if i >= 5 {
					break
				}
				finalContexts = append(finalContexts, t)
			}
		} else {
			for _, res := range rankedResults {
				fmt.Printf("   [Score: %.4f] %s...\n", res.Score, res.Document[:20])
				if res.Score > 0.2 {
					finalContexts = append(finalContexts, res.Document)
				}
			}
		}
		return err
	})

	g.Go(func() error {
		var err error
		entities, err = uc.extractEntities(ctx, query)
		return err
	})
	if err := g.Wait(); err != nil {
		return "", err
	}
	var graphContext []string
	fmt.Printf(" Extracted Entities: %v\n", entities)
	for _, entity := range entities {
		// 查每个实体的一跳邻居
		knowledge, err := uc.graphRepo.GetRelatedKnowledge(ctx, entity)
		if err != nil {
			continue
		}
		// 去重合并
		graphContext = append(graphContext, knowledge...)
	}
	graphContext = uniqueStrings(graphContext)

	var contextBuilder strings.Builder
	contextBuilder.WriteString("【文档片段】:\n")
	for _, c := range finalContexts {
		contextBuilder.WriteString(c + "\n---\n")
	}
	if len(graphContext) > 0 {
		contextBuilder.WriteString("\n【知识图谱信息】:\n")
		// 最多只取前 15 条关系，防止撑爆 Token
		limit := 15
		if len(graphContext) < limit {
			limit = len(graphContext)
		}

		for _, k := range graphContext[:limit] {
			contextBuilder.WriteString("- " + k + "\n")
		}
	}
	fmt.Printf(" Final Context: Graph Nodes=%d, Vector Chunks=%d\n", len(graphContext), len(finalContexts))
	systemPrompt := fmt.Sprintf(`你是一个智能助手。请结合以下资料回答问题。
优先基于【知识图谱信息】理清实体间的关系，再结合【文档片段】补充细节。

参考资料:
%s`, contextBuilder.String())

	history := []domain.Message{
		{Role: domain.RoleSystem, Content: systemPrompt},
		{Role: domain.RoleUser, Content: query},
	}
	return uc.llm.Chat(ctx, history)
}

func (uc *RAGUseCase) AddDocumentText(ctx context.Context, text string, filename string) error {
	// 创建文档记录
	docID := uuid.New().String()
	doc := domain.Document{
		ID:     docID,
		Type:   "text",
		Name:   filename,
		Status: domain.StatusProcessing,
	}
	if err := uc.docRepo.Create(ctx, &doc); err != nil {
		return err
	}
	chunks := uc.smartSplit(docID, text, filename, 500)

	// 计算向量
	var texts []string
	for _, c := range chunks {
		texts = append(texts, c.Content)
	}
	vectors, err := uc.llm.Embed(ctx, texts)
	if err != nil {
		return fmt.Errorf("embedding failed: %w", err)
	}

	if len(chunks) != len(vectors) {
		return fmt.Errorf("vector count mismatch")
	}
	for i, vec := range vectors {
		chunks[i].Vector = vec
	}

	if err := uc.vectorRepo.StoreChunks(ctx, chunks); err != nil {
		return err
	}
	return uc.docRepo.UpdateStatus(ctx, docID, domain.StatusIndexed)
}

func (uc *RAGUseCase) SearchOnly(ctx context.Context, query string) (string, error) {
	vectors, _ := uc.llm.Embed(ctx, []string{query})
	chunks, _ := uc.vectorRepo.SearchSimilar(ctx, vectors[0], 5)

	if len(chunks) == 0 {
		return "没有找到相关资料。", nil
	}
	var buf strings.Builder
	for _, c := range chunks {
		buf.WriteString(c.Content)
		buf.WriteString("\n---\n")
	}
	return buf.String(), nil
}

func (uc *RAGUseCase) BuildGraphFromText(ctx context.Context, text string) error {
	// 渲染 Prompt
	tmpl, err := template.New("graph").Parse(PromptExtractGraph)
	if err != nil {
		return fmt.Errorf("parse template error: %w", err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, map[string]string{"Text": text}); err != nil {
		return fmt.Errorf("execute template error: %w", err)
	}

	// 调用 LLM
	msgs := []domain.Message{
		{Role: domain.RoleUser, Content: buf.String()},
	}
	resp, err := uc.llm.Chat(ctx, msgs)
	if err != nil {
		return fmt.Errorf("llm chat error: %w", err)
	}
	cleanJSON := CleanJSONBlock(resp)
	var result GraphExtractionResult
	if err := json.Unmarshal([]byte(cleanJSON), &result); err != nil {
		fmt.Printf(" Graph JSON Parse Error: %v\nResp: %s\n", err, resp)
		return nil
	}
	if len(result.Entities) > 0 || len(result.Relations) > 0 {
		err = uc.graphRepo.SaveKnowledgeGraph(ctx, result.Entities, result.Relations)
		if err != nil {
			return fmt.Errorf("neo4j store failed: %w", err)
		}
	}
	return nil
}

// 按行分隔并合并
func (uc *RAGUseCase) smartSplit(docID, text, filename string, chunkSize int) []*domain.DocumentChunk {
	var chunks []*domain.DocumentChunk

	lines := strings.Split(text, "\n")
	var currentChunk strings.Builder
	prefix := fmt.Sprintf("《来源文档：%s》\n", filename)
	currentChunk.WriteString(prefix)
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if currentChunk.Len()+len(line) > chunkSize {
			chunks = append(chunks, &domain.DocumentChunk{
				ID:         uuid.New().String(),
				DocumentID: docID,
				Content:    currentChunk.String(),
				PageNumber: 1, // 暂未处理页码
			})
			currentChunk.Reset()
			currentChunk.WriteString(prefix) // 新块也要加前缀！
		}
		currentChunk.WriteString(line)
		currentChunk.WriteString("\n")
	}
	if currentChunk.Len() > len(prefix) {
		chunks = append(chunks, &domain.DocumentChunk{
			ID:         uuid.New().String(),
			DocumentID: docID,
			Content:    currentChunk.String(),
			PageNumber: 1,
		})
	}
	return chunks
}

// extractEntities 利用 LLM 从用户问题中提取搜索关键词
func (uc *RAGUseCase) extractEntities(ctx context.Context, query string) ([]string, error) {
	// 1. 渲染 Prompt
	tmpl, _ := template.New("query_extract").Parse(PromptQueryEntityExtraction)
	var buf bytes.Buffer
	tmpl.Execute(&buf, map[string]string{"Query": query})

	// 2. 调用 LLM
	// 这里建议用 Temperature = 0，保证结果稳定
	// 但我们的接口目前没有暴露 Temperature 参数，暂时用默认的
	history := []domain.Message{
		{Role: domain.RoleUser, Content: buf.String()},
	}

	resp, err := uc.llm.Chat(ctx, history)
	if err != nil {
		return nil, err
	}

	// 3. 清洗 & 解析 JSON
	cleanResp := CleanJSONBlock(resp)

	var keywords []string
	if err := json.Unmarshal([]byte(cleanResp), &keywords); err != nil {
		// 如果解析失败，回退策略：直接把原始 query 当作关键词
		// 或者尝试简单的 split
		fmt.Printf(" Query extraction JSON error: %v. Raw: %s\n", err, resp)
		return []string{query}, nil
	}

	return keywords, nil
}

func uniqueStrings(input []string) []string {
	keys := make(map[string]bool)
	list := []string{}
	for _, entry := range input {
		if _, value := keys[entry]; !value {
			keys[entry] = true
			list = append(list, entry)
		}
	}
	return list
}
