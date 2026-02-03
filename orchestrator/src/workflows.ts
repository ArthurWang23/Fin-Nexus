import { proxyActivities, log } from '@temporalio/workflow';
import {
    WorkflowBlueprint,
    GraphNode,
    LLMNodeConfig,
    ToolNodeConfig,
    RouterNodeConfig,
    ExecutionState,
    NodeLLMConfig
} from './types';

// ============================================
// Go Activities 接口定义
// ============================================

// 节点级别 LLM 配置输入（传给 Go）
interface NodeLLMConfigInput {
    provider?: string;
    api_key?: string;
    base_url?: string;
    model_name?: string;
}

interface GoActivities {
    // 基础 LLM 生成（使用用户默认配置）
    DynamicLLMGenerate(
        systemPrompt: string,
        userPrompt: string,
        modelName: string,
        userId: string
    ): Promise<string>;

    // 使用 Blueprint 配置的 LLM 生成
    DynamicLLMGenerateWithBlueprint(
        blueprintId: string,
        systemPrompt: string,
        userPrompt: string,
        userId: string
    ): Promise<string>;

    // 流式 LLM 生成
    DynamicLLMGenerateStream(
        blueprintId: string,
        systemPrompt: string,
        userPrompt: string,
        streamId: string,
        userId: string
    ): Promise<string>;

    // 使用节点级别配置的 LLM 生成（新增）
    DynamicLLMGenerateWithNodeConfig(
        nodeConfig: NodeLLMConfigInput | null,
        blueprintId: string,
        systemPrompt: string,
        userPrompt: string,
        userId: string
    ): Promise<string>;

    // 使用节点级别配置的流式 LLM 生成（新增）
    DynamicLLMGenerateStreamWithNodeConfig(
        nodeConfig: NodeLLMConfigInput | null,
        blueprintId: string,
        systemPrompt: string,
        userPrompt: string,
        streamId: string,
        userId: string
    ): Promise<string>;

    // 路由决策
    DynamicRouterDecide(
        prompt: string,
        choices: string[],
        input: string
    ): Promise<string>;

    // 使用 Blueprint 配置的路由决策
    DynamicRouterDecideWithBlueprint(
        blueprintId: string,
        prompt: string,
        choices: string[],
        input: string,
        userId: string
    ): Promise<string>;

    // 使用节点级别配置的路由决策（新增）
    DynamicRouterDecideWithNodeConfig(
        nodeConfig: NodeLLMConfigInput | null,
        blueprintId: string,
        prompt: string,
        choices: string[],
        input: string,
        userId: string
    ): Promise<string>;

    // 工具调用
    ResearcherSearch(instruction: string, userId: string): Promise<string>;
    CoderRun(instruction: string, userId: string): Promise<string>;

    // 流式事件发布
    PublishStreamEvent(sessionId: string, msgType: string, content: string): Promise<void>;
}

// 代理 Go Activities
const activities = proxyActivities<GoActivities>({
    startToCloseTimeout: '10m',
    taskQueue: 'agent-task-queue',
});

// ============================================
// 图执行工作流
// ============================================

export interface GraphExecutionInput {
    blueprint: WorkflowBlueprint;
    initialInput: string;
    streamId: string;
    userId: string;
    enableStreaming?: boolean;  // 是否启用流式输出
}

/**
 * GraphExecutionWorkflow - 图执行工作流
 * 
 * 根据用户定义的 Blueprint 执行节点序列，支持：
 * - LLM 节点：调用 LLM 生成内容
 * - Tool 节点：调用工具（researcher, coder, fetch_data）
 * - Router 节点：条件路由
 * - 模板变量替换：{{variable}}
 * - 流式输出支持
 */
