package usecase

import (
	"encoding/json"
	"fmt"
	"go-nexus/internal/domain"
)

// BlueprintUseCase Blueprint 业务逻辑
type BlueprintUseCase struct {
	repo domain.BlueprintRepository
}

// NewBlueprintUseCase 创建 Blueprint UseCase
func NewBlueprintUseCase(repo domain.BlueprintRepository) *BlueprintUseCase {
	return &BlueprintUseCase{repo: repo}
}

// Create 创建 Blueprint（带验证）
func (uc *BlueprintUseCase) Create(bp *domain.WorkflowBlueprint) error {
	result := uc.Validate(bp)
	if !result.Valid {
		return fmt.Errorf("validation failed: %v", result.Errors)
	}
	return uc.repo.Create(bp)
}

// Update 更新 Blueprint（带验证）
func (uc *BlueprintUseCase) Update(bp *domain.WorkflowBlueprint, userID string) error {
	// 检查所有权
	existing, err := uc.repo.GetByID(bp.ID)
	if err != nil {
		return fmt.Errorf("blueprint not found: %w", err)
	}
	if existing.UserID != userID {
		return fmt.Errorf("access denied: you don't own this blueprint")
	}

	result := uc.Validate(bp)
	if !result.Valid {
		return fmt.Errorf("validation failed: %v", result.Errors)
	}

	bp.UserID = userID // 确保 UserID 不被修改
	return uc.repo.Update(bp)
}

// Delete 删除 Blueprint
func (uc *BlueprintUseCase) Delete(id string, userID string) error {
	return uc.repo.Delete(id, userID)
}

// GetByID 获取 Blueprint（不含敏感信息）
func (uc *BlueprintUseCase) GetByID(id string, userID string) (*domain.WorkflowBlueprint, error) {
	bp, err := uc.repo.GetByID(id)
	if err != nil {
		return nil, err
	}

	// 检查访问权限
	if !bp.IsPublic && bp.UserID != userID {
		return nil, fmt.Errorf("access denied")
	}

	return bp, nil
}

// GetForExecution 获取用于执行的 Blueprint（含解密的配置）
func (uc *BlueprintUseCase) GetForExecution(id string, userID string) (*domain.WorkflowBlueprint, error) {
	bp, err := uc.repo.GetByIDWithDecryption(id)
	if err != nil {
		return nil, err
	}

	// 检查访问权限
	if !bp.IsPublic && bp.UserID != userID {
		return nil, fmt.Errorf("access denied")
	}

	return bp, nil
}

// ListByUser 列出用户的 Blueprint
func (uc *BlueprintUseCase) ListByUser(userID string) ([]domain.WorkflowBlueprint, error) {
	return uc.repo.ListByUser(userID)
}

// ListPublic 列出公开的 Blueprint
func (uc *BlueprintUseCase) ListPublic() ([]domain.WorkflowBlueprint, error) {
	return uc.repo.ListPublic()
}

// Clone 克隆一个 Blueprint 到自己的账户
func (uc *BlueprintUseCase) Clone(id string, userID string) (*domain.WorkflowBlueprint, error) {
	original, err := uc.repo.GetByID(id)
	if err != nil {
		return nil, err
	}

	// 只能克隆公开的 Blueprint 或自己的
	if !original.IsPublic && original.UserID != userID {
		return nil, fmt.Errorf("access denied")
	}

	// 创建副本
	clone := &domain.WorkflowBlueprint{
		UserID:      userID,
		Name:        original.Name + " (Copy)",
		Description: original.Description,
		StartNodeID: original.StartNodeID,
		Nodes:       original.Nodes,
		Edges:       original.Edges,
		IsPublic:    false, // 克隆的默认私有
		// 不复制 LLMConfig，用户需要自己配置
	}

	if err := uc.repo.Create(clone); err != nil {
		return nil, err
	}

	return clone, nil
}

