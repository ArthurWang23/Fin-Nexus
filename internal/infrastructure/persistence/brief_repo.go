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
