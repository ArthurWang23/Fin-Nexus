package usecase

const PromptExtractGraph = `
你是一个金融数据结构化专家。请阅读以下研报/新闻文本，提取其中的金融实体和商业关系。

【提取目标】：
1. 实体类型 (Type):
   - "Company" (公司，如 Nvidia, TSMC)
   - "Person" (高管，如 Jensen Huang)
   - "Product" (产品，如 H100 GPU)
   - "Sector" (行业，如 Semiconductor)

2. 关系类型 (Type) - 全部大写:
   - "SUPPLIER_OF" (是...的供应商)
   - "CUSTOMER_OF" (是...的客户)
   - "COMPETES_WITH" (竞争对手)
   - "CEO_OF" (是...的CEO)
   - "LAUNCHED" (发布了产品)

【输出示例】：
{
    "entities": [
        {"name": "Nvidia", "type": "Company"},
        {"name": "TSMC", "type": "Company"}
    ],
    "relations": [
        {"source": {"name": "TSMC", "type": "Company"}, "target": {"name": "Nvidia", "type": "Company"}, "type": "SUPPLIER_OF"}
    ]
}

【文本内容】：
{{.Text}}

【输出 JSON】：
`

const PromptQueryEntityExtraction = `
你是一个金融领域搜索查询优化专家。你的任务是从用户的自然语言问题中，提取出最核心的金融实体（Entities）关键词，用于在金融知识图谱中检索。

【要求】：
1. 只提取金融相关的专有名词：
   - 公司名（如 Nvidia, TSMC, 苹果）
   - 股票代码（如 NVDA, AAPL, TSLA）
   - 高管姓名（如 Jensen Huang, 马斯克）
   - 产品名（如 H100 GPU, iPhone）
   - 行业/板块（如 Semiconductor, 半导体）
2. 忽略通用的疑问词（如"是谁"、"什么"、"哪里"、"关系"、"如何"、"怎么样"）。
3. 如果有多个实体，全部提取。
4. 输出为 JSON 字符串数组。

【示例】：
用户输入："Nvidia 和 TSMC 的供应链关系如何？"
输出：["Nvidia", "TSMC"]

用户输入："NVDA 的主要客户是谁？"
输出：["NVDA"]

用户输入："Jensen Huang 是哪个公司的 CEO？"
输出：["Jensen Huang"]

用户输入："介绍一下半导体行业的竞争格局"
输出：["半导体"]

用户输入："苹果和特斯拉的股价对比"
输出：["苹果", "特斯拉"]

【当前用户输入】：
{{.Query}}

【输出】：
`
