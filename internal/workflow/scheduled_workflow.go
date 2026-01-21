package workflow

import (
	"go.temporal.io/sdk/workflow"
	"time"
)

var TargetTickers = []string{"NVDA", "AAPL", "VOO", "GOOGL", "AMZN", "TSLA", "META", "TSM"}

func ScheduledDataIngestion(ctx workflow.Context) (string, error) {
	ao := workflow.ActivityOptions{
		StartToCloseTimeout: time.Minute * 5, // 每个股票给5分钟下载处理
	}
	ctx = workflow.WithActivityOptions(ctx, ao)

	results := "Batch Ingestion Report:\n"

	for _, ticker := range TargetTickers {
		var status string
		err := workflow.ExecuteActivity(ctx, "FetchAndIngest", ticker).Get(ctx, &status)
		if err != nil {
			results += "[FAIL] " + ticker + ": " + err.Error() + "\n"
			// 单个失败不影响整体，继续下一个
			continue
		}
		results += "[OK] " + ticker + ": " + status + "\n"
	}
	return results, nil
}
