// ============================================
// 节点类型定义
// ============================================

export type NodeType = 'Start' | 'LLM' | 'Tool' | 'Router' | 'End';

// 节点级别的 LLM 配置（每个节点可以有独立的模型）
export interface NodeLLMConfig {
    provider?: string;      // 'openai' | 'deepseek' | 'qwen' | 'anthropic'
    api_key?: string;        // 节点专用 API Key（加密存储）
    base_url?: string;       // 节点专用 Base URL
    model_name?: string;     // 节点专用模型名称
}

// LLM 节点配置
export interface LLMNodeConfig {
    system_prompt: string;
    template: string;           // 支持 {{variable}} 模板变量
    streaming?: boolean;        // 是否启用流式输出
    llm_config?: NodeLLMConfig;  // 节点级别的模型配置（可选，为空则使用 Blueprint 默认配置）
}

// 工具节点配置
export interface ToolNodeConfig {
    tool_name: string;       // 工具名称: 'researcher' | 'coder'
    input_template: string;  // 输入模板，支持 {{variable}}
}

// 路由节点配置（条件判断）
export interface RouterNodeConfig {
    prompt: string;         // 判断意图的 Prompt
    choices: string[];      // 可选的路由选项
}

// ============================================
// 图节点和边定义
// ============================================

export interface GraphNode {
    id: string;
    type: NodeType;
    config: LLMNodeConfig | ToolNodeConfig | RouterNodeConfig | null;
    next?: string;          // 下一个节点 ID（简单顺序流）
    position?: { x: number; y: number }; // 前端布局坐标
}

export interface Edge {
    source: string;
    target: string;
    condition?: string;     // 条件匹配（用于 Router 节点）
}

// ============================================
// Blueprint LLM 配置（用于覆盖默认配置）
// ============================================

export interface BlueprintLLMConfig {
    provider?: string;      // 'openai' | 'deepseek' | 'qwen' | 'anthropic'
    api_key?: string;        // 加密存储，运行时解密
    base_url?: string;
    model_name?: string;     // 默认模型名称
}

// ============================================
// 工作流蓝图定义
// ============================================

export interface WorkflowBlueprint {
    id: string;
    user_id?: string;
    name: string;
    description?: string;
    start_node_id: string;
    nodes: GraphNode[];
    edges: Edge[];
    llm_config?: BlueprintLLMConfig;
    is_public?: boolean;
    version?: number;
}


// ============================================
// 执行状态
// ============================================

export interface ExecutionState {
    [key: string]: string;  // 节点输出和全局变量
}

// ============================================
// 验证结果
// ============================================

export interface ValidationError {
    field: string;
    message: string;
}

export interface ValidationResult {
    valid: boolean;
    errors?: ValidationError[];
}

// ============================================
// 流式事件类型
// ============================================

export type StreamEventType = 'step' | 'token' | 'error' | 'done';

export interface StreamEvent {
    type: StreamEventType;
    content: string;
    nodeId?: string;
}
