package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
)

const (
	// MasterKeyEnvVar 主密钥环境变量名
	MasterKeyEnvVar = "API_KEY_MASTER_KEY"
	// KeySize AES-256 需要 32 字节密钥
	KeySize = 32
)

var (
	ErrMasterKeyNotSet    = errors.New("master key environment variable not set")
	ErrMasterKeyInvalid   = errors.New("master key must be 32 bytes (base64 encoded)")
	ErrCiphertextTooShort = errors.New("ciphertext too short")
	ErrDecryptionFailed   = errors.New("decryption failed")
)

// GetMasterKey 从环境变量获取主密钥
// 主密钥应该是 base64 编码的 32 字节随机数据
func GetMasterKey() ([]byte, error) {
	encoded := os.Getenv(MasterKeyEnvVar)
	if encoded == "" {
		return nil, ErrMasterKeyNotSet
	}

	key, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("failed to decode master key: %w", err)
	}

	if len(key) != KeySize {
		return nil, ErrMasterKeyInvalid
	}

	return key, nil
}

// Encrypt 使用 AES-256-GCM 加密明文
// 返回 base64 编码的密文 (nonce + ciphertext)
func Encrypt(plaintext string, key []byte) (string, error) {
	if plaintext == "" {
		return "", nil
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("failed to create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("failed to create GCM: %w", err)
	}

	// 生成随机 nonce
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("failed to generate nonce: %w", err)
	}

	// 加密并附加认证标签
	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)

	// Base64 编码输出
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

// Decrypt 解密 AES-256-GCM 密文
// 输入为 base64 编码的密文 (nonce + ciphertext)
func Decrypt(ciphertext string, key []byte) (string, error) {
	if ciphertext == "" {
		return "", nil
	}

	data, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		return "", fmt.Errorf("failed to decode ciphertext: %w", err)
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("failed to create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("failed to create GCM: %w", err)
	}

	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return "", ErrCiphertextTooShort
	}

	nonce, ciphertextBytes := data[:nonceSize], data[nonceSize:]

	plaintext, err := gcm.Open(nil, nonce, ciphertextBytes, nil)
	if err != nil {
		return "", ErrDecryptionFailed
	}

	return string(plaintext), nil
}

// GenerateMasterKey 生成新的主密钥 (用于初始设置)
// 返回 base64 编码的 32 字节随机密钥
func GenerateMasterKey() (string, error) {
	key := make([]byte, KeySize)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return "", fmt.Errorf("failed to generate key: %w", err)
	}
	return base64.StdEncoding.EncodeToString(key), nil
}
