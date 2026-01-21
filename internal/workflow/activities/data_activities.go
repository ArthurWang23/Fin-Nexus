package activities

import (
	"bytes"
	"context"
	"fmt"
	"go-nexus/internal/usecase"
	"go-nexus/internal/usecase/tools"
	"os"
	"text/template"
)

type DataActivities struct {
	agentUC *usecase.AgentUseCase
}

func NewDataActivities(uc *usecase.AgentUseCase) *DataActivities {
	return &DataActivities{agentUC: uc}
}

func (a *DataActivities) FetchAndIngest(ctx context.Context, ticker string) (string, error) {
	fmt.Printf("[FetchAndIngest] Starting fetch for ticker: %s\n", ticker)

	tmpl, err := template.New("fetch").Parse(tools.ScriptFetchFundamentals)
	if err != nil {
		return "", fmt.Errorf("failed to parse template: %w", err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, map[string]string{"Ticker": ticker}); err != nil {
		return "", fmt.Errorf("failed to execute template: %w", err)
	}

	fmt.Printf("[FetchAndIngest] Executing Python script for %s\n", ticker)
	output, files := tools.RunPythonCode(buf.String())
	fmt.Printf("[FetchAndIngest] Python execution output: %s\n", output)
	fmt.Printf("[FetchAndIngest] Generated files: %v\n", files)

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
	fmt.Printf("[FetchAndIngest] Read %d bytes from file\n", len(content))

	filename := fmt.Sprintf("AutoFetch_%s.txt", ticker)
	fmt.Printf("[FetchAndIngest] Ingesting knowledge with filename: %s\n", filename)
	err = a.agentUC.IngestKnowledge(ctx, content, filename)
	if err != nil {
		return "", fmt.Errorf("failed to ingest knowledge: %w", err)
	}

	fmt.Printf("[FetchAndIngest] Successfully ingested %s\n", ticker)
	return fmt.Sprintf("Successfully ingested %s", ticker), nil
}
