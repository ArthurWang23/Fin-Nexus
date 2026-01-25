package usecase

import (
	"context"
	"errors"
	"go-nexus/pkg/auth"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"go-nexus/internal/domain"
	"go-nexus/internal/usecase/repo"
)

// os.Getenv("JWT_SECRET")
var jwtSecret = []byte("my-jwt-secret")

type AuthUseCase struct {
	userRepo repo.UserRepository
}

func NewAuthUseCase(userRepo repo.UserRepository) *AuthUseCase {
	return &AuthUseCase{userRepo: userRepo}
}

func (uc *AuthUseCase) Register(ctx context.Context, email, password string) (*domain.User, error) {
	existing, err := uc.userRepo.GetByEmail(ctx, email)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return nil, errors.New("email already exists")
	}

	hashedPwd, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}

	user := &domain.User{
		ID:           uuid.New().String(),
		Email:        email,
		PasswordHash: string(hashedPwd),
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}

	if err := uc.userRepo.Create(ctx, user); err != nil {
		return nil, err
	}

	return user, nil
}

func (uc *AuthUseCase) Login(ctx context.Context, email, password string) (string, error) {
	user, err := uc.userRepo.GetByEmail(ctx, email)
	if err != nil {
		return "", err
	}
	if user == nil {
		return "", errors.New("invalid credentials")
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
		return "", errors.New("invalid credentials")
	}
	return generateToken(user.ID)
}

func generateToken(userID string) (string, error) {
	return auth.GenerateToken(userID)
}
