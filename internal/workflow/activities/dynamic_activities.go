package activities

import (
	"context"
	"encoding/json"
	"fmt"
	"go-nexus/internal/domain"
	"go-nexus/internal/infrastructure/llm"
	"go-nexus/internal/usecase"
	"go-nexus/internal/usecase/gateway"
	"strings"

	"github.com/redis/go-redis/v9"
)

// DynamicActivities 动态工作流 Activity
type DynamicActivities struct {
	agentUC       *usecase.AgentUseCase
	blueprintRepo domain.BlueprintRepository
	llmFactory    *llm.LLMFactory
	rdb           *redis.Client
}

// NewDynamicActivities 创建 DynamicActivities
func NewDynamicActivities(
	agentUC *usecase.AgentUseCase,
	blueprintRepo domain.BlueprintRepository,
	llmFactory *llm.LLMFactory,
	rdb *redis.Client,
) *DynamicActivities {
	return &DynamicActivities{
		agentUC:       agentUC,
		blueprintRepo: blueprintRepo,
		llmFactory:    llmFactory,
		rdb:           rdb,
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

// DynamicLLMGenerate 通用 LLM 生成（支持 Blueprint 配置）
func (a *DynamicActivities) DynamicLLMGenerate(
	ctx context.Context,
	systemPrompt string,
	userPrompt string,
	modelName string,
	userId string,
) (string, error) {
	// 使用用户配置或默认配置
	client := a.agentUC.GetClientForAgent(ctx, userId, domain.AgentDynamic)
	msgs := []domain.Message{
		{Role: domain.RoleSystem, Content: systemPrompt},
		{Role: domain.RoleUser, Content: userPrompt},
	}
	return client.Chat(ctx, msgs)
}

// DynamicLLMGenerateWithBlueprint 使用 Blueprint 配置的 LLM 生成
func (a *DynamicActivities) DynamicLLMGenerateWithBlueprint(
	ctx context.Context,
	blueprintID string,
	systemPrompt string,
	userPrompt string,
	userId string,
) (string, error) {
	client, err := a.getClientFromBlueprint(ctx, blueprintID, userId)
	if err != nil {
		return "", fmt.Errorf("failed to get LLM client: %w", err)
	}

	msgs := []domain.Message{
		{Role: domain.RoleSystem, Content: systemPrompt},
		{Role: domain.RoleUser, Content: userPrompt},
	}
	return client.Chat(ctx, msgs)
}

// DynamicLLMGenerateStream 流式 LLM 生成（支持 Blueprint 配置）
func (a *DynamicActivities) DynamicLLMGenerateStream(
	ctx context.Context,
	blueprintID string,
	systemPrompt string,
	userPrompt string,
	streamID string,
	userId string,
) (string, error) {
	client, err := a.getClientFromBlueprint(ctx, blueprintID, userId)
	if err != nil {
		return "", fmt.Errorf("failed to get LLM client: %w", err)
	}

	msgs := []domain.Message{
		{Role: domain.RoleSystem, Content: systemPrompt},
		{Role: domain.RoleUser, Content: userPrompt},
	}

	// 流式回调
	tokenCallback := func(token string) {
		a.publishStreamEvent(ctx, streamID, "token", token)
	}

	result, err := client.StreamChat(ctx, msgs, tokenCallback)
	if err != nil {
		return "", err
	}

	return result, nil
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
func (a *DynamicActivities) DynamicLLMGenerateStreamWithNodeConfig(
	ctx context.Context,
	nodeConfig *NodeLLMConfigInput, // 节点级别配置（可为 nil）
	blueprintID string, // Blueprint ID（用于获取默认配置）
	systemPrompt string,
	userPrompt string,
	streamID string,
	userId string,
) (string, error) {
	client := a.getClientWithFallback(ctx, nodeConfig, blueprintID, userId)

	msgs := []domain.Message{
		{Role: domain.RoleSystem, Content: systemPrompt},
		{Role: domain.RoleUser, Content: userPrompt},
	}

	tokenCallback := func(token string) {
		a.publishStreamEvent(ctx, streamID, "token", token)
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

	// 3. 回退到用户默认配置
	return a.agentUC.GetClientForAgent(ctx, userId, domain.AgentDynamic)
}

// DynamicRouterDecide 路由决策
func (a *DynamicActivities) DynamicRouterDecide(
	ctx context.Context,
	prompt string,
	choices []string,
	input string,
) (string, error) {
	client := a.agentUC.GetClientForAgent(ctx, "", domain.AgentSupervisor)
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

// DynamicRouterDecideWithBlueprint 使用 Blueprint 配置的路由决策
func (a *DynamicActivities) DynamicRouterDecideWithBlueprint(
	ctx context.Context,
	blueprintID string,
	prompt string,
	choices []string,
	input string,
	userId string,
) (string, error) {
	client, err := a.getClientFromBlueprint(ctx, blueprintID, userId)
	if err != nil {
		return "", fmt.Errorf("failed to get LLM client: %w", err)
	}

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

// getClientFromBlueprint 从 Blueprint 获取 LLM 客户端
func (a *DynamicActivities) getClientFromBlueprint(ctx context.Context, blueprintID string, userId string) (gateway.LLMClient, error) {
	// 如果没有 Blueprint ID，使用用户默认配置
	if blueprintID == "" {
		return a.agentUC.GetClientForAgent(ctx, userId, domain.AgentDynamic), nil
	}

	// 获取 Blueprint（带解密）
	bp, err := a.blueprintRepo.GetByIDWithDecryption(blueprintID)
	if err != nil {
		// 如果获取失败，回退到用户默认配置
		return a.agentUC.GetClientForAgent(ctx, userId, domain.AgentDynamic), nil
	}

	// 如果 Blueprint 没有配置 API Key，使用用户默认配置
	if bp.LLMConfig.APIKey == "" {
		return a.agentUC.GetClientForAgent(ctx, userId, domain.AgentDynamic), nil
	}

	// 使用 Blueprint 的配置创建客户端
	llmCfg := &gateway.LLMConfig{
		APIKey:    bp.LLMConfig.APIKey,
		BaseURL:   bp.LLMConfig.BaseURL,
		ModelName: bp.LLMConfig.ModelName,
	}
	return a.llmFactory.CreateClient(llmCfg), nil
}

// publishStreamEvent 发布流式事件到 Redis
func (a *DynamicActivities) publishStreamEvent(ctx context.Context, streamID string, msgType string, content string) {
	if a.rdb == nil || streamID == "" {
		return
	}
	msg := usecase.StreamMessage{
		Type:    usecase.StreamEventType(msgType),
		Content: content,
	}
	bytes, _ := json.Marshal(msg)
	a.rdb.Publish(ctx, "stream:"+streamID, bytes)
}
