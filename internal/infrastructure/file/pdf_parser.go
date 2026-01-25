package file

import (
	"bytes"
	"fmt"
	"github.com/dslipak/pdf"
	"io"
)

type PDFParser struct{}

func NewPDFParse() *PDFParser {
	return &PDFParser{}
}

func (p *PDFParser) Parse(reader io.ReaderAt, size int64) (string, error) {
	r, err := pdf.NewReader(reader, size)
	if err != nil {
		return "", fmt.Errorf("failed to create pdf reader: %w", err)
	}

	var buf bytes.Buffer

	for i := 1; i <= r.NumPage(); i++ {
		page := r.Page(i)
		if page.V.IsNull() {
			continue
		}
		text, err := page.GetPlainText(nil)
		if err != nil {
			continue
		}
		buf.WriteString(text)
		buf.WriteString("\n")
	}
	return buf.String(), nil
}
