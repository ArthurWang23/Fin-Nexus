package activities

import (
	"context"
	"encoding/json"
	"fmt"
	"go-nexus/internal/domain"
	"go-nexus/internal/infrastructure/llm"
	"go-nexus/internal/usecase"
	"go-nexus/internal/usecase/gateway"
	"go-nexus/internal/usecase/repo"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// DynamicActivities 动态工作流 Activity
type DynamicActivities struct {
	agentUC       *usecase.AgentUseCase
	blueprintRepo domain.BlueprintRepository
	llmFactory    *llm.LLMFactory
	rdb           *redis.Client
	chatRepo      repo.ChatHistoryRepository // Redis 聊天历史
	sessionRepo   domain.SessionRepository   // PostgreSQL 会话
}

// NewDynamicActivities 创建 DynamicActivities
func NewDynamicActivities(
	agentUC *usecase.AgentUseCase,
	blueprintRepo domain.BlueprintRepository,
	llmFactory *llm.LLMFactory,
	rdb *redis.Client,
	chatRepo repo.ChatHistoryRepository,
	sessionRepo domain.SessionRepository,
) *DynamicActivities {
	return &DynamicActivities{
		agentUC:       agentUC,
		blueprintRepo: blueprintRepo,
		llmFactory:    llmFactory,
		rdb:           rdb,
		chatRepo:      chatRepo,
		sessionRepo:   sessionRepo,
	}
}

// NodeLLMConfigInput 节点级别的 LLM 配置输入（从 TS 传入）
type NodeLLMConfigInput struct {
	Provider  string `json:"provider,omitempty"`
	APIKey    string `json:"api_key,omitempty"`
	BaseURL   string `json:"base_url,omitempty"`
	ModelName string `json:"model_name,omitempty"`
}

// DynamicLLMGenerateInput LLM 生成输入参数
type DynamicLLMGenerateInput struct {
	SystemPrompt string `json:"system_prompt"`
	UserPrompt   string `json:"user_prompt"`
	ModelName    string `json:"model_name"`
	UserID       string `json:"user_id"`
	BlueprintID  string `json:"blueprint_id,omitempty"` // 可选，如果提供则使用 Blueprint 的配置
}

// DynamicLLMGenerateWithNodeConfig 使用节点级别配置的 LLM 生成
// 优先级：节点配置 > Blueprint 默认配置 > 用户默认配置
func (a *DynamicActivities) DynamicLLMGenerateWithNodeConfig(
	ctx context.Context,
	nodeConfig *NodeLLMConfigInput, // 节点级别配置（可为 nil）
	blueprintID string, // Blueprint ID（用于获取默认配置）
	systemPrompt string,
	userPrompt string,
	userId string,
) (string, error) {
	client := a.getClientWithFallback(ctx, nodeConfig, blueprintID, userId)

	msgs := []domain.Message{
		{Role: domain.RoleSystem, Content: systemPrompt},
		{Role: domain.RoleUser, Content: userPrompt},
	}
	return client.Chat(ctx, msgs)
}

// DynamicLLMGenerateStreamWithNodeConfig 使用节点级别配置的流式 LLM 生成
// msgType: 消息类型，"token" 用于正常聊天，"agent_output" 用于 Blueprint 内部节点
func (a *DynamicActivities) DynamicLLMGenerateStreamWithNodeConfig(
	ctx context.Context,
	nodeConfig *NodeLLMConfigInput, // 节点级别配置（可为 nil）
	blueprintID string, // Blueprint ID（用于获取默认配置）
	systemPrompt string,
	userPrompt string,
	streamID string,
	msgType string,
	userId string,
) (string, error) {
	client := a.getClientWithFallback(ctx, nodeConfig, blueprintID, userId)

	msgs := []domain.Message{
		{Role: domain.RoleSystem, Content: systemPrompt},
		{Role: domain.RoleUser, Content: userPrompt},
	}

	// 使用指定的消息类型发布流式事件
	eventType := msgType
	if eventType == "" {
		eventType = "token"
	}

	tokenCallback := func(token string) {
		a.PublishStreamEvent(ctx, streamID, eventType, token)
	}

	result, err := client.StreamChat(ctx, msgs, tokenCallback)
	if err != nil {
		return "", err
	}

	return result, nil
}

// DynamicRouterDecideWithNodeConfig 使用节点级别配置的路由决策
func (a *DynamicActivities) DynamicRouterDecideWithNodeConfig(
	ctx context.Context,
	nodeConfig *NodeLLMConfigInput,
	blueprintID string,
	prompt string,
	choices []string,
	input string,
	userId string,
) (string, error) {
	client := a.getClientWithFallback(ctx, nodeConfig, blueprintID, userId)

	fullPrompt := fmt.Sprintf("%s\nContext: %s\nOptions: %v\nAnswer with the option key only.", prompt, input, choices)
	msgs := []domain.Message{
		{Role: domain.RoleUser, Content: fullPrompt},
	}
	resp, err := client.Chat(ctx, msgs)
	if err != nil {
		return "", err
	}
	clean := strings.TrimSpace(resp)
	clean = strings.ToLower(clean)
	return clean, nil
}

// getClientWithFallback 获取 LLM 客户端（带优先级回退）
// 优先级：节点配置 > Blueprint 默认配置 > 用户默认配置
func (a *DynamicActivities) getClientWithFallback(
	ctx context.Context,
	nodeConfig *NodeLLMConfigInput,
	blueprintID string,
	userId string,
) gateway.LLMClient {
	// 1. 优先使用节点级别配置
	if nodeConfig != nil && nodeConfig.APIKey != "" {
		llmCfg := &gateway.LLMConfig{
			APIKey:    nodeConfig.APIKey,
			BaseURL:   nodeConfig.BaseURL,
			ModelName: nodeConfig.ModelName,
		}
		return a.llmFactory.CreateClient(llmCfg)
	}

	// 2. 尝试使用 Blueprint 默认配置
	if blueprintID != "" {
		bp, err := a.blueprintRepo.GetByIDWithDecryption(blueprintID)
		if err == nil && bp.LLMConfig.APIKey != "" {
			llmCfg := &gateway.LLMConfig{
				APIKey:    bp.LLMConfig.APIKey,
				BaseURL:   bp.LLMConfig.BaseURL,
				ModelName: bp.LLMConfig.ModelName,
			}
			// 如果节点指定了 ModelName 但没有 APIKey，使用 Blueprint 的 APIKey + 节点的 ModelName
			if nodeConfig != nil && nodeConfig.ModelName != "" {
				llmCfg.ModelName = nodeConfig.ModelName
			}
			return a.llmFactory.CreateClient(llmCfg)
		}
	}

	// 3. 回退到系统默认配置 (直接使用 Factory 默认值，不查库)
	return a.llmFactory.CreateClient(nil)
}

// PublishStreamEvent 发布流式事件到 Redis (Activity)
func (a *DynamicActivities) PublishStreamEvent(ctx context.Context, streamID string, msgType string, content string) error {
	if a.rdb == nil || streamID == "" {
		return nil
	}
	msg := usecase.StreamMessage{
		Type:    usecase.StreamEventType(msgType),
		Content: content,
	}
	bytes, _ := json.Marshal(msg)
	return a.rdb.Publish(ctx, "stream:"+streamID, bytes).Err()
}

// ResearcherSearch 执行搜索工具
func (a *DynamicActivities) ResearcherSearch(ctx context.Context, instruction string, userId string) (string, error) {
	// 复用 AgentUseCase 中的 RunResearcher 逻辑 (包含 RAG + WebSearch + Summarize)
	return a.agentUC.RunResearcher(ctx, instruction, userId)
}

// CoderRun 执行代码工具
func (a *DynamicActivities) CoderRun(ctx context.Context, instruction string, userId string) (string, error) {
	// 复用 AgentUseCase 中的 RunCoder 逻辑
	return a.agentUC.RunCoder(ctx, instruction, userId)
}

// SaveBlueprintChatTurn 保存 Blueprint 工作流的对话记录
// 同时存入 Redis（LLM 上下文）和 PostgreSQL（历史记录）
func (a *DynamicActivities) SaveBlueprintChatTurn(ctx context.Context, sessionID, userID, userQuery, finalAnswer string) error {
	if sessionID == "" {
		return nil
	}

	// 1. 保存到 Redis（用于 LLM 上下文）
	if a.chatRepo != nil {
		if err := a.chatRepo.AddMessage(ctx, sessionID, domain.Message{
			Role:    domain.RoleUser,
			Content: userQuery,
		}); err != nil {
			return err
		}
		if err := a.chatRepo.AddMessage(ctx, sessionID, domain.Message{
			Role:    domain.RoleAssistant,
			Content: finalAnswer,
		}); err != nil {
			return err
		}
	}

	// 2. 检查并创建 PostgreSQL Session
	if a.sessionRepo != nil {
		_, err := a.sessionRepo.GetSessionByID(sessionID)
		if err != nil {
			// 会话不存在，创建新会话
			newSession := &domain.ChatSession{
				ID:        sessionID,
				UserID:    userID,
				Title:     truncateBlueprintTitle(userQuery, 20),
				CreatedAt: time.Now(),
				UpdatedAt: time.Now(),
			}
			if err := a.sessionRepo.CreateSession(newSession); err != nil {
				return fmt.Errorf("failed to create session: %w", err)
			}
		}

		// 3. 保存消息到 PostgreSQL
		userMsg := &domain.ChatMessage{
			ID:        uuid.New().String(),
			SessionID: sessionID,
			Role:      "user",
			Content:   userQuery,
			CreatedAt: time.Now(),
		}
		a.sessionRepo.SaveMessage(userMsg)

		aiMsg := &domain.ChatMessage{
			ID:        uuid.New().String(),
			SessionID: sessionID,
			Role:      "assistant",
			Content:   finalAnswer,
			CreatedAt: time.Now().Add(time.Second),
		}
		a.sessionRepo.SaveMessage(aiMsg)
	}

	return nil
}

func truncateBlueprintTitle(s string, max int) string {
	if len([]rune(s)) > max {
		return string([]rune(s)[:max]) + "..."
	}
	return s
}
