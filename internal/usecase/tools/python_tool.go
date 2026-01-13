package tools

import (
	"context"
	"fmt"
	"go-nexus/internal/infrastructure/sandbox"
	"time"
)

var executor *sandbox.DockerExecutor

func InitPythonTool() error {
	var err error
	executor, err = sandbox.NewDockerExecutor()
	return err
}

func RunPythonCode(code string) string {
	if executor == nil {
		return "Error: Python executor not initialized."
	}
	fmt.Printf("Running Python Code:\n%s\n", code)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	output, err := executor.RunPython(ctx, code)
	if err != nil {
		errMsg := fmt.Sprintf("Execution Failed: %v", err)
		fmt.Printf("Docker Error: %s\n", errMsg)
		return errMsg
	}
	return output
}

func GetPythonToolSchema() string {
	return `{
		"type": "object",
		"properties": {
			"code": {
				"type": "string",
				"description": "Valid Python code to execute. Use print() to output results. Use this for math, data processing, or string manipulation."
			}
		},
		"required": ["code"]
	}`
}
