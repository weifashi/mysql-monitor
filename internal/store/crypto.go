package store

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"

	"golang.org/x/crypto/pbkdf2"
)

const (
	pbkdf2Iterations = 100000
	keyLen           = 32 // AES-256
	saltLen          = 16
	encPrefix        = "enc:"
)

var encryptionKey []byte

// InitEncryption derives a 256-bit AES key from the admin password using PBKDF2.
// Must be called before any Store operations that read/write passwords.
func InitEncryption(adminPassword string) {
	salt := []byte("mysql-monitor-fixed-salt-v1")
	encryptionKey = pbkdf2.Key([]byte(adminPassword), salt, pbkdf2Iterations, keyLen, sha256.New)
}

// Encrypt encrypts plaintext using AES-256-GCM and returns a base64-encoded string prefixed with "enc:".
func Encrypt(plaintext string) (string, error) {
	if len(encryptionKey) == 0 {
		return plaintext, nil
	}
	block, err := aes.NewCipher(encryptionKey)
	if err != nil {
		return "", fmt.Errorf("create cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("create gcm: %w", err)
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("generate nonce: %w", err)
	}
	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return encPrefix + base64.StdEncoding.EncodeToString(ciphertext), nil
}

// Decrypt decrypts an "enc:"-prefixed base64 string. If the string is not prefixed, returns it as-is (legacy plaintext).
func Decrypt(encrypted string) (string, error) {
	if len(encrypted) < len(encPrefix) || encrypted[:len(encPrefix)] != encPrefix {
		return encrypted, nil // legacy plaintext
	}
	if len(encryptionKey) == 0 {
		return "", fmt.Errorf("encryption key not initialized")
	}
	data, err := base64.StdEncoding.DecodeString(encrypted[len(encPrefix):])
	if err != nil {
		return "", fmt.Errorf("decode base64: %w", err)
	}
	block, err := aes.NewCipher(encryptionKey)
	if err != nil {
		return "", fmt.Errorf("create cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("create gcm: %w", err)
	}
	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return "", fmt.Errorf("ciphertext too short")
	}
	plaintext, err := gcm.Open(nil, data[:nonceSize], data[nonceSize:], nil)
	if err != nil {
		return "", fmt.Errorf("decrypt: %w", err)
	}
	return string(plaintext), nil
}