// Validate 验证 Blueprint 结构
func (uc *BlueprintUseCase) Validate(bp *domain.WorkflowBlueprint) *domain.BlueprintValidationResult {
	result := &domain.BlueprintValidationResult{Valid: true}
	var errors []domain.ValidationError

	// 1. 检查基本字段
	if bp.Name == "" {
		errors = append(errors, domain.ValidationError{
			Field:   "name",
			Message: "name is required",
		})
	}

	if bp.StartNodeID == "" {
		errors = append(errors, domain.ValidationError{
			Field:   "start_node_id",
			Message: "start_node_id is required",
		})
	}

	if len(bp.Nodes) == 0 {
		errors = append(errors, domain.ValidationError{
			Field:   "nodes",
			Message: "at least one node is required",
		})
	}

	// 2. 检查节点数量限制
	if len(bp.Nodes) > 50 {
		errors = append(errors, domain.ValidationError{
			Field:   "nodes",
			Message: "too many nodes (max 50)",
		})
	}

	// 3. 构建节点 ID 集合
	nodeIDs := make(map[string]bool)
	for _, node := range bp.Nodes {
		if node.ID == "" {
			errors = append(errors, domain.ValidationError{
				Field:   "nodes",
				Message: "node id is required",
			})
			continue
		}
		if nodeIDs[node.ID] {
			errors = append(errors, domain.ValidationError{
				Field:   "nodes",
				Message: fmt.Sprintf("duplicate node id: %s", node.ID),
			})
		}
		nodeIDs[node.ID] = true
	}

	// 4. 检查 start_node_id 存在
	if bp.StartNodeID != "" && !nodeIDs[bp.StartNodeID] {
		errors = append(errors, domain.ValidationError{
			Field:   "start_node_id",
			Message: fmt.Sprintf("start node '%s' not found in nodes", bp.StartNodeID),
		})
	}

	// 5. 检查边的有效性
	for _, edge := range bp.Edges {
		if !nodeIDs[edge.Source] {
			errors = append(errors, domain.ValidationError{
				Field:   "edges",
				Message: fmt.Sprintf("edge source '%s' not found in nodes", edge.Source),
			})
		}
		if edge.Target != "END" && !nodeIDs[edge.Target] {
			errors = append(errors, domain.ValidationError{
				Field:   "edges",
				Message: fmt.Sprintf("edge target '%s' not found in nodes", edge.Target),
			})
		}
	}

	// 6. 验证节点配置
	for _, node := range bp.Nodes {
		nodeErrors := uc.validateNodeConfig(node)
		errors = append(errors, nodeErrors...)
	}

	// 7. 检查是否有 End 节点可达
	hasEndReachable := false
	for _, edge := range bp.Edges {
		if edge.Target == "END" {
			hasEndReachable = true
			break
		}
	}
	for _, node := range bp.Nodes {
		if node.Type == domain.NodeTypeEnd || node.Next == "END" {
			hasEndReachable = true
			break
		}
	}
	if !hasEndReachable && len(bp.Nodes) > 0 {
		errors = append(errors, domain.ValidationError{
			Field:   "edges",
			Message: "no path to END node found",
		})
	}

	// 8. 检测循环（已移除，Execution Engine 支持 maxSteps 限制，允许循环）
	// if uc.hasCycle(bp) {
	// 	errors = append(errors, domain.ValidationError{
	// 		Field:   "edges",
	// 		Message: "cycle detected in workflow graph",
	// 	})
	// }

	if len(errors) > 0 {
		result.Valid = false
		result.Errors = errors
	}

	return result
}