export async function GraphExecutionWorkflow(
    blueprint: WorkflowBlueprint,
    initialInput: string,
    streamId: string,
    userId: string
): Promise<string> {
    log.info(`[GraphEngine] Started. User: ${userId}, Blueprint: ${blueprint.id}`);

    // 初始化执行状态
    const state: ExecutionState = {
        "global_input": initialInput,
        "last_output": initialInput,
    };

    let currentNodeId: string | undefined = blueprint.start_node_id;
    let stepsCount = 0;
    const maxSteps = 50;  // 防止无限循环

    while (currentNodeId && currentNodeId !== 'END' && stepsCount < maxSteps) {
        const node = blueprint.nodes.find(n => n.id === currentNodeId);
        if (!node) {
            throw new Error(`Node ${currentNodeId} not found in blueprint`);
        }

        // 发布步骤事件
        await activities.PublishStreamEvent(
            streamId,
            'step',
            `执行节点: ${node.id} (${node.type})`
        );

        let output = "";
        let nextCondition = "default";

        try {
            switch (node.type) {
                case 'Start':
                    output = state["last_output"];
                    break;

                case 'LLM': {
                    const cfg = node.config as LLMNodeConfig;
                    // 将 last_output 映射为 input 供模板使用
                    const templateState = { ...state, input: state["last_output"] };
                    const prompt = fillTemplate(cfg.template, templateState);

                    // 转换节点配置为 Go 格式
                    const nodeConfig = convertNodeConfig(cfg.llm_config);

                    // 检查是否启用流式输出
                    if (cfg.streaming) {
                        output = await activities.DynamicLLMGenerateStreamWithNodeConfig(
                            nodeConfig,
                            blueprint.id,
                            cfg.system_prompt || '',
                            prompt,
                            streamId,
                            userId
                        );
                    } else {
                        // 使用节点配置 + Blueprint 回退 + 用户默认配置
                        output = await activities.DynamicLLMGenerateWithNodeConfig(
                            nodeConfig,
                            blueprint.id,
                            cfg.system_prompt || '',
                            prompt,
                            userId
                        );
                        // 非流式模式下，也发布输出内容以便前端展示
                        await activities.PublishStreamEvent(streamId, 'token', output);
                    }
                    break;
                }

                case 'Tool': {
                    const cfg = node.config as ToolNodeConfig;
                    // 将 last_output 映射为 input 供模板使用
                    const templateState = { ...state, input: state["last_output"] };
                    const input = fillTemplate(cfg.input_template, templateState);

                    await activities.PublishStreamEvent(
                        streamId,
                        'step',
                        `正在执行工具: ${cfg.tool_name}`
                    );

                    switch (cfg.tool_name) {
                        case 'researcher':
                            output = await activities.ResearcherSearch(input, userId);
                            break;
                        case 'coder':
                            output = await activities.CoderRun(input, userId);
                            break;
                        default:
                            output = `Error: Unknown tool '${cfg.tool_name}'`;
                    }
                    // 发布工具输出
                    await activities.PublishStreamEvent(streamId, 'token', `\n[Tool Output]\n${output}\n`);
                    break;
                }

                case 'Router': {
                    const cfg = node.config as RouterNodeConfig;
                    const input = state["last_output"];

                    // Router 也可以有自己的 LLM 配置（用于决策的模型）
                    const nodeConfig = (cfg as any).llm_config
                        ? convertNodeConfig((cfg as any).llm_config)
                        : null;

                    const choice = await activities.DynamicRouterDecideWithNodeConfig(
                        nodeConfig,
                        blueprint.id,
                        cfg.prompt,
                        cfg.choices,
                        input,
                        userId
                    );

                    output = choice;
                    nextCondition = choice;

                    await activities.PublishStreamEvent(
                        streamId,
                        'step',
                        `路由决策: ${choice}`
                    );
                    break;
                }

                case 'End':
                    await activities.PublishStreamEvent(streamId, 'done', 'completed');
                    return state["last_output"];
            }
        } catch (err: any) {
            const errorMsg = `Error in node ${node.id}: ${err.message}`;
            log.error(errorMsg);
            await activities.PublishStreamEvent(streamId, 'error', errorMsg);
            // 变更：遇到错误直接抛出，终止工作流，而不是继续执行
            throw new Error(errorMsg);
        }

        // 更新状态
        state[`${node.id}.output`] = output;
        state["last_output"] = output;

        // 导航到下一个节点
        currentNodeId = findNextNode(blueprint, node, nextCondition);
        stepsCount++;
    }

    // 发送完成事件
    await activities.PublishStreamEvent(streamId, 'done', state["last_output"]);

    if (stepsCount >= maxSteps) {
        log.warn(`[GraphEngine] Max steps reached (${maxSteps}), workflow terminated`);
    }

    return state["last_output"];
}

