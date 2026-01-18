package usecase

const PromptExtractGraph = `你是一个专业的知识图谱构建专家。请分析以下文本，提取其中的核心实体（Entities）和关系（Relationships）。

【文本内容】：
{{.Text}}

【提取规则】：
1. 实体类型(Type)必须属于以下几种之一: "Person" (人), "Organization" (组织), "Paper" (论文/文档), "Concept" (概念/技术), "Product" (产品)。
2. 关系类型(Type)必须全部大写，例如: "AUTHORED" (作者), "BELONGS_TO" (属于), "IMPROVES" (改进了), "USES" (使用了), "MENTIONS" (提及)。
3. 请尽可能挖掘潜藏的逻辑关系，丢弃无意义的代词或停用词。

【输出格式】:
请严格输出 JSON 格式，不要包含 markdown 代码块 (` + "```" + `json) 和其他解释性文字。格式如下：
{
	"entities": [
		{"name": "Ankit Goyal", "type": "Person"},
		{"name": "VLA-0", "type": "Paper"}
	],
	"relations": [
		{
		"source": {"name": "Ankit Goyal", "type": "Person"},
		"target": {"name": "VLA-0", "type": "Paper"},
		"type": "AUTHORED"
		}
	]
}
`

const PromptQueryEntityExtraction = `
你是一个搜索查询优化专家。你的任务是从用户的自然语言问题中，提取出最核心的实体（Entities）关键词，用于在知识图谱中检索。

【要求】：
1. 只提取专有名词（如人名、论文名、技术术语、机构名）。
2. 忽略通用的疑问词（如“是谁”、“什么”、“哪里”、“关系”）。
3. 如果有多个实体，全部提取。
4. 输出为 JSON 字符串数组。

【示例】：
用户输入："VLA-0 和 OpenVLA 的性能对比如何？"
输出：["VLA-0", "OpenVLA"]

用户输入："Ankit Goyal 写了哪些论文？"
输出：["Ankit Goyal"]

用户输入："介绍一下 GraphRAG"
输出：["GraphRAG"]

【当前用户输入】：
{{.Query}}

【输出】：
`