// validateNodeConfig 验证节点配置
func (uc *BlueprintUseCase) validateNodeConfig(node domain.GraphNode) []domain.ValidationError {
	var errors []domain.ValidationError

	switch node.Type {
	case domain.NodeTypeLLM:
		var cfg domain.LLMNodeConfig
		if err := convertConfig(node.Config, &cfg); err != nil {
			errors = append(errors, domain.ValidationError{
				Field:   fmt.Sprintf("nodes[%s].config", node.ID),
				Message: "invalid LLM node config",
			})
		} else {
			if cfg.Template == "" {
				errors = append(errors, domain.ValidationError{
					Field:   fmt.Sprintf("nodes[%s].config.template", node.ID),
					Message: "LLM node template is required",
				})
			}
		}

	case domain.NodeTypeTool:
		var cfg domain.ToolNodeConfig
		if err := convertConfig(node.Config, &cfg); err != nil {
			errors = append(errors, domain.ValidationError{
				Field:   fmt.Sprintf("nodes[%s].config", node.ID),
				Message: "invalid Tool node config",
			})
		} else {
			if cfg.ToolName == "" {
				errors = append(errors, domain.ValidationError{
					Field:   fmt.Sprintf("nodes[%s].config.tool_name", node.ID),
					Message: "Tool node tool_name is required",
				})
			}
			// 验证工具名称
			validTools := map[string]bool{
				"researcher": true,
				"coder":      true,
				"fetch_data": true,
			}
			if !validTools[cfg.ToolName] {
				errors = append(errors, domain.ValidationError{
					Field:   fmt.Sprintf("nodes[%s].config.tool_name", node.ID),
					Message: fmt.Sprintf("unknown tool: %s", cfg.ToolName),
				})
			}
		}

	case domain.NodeTypeRouter:
		var cfg domain.RouterNodeConfig
		if err := convertConfig(node.Config, &cfg); err != nil {
			errors = append(errors, domain.ValidationError{
				Field:   fmt.Sprintf("nodes[%s].config", node.ID),
				Message: "invalid Router node config",
			})
		} else {
			if cfg.Prompt == "" {
				errors = append(errors, domain.ValidationError{
					Field:   fmt.Sprintf("nodes[%s].config.prompt", node.ID),
					Message: "Router node prompt is required",
				})
			}
			if len(cfg.Choices) < 2 {
				errors = append(errors, domain.ValidationError{
					Field:   fmt.Sprintf("nodes[%s].config.choices", node.ID),
					Message: "Router node needs at least 2 choices",
				})
			}
		}

	case domain.NodeTypeStart, domain.NodeTypeEnd:
		// 这些节点不需要特殊配置

	default:
		errors = append(errors, domain.ValidationError{
			Field:   fmt.Sprintf("nodes[%s].type", node.ID),
			Message: fmt.Sprintf("unknown node type: %s", node.Type),
		})
	}

	return errors
}

// hasCycle 检测图中是否有循环
func (uc *BlueprintUseCase) hasCycle(bp *domain.WorkflowBlueprint) bool {
	// 构建邻接表
	graph := make(map[string][]string)
	for _, node := range bp.Nodes {
		graph[node.ID] = []string{}
		if node.Next != "" && node.Next != "END" {
			graph[node.ID] = append(graph[node.ID], node.Next)
		}
	}
	for _, edge := range bp.Edges {
		if edge.Target != "END" {
			graph[edge.Source] = append(graph[edge.Source], edge.Target)
		}
	}

	// DFS 检测循环
	visited := make(map[string]int) // 0: 未访问, 1: 访问中, 2: 已完成

	var dfs func(node string) bool
	dfs = func(node string) bool {
		if visited[node] == 1 {
			return true // 发现循环
		}
		if visited[node] == 2 {
			return false
		}

		visited[node] = 1
		for _, next := range graph[node] {
			if dfs(next) {
				return true
			}
		}
		visited[node] = 2
		return false
	}

	for nodeID := range graph {
		if visited[nodeID] == 0 {
			if dfs(nodeID) {
				return true
			}
		}
	}

	return false
}

// convertConfig 将 interface{} 转换为具体配置类型
func convertConfig(cfg interface{}, target interface{}) error {
	bytes, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	return json.Unmarshal(bytes, target)
}