// ============================================
// 辅助函数
// ============================================

/**
 * convertNodeConfig - 转换节点 LLM 配置为 Go 格式
 */
function convertNodeConfig(config: NodeLLMConfig | undefined): NodeLLMConfigInput | null {
    if (!config) return null;

    return {
        provider: config.provider,
        api_key: config.api_key,
        base_url: config.base_url,
        model_name: config.model_name,
    };
}

/**
 * fillTemplate - 填充模板变量
 * 使用正则匹配 {{variable}}，支持：
 * 1. {{input}} -> 自动映射为当前上下文输入 (last_output)
 * 2. {{global_input}} -> 初始输入
 * 3. {{nodeId.output}} -> 指定节点输出
 * 4. 如果变量不存在，保留原样以便调试
 */
function fillTemplate(template: string, state: ExecutionState): string {
    if (!template) return "";

    // 正则匹配 {{ key }}，允许键名周围有空格
    return template.replace(/\{\{\s*(.*?)\s*\}\}/g, (match, key) => {
        const trimmedKey = key.trim();

        // 1. 优先查找 State 中的直接匹配 (包括 input, global_input, nodeId.output)
        if (Object.prototype.hasOwnProperty.call(state, trimmedKey)) {
            return String(state[trimmedKey]);
        }

        // 2. 如果未找到，返回原字符串 (或者可以配置为返回空字符串)
        return match;
    });
}

/**
 * findNextNode - 查找下一个节点
 * 优先级：
 * 1. 匹配特定条件的边
 * 2. 默认边（condition 为空或 'default'）
 * 3. 节点的 next 字段
 */
function findNextNode(
    blueprint: WorkflowBlueprint,
    currentNode: GraphNode,
    condition: string
): string | undefined {
    // 查找匹配条件的边
    const matchedEdge = blueprint.edges.find(
        e => e.source === currentNode.id && e.condition === condition
    );
    if (matchedEdge) {
        return matchedEdge.target;
    }

    // 查找默认边
    const defaultEdge = blueprint.edges.find(
        e => e.source === currentNode.id && (!e.condition || e.condition === 'default')
    );
    if (defaultEdge) {
        return defaultEdge.target;
    }

    // 回退到节点的 next 字段
    return currentNode.next;
}

// ============================================
// 验证工作流（可选，在启动前验证）
// ============================================

export function validateBlueprint(blueprint: WorkflowBlueprint): { valid: boolean; errors: string[] } {
    const errors: string[] = [];

    // 检查基本字段
    if (!blueprint.id) {
        errors.push("Blueprint ID is required");
    }
    if (!blueprint.start_node_id) {
        errors.push("Start node ID is required");
    }
    if (!blueprint.nodes || blueprint.nodes.length === 0) {
        errors.push("At least one node is required");
    }

    // 检查节点数量限制
    if (blueprint.nodes && blueprint.nodes.length > 50) {
        errors.push("Too many nodes (max 50)");
    }

    // 构建节点 ID 集合
    const nodeIds = new Set(blueprint.nodes?.map(n => n.id) || []);

    // 检查 start_node_id 存在
    if (blueprint.start_node_id && !nodeIds.has(blueprint.start_node_id)) {
        errors.push(`Start node '${blueprint.start_node_id}' not found in nodes`);
    }

    // 检查边的有效性
    for (const edge of blueprint.edges || []) {
        if (!nodeIds.has(edge.source)) {
            errors.push(`Edge source '${edge.source}' not found in nodes`);
        }
        if (edge.target !== 'END' && !nodeIds.has(edge.target)) {
            errors.push(`Edge target '${edge.target}' not found in nodes`);
        }
    }

    return {
        valid: errors.length === 0,
        errors
    };
}
