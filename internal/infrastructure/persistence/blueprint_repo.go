package persistence

import (
	"encoding/json"
	"fmt"
	"go-nexus/internal/domain"
	"go-nexus/pkg/crypto"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// PostgresBlueprintRepo Blueprint 仓库实现
type PostgresBlueprintRepo struct {
	db        *gorm.DB
	masterKey []byte // AES-256 主密钥，用于加密 API Key
}

// NewBlueprintRepo 创建 Blueprint 仓库
func NewBlueprintRepo(db *gorm.DB, masterKey []byte) *PostgresBlueprintRepo {
	return &PostgresBlueprintRepo{db: db, masterKey: masterKey}
}

// Create 创建新的 Blueprint
func (r *PostgresBlueprintRepo) Create(bp *domain.WorkflowBlueprint) error {
	if bp.ID == "" {
		bp.ID = uuid.New().String()
	}
	bp.CreatedAt = time.Now()
	bp.UpdatedAt = time.Now()
	bp.Version = 1

	// 加密 API Key
	bpToSave := *bp
	if err := r.encryptLLMConfig(&bpToSave); err != nil {
		return fmt.Errorf("failed to encrypt llm config: %w", err)
	}

	return r.db.Create(&bpToSave).Error
}

// Update 更新 Blueprint
func (r *PostgresBlueprintRepo) Update(bp *domain.WorkflowBlueprint) error {
	bp.UpdatedAt = time.Now()
	bp.Version++

	// 加密 API Key
	bpToSave := *bp
	if err := r.encryptLLMConfig(&bpToSave); err != nil {
		return fmt.Errorf("failed to encrypt llm config: %w", err)
	}

	return r.db.Save(&bpToSave).Error
}

// Delete 删除 Blueprint（仅限所有者）
func (r *PostgresBlueprintRepo) Delete(id string, userID string) error {
	result := r.db.Where("id = ? AND user_id = ?", id, userID).Delete(&domain.WorkflowBlueprint{})
	if result.RowsAffected == 0 {
		return fmt.Errorf("blueprint not found or access denied")
	}
	return result.Error
}

// GetByID 根据 ID 获取 Blueprint（不解密 API Key）
func (r *PostgresBlueprintRepo) GetByID(id string) (*domain.WorkflowBlueprint, error) {
	var bp domain.WorkflowBlueprint
	err := r.db.Where("id = ?", id).First(&bp).Error
	if err != nil {
		return nil, err
	}
	// 清空所有 API Key，不返回给前端
	bp.LLMConfig.APIKey = ""
	r.clearNodeAPIKeys(&bp)
	return &bp, nil
}

// GetByIDWithDecryption 根据 ID 获取 Blueprint 并解密 API Key（仅用于内部执行）
func (r *PostgresBlueprintRepo) GetByIDWithDecryption(id string) (*domain.WorkflowBlueprint, error) {
	var bp domain.WorkflowBlueprint
	err := r.db.Where("id = ?", id).First(&bp).Error
	if err != nil {
		return nil, err
	}

	// 解密 API Key
	if err := r.decryptLLMConfig(&bp); err != nil {
		return nil, fmt.Errorf("failed to decrypt llm config: %w", err)
	}

	return &bp, nil
}

// ListByUser 列出用户的所有 Blueprint
func (r *PostgresBlueprintRepo) ListByUser(userID string) ([]domain.WorkflowBlueprint, error) {
	var blueprints []domain.WorkflowBlueprint
	err := r.db.Where("user_id = ?", userID).Order("updated_at DESC").Find(&blueprints).Error
	if err != nil {
		return nil, err
	}

	// 清空所有 API Key
	for i := range blueprints {
		blueprints[i].LLMConfig.APIKey = ""
		r.clearNodeAPIKeys(&blueprints[i])
	}

	return blueprints, nil
}

// ListPublic 列出所有公开的 Blueprint
func (r *PostgresBlueprintRepo) ListPublic() ([]domain.WorkflowBlueprint, error) {
	var blueprints []domain.WorkflowBlueprint
	err := r.db.Where("is_public = ?", true).Order("updated_at DESC").Find(&blueprints).Error
	if err != nil {
		return nil, err
	}

	// 清空所有 API Key
	for i := range blueprints {
		blueprints[i].LLMConfig.APIKey = ""
		r.clearNodeAPIKeys(&blueprints[i])
	}

	return blueprints, nil
}

// encryptLLMConfig 加密 Blueprint 中的所有 API Key（包括节点级别）
func (r *PostgresBlueprintRepo) encryptLLMConfig(bp *domain.WorkflowBlueprint) error {
	if r.masterKey == nil {
		return nil
	}

	// 1. 加密 Blueprint 级别的 API Key
	if bp.LLMConfig.APIKey != "" {
		encrypted, err := crypto.Encrypt(bp.LLMConfig.APIKey, r.masterKey)
		if err != nil {
			return err
		}
		bp.LLMConfig.APIKey = encrypted
	}

	// 2. 加密节点级别的 API Key
	for i := range bp.Nodes {
		if bp.Nodes[i].Type == domain.NodeTypeLLM {
			if err := r.encryptNodeConfig(&bp.Nodes[i]); err != nil {
				return err
			}
		}
	}

	return nil
}

// decryptLLMConfig 解密 Blueprint 中的所有 API Key（包括节点级别）
func (r *PostgresBlueprintRepo) decryptLLMConfig(bp *domain.WorkflowBlueprint) error {
	if r.masterKey == nil {
		return nil
	}

	// 1. 解密 Blueprint 级别的 API Key
	if bp.LLMConfig.APIKey != "" {
		decrypted, err := crypto.Decrypt(bp.LLMConfig.APIKey, r.masterKey)
		if err != nil {
			return err
		}
		bp.LLMConfig.APIKey = decrypted
	}

	// 2. 解密节点级别的 API Key
	for i := range bp.Nodes {
		if bp.Nodes[i].Type == domain.NodeTypeLLM {
			if err := r.decryptNodeConfig(&bp.Nodes[i]); err != nil {
				return err
			}
		}
	}

	return nil
}

// encryptNodeConfig 加密节点配置中的 API Key
func (r *PostgresBlueprintRepo) encryptNodeConfig(node *domain.GraphNode) error {
	// 将 interface{} 转换为 LLMNodeConfig
	configBytes, err := json.Marshal(node.Config)
	if err != nil {
		return fmt.Errorf("failed to marshal node config: %w", err)
	}

	var llmConfig domain.LLMNodeConfig
	if err := json.Unmarshal(configBytes, &llmConfig); err != nil {
		return fmt.Errorf("failed to unmarshal node config: %w", err)
	}

	// 如果有节点级别的 API Key，加密它
	if llmConfig.LLMConfig != nil && llmConfig.LLMConfig.APIKey != "" {
		encrypted, err := crypto.Encrypt(llmConfig.LLMConfig.APIKey, r.masterKey)
		if err != nil {
			return err
		}
		llmConfig.LLMConfig.APIKey = encrypted
		node.Config = llmConfig
	}

	return nil
}

// decryptNodeConfig 解密节点配置中的 API Key
func (r *PostgresBlueprintRepo) decryptNodeConfig(node *domain.GraphNode) error {
	// 将 interface{} 转换为 LLMNodeConfig
	configBytes, err := json.Marshal(node.Config)
	if err != nil {
		return fmt.Errorf("failed to marshal node config: %w", err)
	}

	var llmConfig domain.LLMNodeConfig
	if err := json.Unmarshal(configBytes, &llmConfig); err != nil {
		return fmt.Errorf("failed to unmarshal node config: %w", err)
	}

	// 如果有节点级别的 API Key，解密它
	if llmConfig.LLMConfig != nil && llmConfig.LLMConfig.APIKey != "" {
		decrypted, err := crypto.Decrypt(llmConfig.LLMConfig.APIKey, r.masterKey)
		if err != nil {
			return err
		}
		llmConfig.LLMConfig.APIKey = decrypted
		node.Config = llmConfig
	}

	return nil
}

// clearNodeAPIKeys 清空节点中的 API Key（用于返回给前端）
func (r *PostgresBlueprintRepo) clearNodeAPIKeys(bp *domain.WorkflowBlueprint) {
	for i := range bp.Nodes {
		if bp.Nodes[i].Type == domain.NodeTypeLLM {
			configBytes, err := json.Marshal(bp.Nodes[i].Config)
			if err != nil {
				continue
			}

			var llmConfig domain.LLMNodeConfig
			if err := json.Unmarshal(configBytes, &llmConfig); err != nil {
				continue
			}

			if llmConfig.LLMConfig != nil {
				llmConfig.LLMConfig.APIKey = ""
				bp.Nodes[i].Config = llmConfig
			}
		}
	}
}
