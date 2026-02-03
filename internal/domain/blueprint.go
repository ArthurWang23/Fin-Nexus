package domain

import (
	"database/sql/driver"
	"encoding/json"
	"errors"
	"time"
)

// NodeType 定义节点类型
type NodeType string

const (
	NodeTypeStart  NodeType = "Start"
	NodeTypeLLM    NodeType = "LLM"
	NodeTypeTool   NodeType = "Tool"
	NodeTypeRouter NodeType = "Router"
	NodeTypeEnd    NodeType = "End"
)

// NodeLLMConfig 节点级别的 LLM 配置（每个节点可以有独立的模型）
type NodeLLMConfig struct {
	Provider  string `json:"provider,omitempty"`   // "openai", "deepseek", "qwen", etc.
	APIKey    string `json:"api_key,omitempty"`    // 节点专用 API Key（加密存储）
	BaseURL   string `json:"base_url,omitempty"`   // 节点专用 Base URL
	ModelName string `json:"model_name,omitempty"` // 节点专用模型名称
}

// LLMNodeConfig LLM 节点配置
type LLMNodeConfig struct {
	SystemPrompt string         `json:"system_prompt"`
	Template     string         `json:"template"`
	Streaming    bool           `json:"streaming,omitempty"`  // 是否启用流式输出
	LLMConfig    *NodeLLMConfig `json:"llm_config,omitempty"` // 节点级别的模型配置（可选，为空则使用 Blueprint 默认配置）
}

// ToolNodeConfig 工具节点配置
type ToolNodeConfig struct {
	ToolName      string `json:"tool_name"`
	InputTemplate string `json:"input_template"`
}

// RouterNodeConfig 路由节点配置
type RouterNodeConfig struct {
	Prompt  string   `json:"prompt"`
	Choices []string `json:"choices"`
}

// GraphNode 图节点定义
type GraphNode struct {
	ID       string      `json:"id"`
	Type     NodeType    `json:"type"`
	Config   interface{} `json:"config"` // LLMNodeConfig | ToolNodeConfig | RouterNodeConfig
	Next     string      `json:"next,omitempty"`
	Position *struct {
		X float64 `json:"x"`
		Y float64 `json:"y"`
	} `json:"position,omitempty"`
}

// Edge 边定义（用于条件路由）
type Edge struct {
	Source    string `json:"source"`
	Target    string `json:"target"`
	Condition string `json:"condition,omitempty"`
}

// BlueprintLLMConfig Blueprint 级别的 LLM 配置（用于覆盖默认配置）
type BlueprintLLMConfig struct {
	Provider  string `json:"provider"`             // "openai", "deepseek", "qwen", etc.
	APIKey    string `json:"api_key,omitempty"`    // 可选，如果用户想用自己的 key
	BaseURL   string `json:"base_url,omitempty"`   // 可选
	ModelName string `json:"model_name,omitempty"` // 默认模型
}

// WorkflowBlueprint 工作流蓝图定义
type WorkflowBlueprint struct {
	ID          string             `json:"id" gorm:"primaryKey"`
	UserID      string             `json:"user_id" gorm:"index"`
	Name        string             `json:"name"`
	Description string             `json:"description"`
	StartNodeID string             `json:"start_node_id"`
	Nodes       GraphNodeList      `json:"nodes" gorm:"type:jsonb"`
	Edges       EdgeList           `json:"edges" gorm:"type:jsonb"`
	LLMConfig   BlueprintLLMConfig `json:"llm_config" gorm:"type:jsonb"` // 存储加密后的配置
	IsPublic    bool               `json:"is_public" gorm:"default:false"`
	Version     int                `json:"version" gorm:"default:1"`
	CreatedAt   time.Time          `json:"created_at"`
	UpdatedAt   time.Time          `json:"updated_at"`
}

// GraphNodeList 自定义类型，支持 GORM JSON 序列化
type GraphNodeList []GraphNode

func (g GraphNodeList) Value() (driver.Value, error) {
	return json.Marshal(g)
}

func (g *GraphNodeList) Scan(value interface{}) error {
	bytes, ok := value.([]byte)
	if !ok {
		return errors.New("type assertion to []byte failed")
	}
	return json.Unmarshal(bytes, g)
}

// EdgeList 自定义类型，支持 GORM JSON 序列化
type EdgeList []Edge

func (e EdgeList) Value() (driver.Value, error) {
	return json.Marshal(e)
}

func (e *EdgeList) Scan(value interface{}) error {
	bytes, ok := value.([]byte)
	if !ok {
		return errors.New("type assertion to []byte failed")
	}
	return json.Unmarshal(bytes, e)
}

// BlueprintLLMConfigJSON 自定义类型，支持 GORM JSON 序列化
func (c BlueprintLLMConfig) Value() (driver.Value, error) {
	return json.Marshal(c)
}

func (c *BlueprintLLMConfig) Scan(value interface{}) error {
	bytes, ok := value.([]byte)
	if !ok {
		return errors.New("type assertion to []byte failed")
	}
	return json.Unmarshal(bytes, c)
}

// BlueprintRepository 蓝图仓库接口
type BlueprintRepository interface {
	Create(bp *WorkflowBlueprint) error
	Update(bp *WorkflowBlueprint) error
	Delete(id string, userID string) error
	GetByID(id string) (*WorkflowBlueprint, error)
	GetByIDWithDecryption(id string) (*WorkflowBlueprint, error) // 解密 API Key
	ListByUser(userID string) ([]WorkflowBlueprint, error)
	ListPublic() ([]WorkflowBlueprint, error)
}

// ValidationError 验证错误
type ValidationError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

// BlueprintValidationResult 验证结果
type BlueprintValidationResult struct {
	Valid  bool              `json:"valid"`
	Errors []ValidationError `json:"errors,omitempty"`
}
