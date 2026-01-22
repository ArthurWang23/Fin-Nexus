package activities

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"go-nexus/internal/domain"
	"go-nexus/internal/usecase"
	"go-nexus/internal/usecase/tools"
	"os"
	"strings"
	"text/template"
	"time"

	"github.com/google/uuid"
)

// ScriptMeta 定义解析结构
type ScriptMeta struct {
	Ticker      string  `json:"ticker"`
	Date        string  `json:"date"`
	PriceChange float64 `json:"price_change"`
	HasNews     bool    `json:"has_news"`
}
type DataActivities struct {
	agentUC   *usecase.AgentUseCase
	briefRepo domain.BriefRepository
	rdb       *redis.Client
}

func NewDataActivities(uc *usecase.AgentUseCase, repo domain.BriefRepository, rdb *redis.Client) *DataActivities {
	return &DataActivities{agentUC: uc, briefRepo: repo, rdb: rdb}
}

func (a *DataActivities) FetchAndIngest(ctx context.Context, ticker string) (string, error) {
	fmt.Printf("[FetchAndIngest] Starting fetch for ticker: %s\n", ticker)
	tmpl, err := template.New("fetch").Parse(tools.ScriptFetchDailyData)
	if err != nil {
		return "", fmt.Errorf("failed to parse template: %w", err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, map[string]string{"Ticker": ticker}); err != nil {
		return "", fmt.Errorf("failed to execute template: %w", err)
	}
	output, files := tools.RunPythonCode(buf.String())

	if len(files) == 0 {
		return "", fmt.Errorf("no data file generated for %s. Output: %s", ticker, output)
	}

	// files[0] 可能是 "/images/xxx.png" 或类似路径，需要正确处理
	localPath := "./public" + files[0]
	fmt.Printf("[FetchAndIngest] Reading file from: %s\n", localPath)
	contentBytes, err := os.ReadFile(localPath)
	if err != nil {
		return "", fmt.Errorf("failed to read file %s: %w", localPath, err)
	}
	content := string(contentBytes)
	err = a.agentUC.IngestKnowledge(ctx, content, files[0])
	if err != nil {
		return "", fmt.Errorf("failed to ingest knowledge: %w", err)
	}

	// 解析 Meta 数据
	meta := parseMetaFromLogs(output)
	if meta.Date == "" {
		meta.Date = time.Now().Format("2006-01-02")
	}
	// 存入 Postgres
	brief := &domain.MorningBrief{
		ID:             uuid.New().String(),
		Ticker:         ticker,
		Date:           meta.Date,
		RawDataSummary: content,
		PriceChange:    meta.PriceChange,
		CreatedAt:      time.Now(),
	}
	if err := a.briefRepo.Save(brief); err != nil {
		fmt.Printf(" Failed to save brief to DB: %v\n", err)
	}

	// 存入redis 快报
	redisKey := fmt.Sprintf("brief:raw:%s:latest", ticker)
	a.rdb.Set(ctx, redisKey, content, 24*time.Hour)
	fmt.Printf("[FetchAndIngest] Successfully ingested %s\n", ticker)
	return fmt.Sprintf("Ingested %s (Chg: %.2f%%)", ticker, meta.PriceChange), nil
}

func parseMetaFromLogs(logs string) ScriptMeta {
	var meta ScriptMeta
	lines := strings.Split(logs, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "__META__:") {
			jsonStr := strings.TrimPrefix(line, "__META__:")
			_ = json.Unmarshal([]byte(jsonStr), &meta)
			break
		}
	}
	return meta
}
