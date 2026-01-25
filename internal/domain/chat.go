package domain

import "time"

// ChatSession 代表对话窗口
type ChatSession struct {
	ID        string    `gorm:"primaryKey" json:"id"`
	UserID    string    `gorm:"index" json:"user_id"`
	Title     string    `json:"title"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type ChatMessage struct {
	ID        string    `gorm:"primaryKey" json:"id"`
	SessionID string    `gorm:"index" json:"session_id"` // 归属会话
	Role      string    `json:"role"`                    // user, assistant
	Content   string    `gorm:"type:text" json:"content"`
	CreatedAt time.Time `json:"created_at"`
}

type SessionRepository interface {
	CreateSession(session *ChatSession) error
	GetSessionByID(id string) (*ChatSession, error)
	ListSessions(userID string) ([]ChatSession, error)

	SaveMessage(msg *ChatMessage) error
	GetMessages(sessionID string) ([]ChatMessage, error)
}
