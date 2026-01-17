package usecase

import (
	"context"
	"fmt"
	"go-nexus/internal/domain"
	"go-nexus/internal/usecase/gateway"
	"go-nexus/internal/usecase/repo"
	"strings"

	"github.com/google/uuid"
)

type RAGUseCase struct {
	docRepo    repo.DocumentRepository
	vectorRepo repo.VectorRepository
	llm        gateway.LLMClient
}

func NewRAGUseCase(
	docRepo repo.DocumentRepository,
	vecRepo repo.VectorRepository,
	llm gateway.LLMClient,
) *RAGUseCase {
	return &RAGUseCase{
		docRepo:    docRepo,
		vectorRepo: vecRepo,
		llm:        llm,
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
	vectors, err := uc.llm.Embed(ctx, []string{query})
	if err != nil || len(vectors) == 0 {
		return "", fmt.Errorf("embedding error: %w", err)
	}
	queryVector := vectors[0]

	chunks, err := uc.vectorRepo.SearchSimilar(ctx, queryVector, 3)
	if err != nil {
		return "", fmt.Errorf("vector search error: %w", err)
	}
	// 🔥 DEBUG 重点：打印检索到的块
	fmt.Printf("🔍 [Debug] User Query: %s\n", query)
	fmt.Printf("🔍 [Debug] Found %d chunks\n", len(chunks))

	contextText := ""
	for i, chunk := range chunks {
		// 打印每个块的内容，确认是否有意义
		fmt.Printf("   Chunk %d: %s (ID: %s)\n", i, chunk.Content[:min(50, len(chunk.Content))], chunk.ID)
		contextText += chunk.Content + "\n---\n"
	}

	// 如果没有找到块，或者内容为空
	if contextText == "" {
		fmt.Println("⚠️ [Debug] Retrieval result is EMPTY. LLM will likely say 'I don't know'.")
	}
	// 构建提示词 (Prompt Engineering)
	// 将检索到的知识拼接给 AI
	for _, chunk := range chunks {
		contextText += chunk.Content + "\n---\n"
	}
	systemPrompt := fmt.Sprintf(`你是一个智能助手。请根据以下参考资料回答用户问题。如果资料中没有答案，请说不知道。
	参考资料:
	%s`, contextText)

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
