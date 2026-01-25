package file

import (
	"io"
	"path/filepath"
	"strings"
)

// Parser 定义文件解析器接口
type Parser interface {
	// Parse 将文件内容解析为纯文本
	Parse(reader io.ReaderAt, size int64) (string, error)
}

// GetParser 根据文件扩展名返回对应的解析器
func GetParser(filename string) (Parser, error) {
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(filename), "."))

	switch ext {
	case "pdf":
		return NewPDFParse(), nil
	case "txt", "text", "md", "markdown":
		return NewTextParser(), nil
	default:
		// 默认使用文本解析器，尝试按文本处理
		return NewTextParser(), nil
	}
}

// IsSupportedFileType 检查文件类型是否支持
func IsSupportedFileType(filename string) bool {
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(filename), "."))
	supported := []string{"pdf", "txt", "text", "md", "markdown"}
	for _, s := range supported {
		if ext == s {
			return true
		}
	}
	return false
}
