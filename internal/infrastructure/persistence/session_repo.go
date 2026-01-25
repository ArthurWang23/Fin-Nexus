package persistence

import (
	"go-nexus/internal/domain"
	"gorm.io/gorm"
)

type PostgresSessionRepo struct {
	db *gorm.DB
}

func NewPostgresSessionRepo(db *gorm.DB) *PostgresSessionRepo {
	return &PostgresSessionRepo{db: db}
}

func (r *PostgresSessionRepo) CreateSession(session *domain.ChatSession) error {
	return r.db.Create(session).Error
}

func (r *PostgresSessionRepo) GetSessionByID(id string) (*domain.ChatSession, error) {
	var s domain.ChatSession
	err := r.db.First(&s, "id = ?", id).Error
	return &s, err
}

func (r *PostgresSessionRepo) ListSessions(userID string) ([]domain.ChatSession, error) {
	var sessions []domain.ChatSession
	err := r.db.Where("user_id = ?", userID).Order("updated_at desc").Find(&sessions).Error
	return sessions, err
}

func (r *PostgresSessionRepo) SaveMessage(msg *domain.ChatMessage) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(msg).Error; err != nil {
			return err
		}
		return tx.Model(&domain.ChatSession{}).
			Where("id = ?", msg.SessionID).
			Update("updated_at", msg.CreatedAt).Error
	})
}

func (r *PostgresSessionRepo) GetMessages(sessionID string) ([]domain.ChatMessage, error) {
	var msgs []domain.ChatMessage
	err := r.db.Where("session_id = ?", sessionID).Order("created_at asc").Find(&msgs).Error
	return msgs, err
}
