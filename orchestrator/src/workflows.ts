import { proxyActivities, defineSignal, setHandler, condition, log } from '@temporalio/workflow';
import {
    WorkflowBlueprint,
    GraphNode,
    LLMNodeConfig,
    ToolNodeConfig,
    RouterNodeConfig,
    ExecutionState,
    NodeLLMConfig,
    AgentInput,
    AgentResult,
    ApprovalSignal,
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
        msgType: string,  // 消息类型: "token" (正常) 或 "agent_output" (内部节点)
        userId: string
    ): Promise<string>;

    // 路由决策

    // 使用节点级别配置的路由决策（新增）
    DynamicRouterDecideWithNodeConfig(
        nodeConfig: NodeLLMConfigInput | null,
        blueprintId: string,
        prompt: string,
        choices: string[],
        input: string,
        userId: string
    ): Promise<string>;

    // 旧版工具调用（保留向后兼容）
    ResearcherSearch(instruction: string, userId: string): Promise<string>;
    CoderRun(instruction: string, userId: string): Promise<string>;

    // 新版 Agent Registry 调用
    AgentExecute(agentName: string, input: AgentInput, streamId: string): Promise<AgentResult>;
    AgentPreview(agentName: string, input: AgentInput): Promise<string>;
    AgentExecuteApproved(agentName: string, approvedContent: string): Promise<AgentResult>;

    // 流式事件发布
    PublishStreamEvent(sessionId: string, msgType: string, content: string): Promise<void>;

    // 会话保存
    SaveBlueprintChatTurn(
        sessionId: string,
        userId: string,
        userQuery: string,
        finalAnswer: string
    ): Promise<void>;
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

// Approval signal — same name as Go's SignalApprove constant
const approveSignal = defineSignal<[ApprovalSignal]>('APPROVE_SIGNAL');

// Agent name mapping: Blueprint tool_name → Registry agent name
const TOOL_AGENT_MAP: Record<string, string> = {
    'researcher': 'Researcher',
    'coder': 'Coder',
};

// Agents that require human approval before execution
const APPROVABLE_AGENTS = new Set(['Coder']);

/**
 * GraphExecutionWorkflow - 图执行工作流
 * 
 * 根据用户定义的 Blueprint 执行节点序列，支持：
 * - LLM 节点：调用 LLM 生成内容
 * - Tool 节点：通过 AgentRegistry 调用（含 Skill Registry、人工审批、结构化输出）
 * - Router 节点：条件路由
 * - 模板变量替换：{{variable}}
 * - 流式输出支持
 */
