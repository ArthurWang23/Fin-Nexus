package usecase

import (
	"context"
	"fmt"
	"github.com/google/uuid"
	"go-nexus/internal/domain"
	"go-nexus/internal/usecase/gateway"
	"go-nexus/internal/usecase/repo"
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

	// 构建提示词 (Prompt Engineering)
	// 将检索到的知识拼接给 AI
	contextText := ""
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

func (uc *RAGUseCase) AddDocumentText(ctx context.Context, text string) error {
	// 创建文档记录
	docID := uuid.New().String()
	doc := domain.Document{
		ID:     docID,
		Type:   "text",
		Name:   "Manual Text Upload",
		Status: domain.StatusProcessing,
	}
	if err := uc.docRepo.Create(ctx, &doc); err != nil {
		return err
	}

	const chunkSize = 200
	var chunks []*domain.DocumentChunk

	runes := []rune(text)
	for i := 0; i < len(runes); i += chunkSize {
		end := i + chunkSize
		if end > len(runes) {
			end = len(runes)
		}
		content := string(runes[i:end])
		chunks = append(chunks, &domain.DocumentChunk{
			ID:         uuid.New().String(),
			DocumentID: docID,
			Content:    content,
			PageNumber: 1,
		})
	}

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
