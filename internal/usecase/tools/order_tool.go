package tools

import "fmt"

// 1. 实际执行的业务函数
// 模拟查库：输入订单号，返回状态
func GetOrderInfo(orderID string) string {
	// 模拟数据库数据
	mockDB := map[string]string{
		"ORD-123": "已发货，当前位置：上海分拣中心，预计明日送达。",
		"ORD-456": "仓库配货中，等待揽收。",
		"ORD-999": "订单支付失败，已自动取消。",
	}

	status, exists := mockDB[orderID]
	if !exists {
		return "未找到该订单信息，请检查订单号是否正确 (示例: ORD-123)。"
	}
	return fmt.Sprintf("订单 %s 的状态：%s", orderID, status)
}

// 2. 返回给 LLM 的 JSON Schema 描述
// Qwen 会根据这个描述来决定是否调用，以及如何提取参数
func GetOrderToolSchema() string {
	return `{
		"type": "object",
		"properties": {
			"order_id": {
				"type": "string",
				"description": "The order ID, for example: ORD-123, ORD-456"
			}
		},
		"required": ["order_id"]
	}`
}
