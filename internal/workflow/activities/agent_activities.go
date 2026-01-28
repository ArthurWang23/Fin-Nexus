package activities

import (
	"context"
	"encoding/json"
	"fmt"
	"go-nexus/internal/domain"
	"go-nexus/internal/usecase"
	"go-nexus/internal/usecase/repo"
	"time"

	"github.com/google/uuid"

	"github.com/redis/go-redis/v9"
)

type AgentActivities struct {
	agentUC     *usecase.AgentUseCase
	rdb         *redis.Client
	chatRepo    repo.ChatHistoryRepository
	sessionRepo domain.SessionRepository
}

type SupervisorInput struct {
	Query   string
	History []domain.Message // 需确保 domain.Message 可序列化
	UserID  string           // 根据 UserID 获取 LLMClient配置
}

// SaveChatInput 保存本轮对话,需同时存 redis （llm context）和 postgres （历史记录）
type SaveChatInput struct {
	SessionID   string
	UserID      string // 区分用户
	UserQuery   string
	FinalAnswer string
}

func NewAgentActivities(agentUC *usecase.AgentUseCase, rdb *redis.Client, chatRepo repo.ChatHistoryRepository, sessionRepo domain.SessionRepository) *AgentActivities {
	return &AgentActivities{agentUC, rdb, chatRepo, sessionRepo}
}

func (a *AgentActivities) SupervisorDecide(ctx context.Context, input SupervisorInput) (*usecase.SupervisorDecision, error) {
	return a.agentUC.CallSupervisor(ctx, input.Query, input.UserID, input.History)
}

func (a *AgentActivities) ResearcherSearch(ctx context.Context, instruction string, userID string) (string, error) {
	return a.agentUC.RunResearcher(ctx, instruction, userID)
}

func (a *AgentActivities) CoderRun(ctx context.Context, instruction string, userID string) (string, error) {
	return a.agentUC.RunCoder(ctx, instruction, userID)
}

func (a *AgentActivities) publish(ctx context.Context, streamID string, message usecase.StreamMessage) {
	bytes, _ := json.Marshal(message)
	a.rdb.Publish(ctx, "stream:"+streamID, bytes)
}

// SupervisorDecideStream 支持流式的决策 Activity
func (a *AgentActivities) SupervisorDecideStream(ctx context.Context, input SupervisorInput, streamID string) (*usecase.SupervisorDecision, error) {
	// 发送状态更新
	a.publish(ctx, streamID, usecase.StreamMessage{
		Type:    usecase.EventStep,
		Content: " 主管正在分析需求...",
	})
	// 2. 这里 Supervisor 主要是输出 JSON，通常不需要逐字流式展示给用户
	// 直接复用原来的 CallSupervisor 即可
	return a.agentUC.CallSupervisor(ctx, input.Query, input.UserID, input.History)
}

func (a *AgentActivities) WorkerRunStream(ctx context.Context, agentName, instruction, streamID string, userID string) (string, error) {
	a.publish(ctx, streamID, usecase.StreamMessage{
		Type:    usecase.EventStep,
		Content: fmt.Sprintf(" %s 正在执行: %s", agentName, instruction),
	})
	if agentName == "Researcher" {
		// 研究员可以复用 RAG，但如果我们想把 RAG 的思考过程也流式出来，
		// 需要改造 RAGUseCase。这里简单起见，Researcher 还是返回一次性结果，
		// 但我们在执行期间发个 "Searching..." 的状态
		return a.agentUC.RunResearcher(ctx, instruction, userID)
	}
	if agentName == "Coder" {
		// Coder 类似
		return a.agentUC.RunCoder(ctx, instruction, userID)
	}
	return "", fmt.Errorf("unknown agent")
}

func (a *AgentActivities) FinalReplyStream(ctx context.Context, history []domain.Message, streamID string, userID string) (string, error) {
	supervisorClient := a.agentUC.GetClientForAgent(ctx, userID, domain.AgentSupervisor)
	tokenCallback := func(token string) {
		a.publish(ctx, streamID, usecase.StreamMessage{Type: usecase.EventToken, Content: token})
	}
	return a.agentUC.StreamChat(ctx, history, tokenCallback, supervisorClient)
}

func (a *AgentActivities) LoadChatHistory(ctx context.Context, sessionID string) ([]domain.Message, error) {
	if sessionID == "" {
		return []domain.Message{}, nil
	}
	return a.chatRepo.GetHistory(ctx, sessionID, 10)
}

func (a *AgentActivities) SaveChatTurn(ctx context.Context, input SaveChatInput) error {
	if input.SessionID == "" {
		return nil
	}
	err := a.chatRepo.AddMessage(ctx, input.SessionID, domain.Message{
		Role:    domain.RoleUser,
		Content: input.UserQuery,
	})
	if err != nil {
		return err
	}
	err = a.chatRepo.AddMessage(ctx, input.SessionID, domain.Message{Role: domain.RoleAssistant, Content: input.FinalAnswer})

	_, err = a.sessionRepo.GetSessionByID(input.SessionID)
	if err != nil {
		// 假设找不到就是不存在 (Gorm error handling 简化处理)
		// 创建新 Session
		newSession := &domain.ChatSession{
			ID:        input.SessionID,
			UserID:    input.UserID,
			Title:     truncateString(input.UserQuery, 20),
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}
		if err := a.sessionRepo.CreateSession(newSession); err != nil {
			return fmt.Errorf("failed to create session: %w", err)
		}
	}
	userMsg := &domain.ChatMessage{
		ID:        uuid.New().String(),
		SessionID: input.SessionID,
		Role:      "user",
		Content:   input.UserQuery,
		CreatedAt: time.Now(),
	}
	a.sessionRepo.SaveMessage(userMsg)
	aiMsg := &domain.ChatMessage{
		ID:        uuid.New().String(),
		SessionID: input.SessionID,
		Role:      "assistant",
		Content:   input.FinalAnswer,
		CreatedAt: time.Now().Add(time.Second),
	}
	a.sessionRepo.SaveMessage(aiMsg)
	return nil
}

func truncateString(s string, max int) string {
	if len([]rune(s)) > max {
		return string([]rune(s)[:max]) + "..."
	}
	return s
}
