package persistence

import (
	"fmt"
	"go-nexus/internal/domain"
	"go-nexus/pkg/crypto"

	"gorm.io/gorm"
)

type PostgresConfigRepo struct {
	db        *gorm.DB
	masterKey []byte // AES-256 主密钥
}

// NewConfigRepo 创建配置仓库
// masterKey 用于加密/解密 API Key，必须是 32 字节
func NewConfigRepo(db *gorm.DB, masterKey []byte) *PostgresConfigRepo {
	return &PostgresConfigRepo{db: db, masterKey: masterKey}
}

func (r *PostgresConfigRepo) SaveConfig(config *domain.UserModelConfig) error {
	// 创建副本，避免修改原始对象
	configToSave := *config

	// 加密 API Key
	if configToSave.APIKey != "" && r.masterKey != nil {
		encrypted, err := crypto.Encrypt(configToSave.APIKey, r.masterKey)
		if err != nil {
			return fmt.Errorf("failed to encrypt api key: %w", err)
		}
		configToSave.APIKey = encrypted
	}

	return r.db.Save(&configToSave).Error
}

func (r *PostgresConfigRepo) GetConfig(userID string, agentType domain.AgentType) (*domain.UserModelConfig, error) {
	var userConfig domain.UserModelConfig
	err := r.db.Where("user_id = ? AND agent_type = ?", userID, agentType).Find(&userConfig).Error
	if err != nil {
		return nil, fmt.Errorf("failed to load user config: %v", err)
	}

	// 解密 API Key
	if userConfig.APIKey != "" && r.masterKey != nil {
		decrypted, err := crypto.Decrypt(userConfig.APIKey, r.masterKey)
		if err != nil {
			return nil, fmt.Errorf("failed to decrypt api key: %w", err)
		}
		userConfig.APIKey = decrypted
	}

	return &userConfig, nil
}

func (r *PostgresConfigRepo) GetAllConfigs(userID string) ([]domain.UserModelConfig, error) {
	var configs []domain.UserModelConfig
	err := r.db.Where("user_id = ?", userID).Find(&configs).Error
	if err != nil {
		return nil, fmt.Errorf("failed to load user configs: %v", err)
	}

	// 解密所有 API Key
	for i := range configs {
		if configs[i].APIKey != "" && r.masterKey != nil {
			decrypted, err := crypto.Decrypt(configs[i].APIKey, r.masterKey)
			if err != nil {
				return nil, fmt.Errorf("failed to decrypt api key: %w", err)
			}
			configs[i].APIKey = decrypted
		}
	}

	return configs, nil
}

func (r *PostgresConfigRepo) DeleteConfig(userID string, agentType domain.AgentType) error {
	return r.db.Where("user_id = ? AND agent_type = ?", userID, agentType).Delete(&domain.UserModelConfig{}).Error
}
