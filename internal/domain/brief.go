package domain

import (
	"time"
)

type MorningBrief struct {
	ID     string `gorm:"primaryKey" json:"id"`
	Ticker string `gorm:"index:idx_ticker_date" json:"ticker"` // 联合索引
	Date   string `gorm:"index:idx_ticker_date" json:"date"`   // YYYY-MM-DD

	RawDataSummary string `gorm:"type:text" json:"raw_data"`
	FinalReport    string `gorm:"type:text" json:"report"` // LLM 生成的最终 Markdown

	// 结构化元数据 (来自 Python __META__)
	PriceChange float64 `json:"price_change"`
	HasNews     bool    `json:"has_news"`

	CreatedAt time.Time `json:"created_at"`
}

type BriefRepository interface {
	Save(brief *MorningBrief) error
	GetLatest(date string) ([]MorningBrief, error)
}
