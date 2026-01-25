package file

import (
	"io"
)

// TextParser 文本文件解析器
type TextParser struct{}

// NewTextParser 创建新的文本文件解析器
func NewTextParser() *TextParser {
	return &TextParser{}
}

// Parse 将文本文件内容读取为字符串
// 使用io.ReaderAt接口以保持与PDFParser的一致性
func (p *TextParser) Parse(reader io.ReaderAt, size int64) (string, error) {
	// 使用SectionReader从指定位置读取指定大小的数据
	sectionReader := io.NewSectionReader(reader, 0, size)

	// 读取所有内容
	data, err := io.ReadAll(sectionReader)
	if err != nil {
		return "", err
	}

	// 直接返回UTF-8字符串
	// 如果需要支持其他编码（如GBK），可以在这里添加转换逻辑
	return string(data), nil
}
