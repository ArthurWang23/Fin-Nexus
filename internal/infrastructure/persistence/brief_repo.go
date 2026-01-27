package persistence

import (
	"go-nexus/internal/domain"

	"gorm.io/gorm"
)

type PostgresBriefRepo struct {
	db *gorm.DB
}

func NewPostgresBriefRepo(db *gorm.DB) *PostgresBriefRepo {
	return &PostgresBriefRepo{db: db}
}

func (r *PostgresBriefRepo) Save(brief *domain.MorningBrief) error {
	return r.db.Create(brief).Error
}

func (r *PostgresBriefRepo) GetLatest(date string) ([]domain.MorningBrief, error) {
	var briefs []domain.MorningBrief
	// 如果 date 为空，取最近一天的
	if date == "" {

		var last domain.MorningBrief
		err := r.db.Order("date desc").First(&last).Error
		if err != nil {
			return nil, err
		}
		date = last.Date
	}
	err := r.db.Where("date = ?", date).Find(&briefs).Error
	return briefs, err
}