export async function GraphExecutionWorkflow(
    blueprint: WorkflowBlueprint,
    initialInput: string,
    streamId: string,
    sessionId: string,
    userId: string
): Promise<string> {
    log.info(`[GraphEngine] Started. User: ${userId}, Blueprint: ${blueprint.id}, SessionId: ${sessionId}`);

    // Signal state for human-in-the-loop approval
    let pendingApproval: ApprovalSignal | null = null;
    setHandler(approveSignal, (signal: ApprovalSignal) => {
        pendingApproval = signal;
    });

    // 初始化执行状态
    const state: ExecutionState = {
        "global_input": initialInput,
        "last_output": initialInput,
    };

    let currentNodeId: string | undefined = blueprint.start_node_id;
    let stepsCount = 0;
    const maxSteps = 50;

    while (currentNodeId && currentNodeId !== 'END' && stepsCount < maxSteps) {
        const node = blueprint.nodes.find(n => n.id === currentNodeId);
        if (!node) {
            throw new Error(`Node ${currentNodeId} not found in blueprint`);
        }

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
                    const templateState = { ...state, input: state["last_output"] };
                    const prompt = fillTemplate(cfg.template, templateState);
                    const nodeConfig = convertNodeConfig(cfg.llm_config);

                    const isConnectedToEnd = blueprint.edges.some(
                        e => e.source === node.id &&
                            blueprint.nodes.find(n => n.id === e.target)?.type === 'End'
                    );
                    const msgType = isConnectedToEnd ? 'token' : 'agent_output';

                    if (cfg.streaming) {
                        output = await activities.DynamicLLMGenerateStreamWithNodeConfig(
                            nodeConfig,
                            blueprint.id,
                            cfg.system_prompt || '',
                            prompt,
                            streamId,
                            msgType,
                            userId
                        );
                    } else {
                        output = await activities.DynamicLLMGenerateWithNodeConfig(
                            nodeConfig,
                            blueprint.id,
                            cfg.system_prompt || '',
                            prompt,
                            userId
                        );
                        await activities.PublishStreamEvent(streamId, msgType, output);
                    }
                    log.info(`[GraphEngine] LLM node ${node.id} output length: ${output.length}, msgType: ${msgType}`);
                    break;
                }

                case 'Tool': {
                    const cfg = node.config as ToolNodeConfig;
                    const templateState = { ...state, input: state["last_output"] };
                    const instruction = fillTemplate(cfg.input_template, templateState);

                    const agentName = TOOL_AGENT_MAP[cfg.tool_name];
                    if (!agentName) {
                        output = `Error: Unknown tool '${cfg.tool_name}'`;
                        break;
                    }

                    await activities.PublishStreamEvent(
                        streamId, 'step',
                        `正在通过 ${agentName} Agent 执行: ${cfg.tool_name}`
                    );

                    const agentInput: AgentInput = {
                        instruction,
                        user_id: userId,
                    };

                    if (APPROVABLE_AGENTS.has(agentName)) {
                        // Approvable agent (e.g. Coder): Preview → Approval → Execute
                        const previewCode = await activities.AgentPreview(agentName, agentInput);

                        if (!previewCode) {
                            output = "No code needed for this task.";
                        } else {
                            // Send code for human review
                            await activities.PublishStreamEvent(
                                streamId, 'approval_required',
                                JSON.stringify({ agent: agentName, code: previewCode })
                            );

                            // Wait for user decision
                            pendingApproval = null;
                            await condition(() => pendingApproval !== null);

                            if (pendingApproval!.approved) {
                                const codeToRun = pendingApproval!.modified_code || previewCode;
                                await activities.PublishStreamEvent(streamId, 'step', '✅ 已批准，执行中...');
                                const result: AgentResult = await activities.AgentExecuteApproved(agentName, codeToRun);
                                output = result.summary;
                                if (result.artifacts?.length) {
                                    await activities.PublishStreamEvent(
                                        streamId, 'artifacts',
                                        JSON.stringify(result.artifacts)
                                    );
                                }
                            } else {
                                const reason = pendingApproval!.reason || '';
                                await activities.PublishStreamEvent(streamId, 'step', '❌ 用户拒绝了执行');
                                output = `User rejected execution. Reason: ${reason}`;
                            }
                        }
                    } else {
                        // Non-approvable agent (e.g. Researcher): direct execution
                        const result: AgentResult = await activities.AgentExecute(agentName, agentInput, streamId);
                        output = result.summary;
                        if (result.artifacts?.length) {
                            await activities.PublishStreamEvent(
                                streamId, 'artifacts',
                                JSON.stringify(result.artifacts)
                            );
                        }
                    }

                    await activities.PublishStreamEvent(streamId, 'agent_output', `[Tool: ${cfg.tool_name}]\n${output}`);
                    break;
                }

                case 'Router': {
                    const cfg = node.config as RouterNodeConfig;
                    const input = state["last_output"];

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
                        streamId, 'step',
                        `路由决策: ${choice}`
                    );
                    break;
                }

                case 'End':
                    if (sessionId) {
                        await activities.SaveBlueprintChatTurn(
                            sessionId, userId, initialInput, state["last_output"]
                        );
                    }
                    await activities.PublishStreamEvent(streamId, 'done', 'completed');
                    return state["last_output"];
            }
        } catch (err: any) {
            const errorMsg = `Error in node ${node.id}: ${err.message}`;
            log.error(errorMsg);
            await activities.PublishStreamEvent(streamId, 'error', errorMsg);
            throw new Error(errorMsg);
        }

        state[`${node.id}.output`] = output;
        state["last_output"] = output;

        const prevNodeId = currentNodeId;
        currentNodeId = findNextNode(blueprint, node, nextCondition);
        log.info(`[GraphEngine] Navigation: ${prevNodeId} -> ${currentNodeId} (condition: ${nextCondition})`);
        stepsCount++;
    }

    if (sessionId) {
        await activities.SaveBlueprintChatTurn(
            sessionId, userId, initialInput, state["last_output"]
        );
    }

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

        // 2. 尝试解析 JSON 属性 (例如: supervisor.output.instruction)
        // 假设 keys 形如 "nodeId.output.propertyName"
        // 我们尝试找到最长匹配的 state key (如 "nodeId.output")
        // 然后解析其值为 JSON 并获取剩余 path (propertyName)
        const parts = trimmedKey.split('.');
        // 尝试从长到短匹配 state key
        for (let i = parts.length - 1; i >= 1; i--) {
            const stateKey = parts.slice(0, i).join('.');
            const propPath = parts.slice(i); // 剩余部分

            if (Object.prototype.hasOwnProperty.call(state, stateKey)) {
                const jsonStr = state[stateKey];
                try {
                    let obj = JSON.parse(jsonStr);
                    // 逐层访问属性
                    for (const prop of propPath) {
                        obj = obj[prop];
                        if (obj === undefined) break;
                    }
                    if (obj !== undefined && typeof obj !== 'object') {
                        return String(obj);
                    }
                } catch (e) {
                    // 解析失败或不是 JSON，继续
                }
            }
        }

        // 3. 如果未找到，返回原字符串 (或者可以配置为返回空字符串)
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
    const conditionLower = condition.toLowerCase();
    log.info(`[findNextNode] Node: ${currentNode.id}, Condition: "${condition}", Total edges: ${blueprint.edges.length}`);

    // 查找匹配条件的边（大小写不敏感）
    const matchedEdge = blueprint.edges.find(
        e => e.source === currentNode.id && e.condition?.toLowerCase() === conditionLower
    );
    if (matchedEdge) {
        log.info(`[findNextNode] Found matched edge: ${matchedEdge.source} -> ${matchedEdge.target} (condition: ${matchedEdge.condition})`);
        return matchedEdge.target;
    }

    // 查找默认边（包括 'default'、'next' 或无条件）
    const isDefaultCondition = (c: string | undefined) =>
        !c || ['default', 'next'].includes(c.toLowerCase());

    const defaultEdge = blueprint.edges.find(
        e => e.source === currentNode.id && isDefaultCondition(e.condition)
    );
    if (defaultEdge) {
        log.info(`[findNextNode] Found default edge: ${defaultEdge.source} -> ${defaultEdge.target}`);
        return defaultEdge.target;
    }

    // 回退到节点的 next 字段
    log.info(`[findNextNode] No edge found, using node.next: ${currentNode.next}`);
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
