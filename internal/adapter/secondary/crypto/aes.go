package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"

	"github.com/matrocheetos/telegram-monitor/internal/port"
)

var (
	ErrCiphertextTooShort = errors.New("crypto: ciphertext too short")
	ErrDecryptionFailed   = errors.New("crypto: decryption failed")
	ErrEmptySecretKey     = errors.New("crypto: secret key is empty")
)

type AESGCMCrypto struct {
	aead cipher.AEAD
}

var _ port.CryptoService = (*AESGCMCrypto)(nil)

func NewAESGCMCrypto(secretKey string) (*AESGCMCrypto, error) {
	if secretKey == "" {
		return nil, ErrEmptySecretKey
	}

	key, err := hkdf.Key(sha256.New, []byte(secretKey), nil, "telegram-monitor-aes-256-gcm", 32)
	if err != nil {
		return nil, fmt.Errorf("crypto: key derivation failed: %w", err)
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("crypto: failed to create cipher: %w", err)
	}

	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("crypto: failed to create gcm: %w", err)
	}

	return &AESGCMCrypto{aead: aead}, nil
}

func (c *AESGCMCrypto) Encrypt(plaintext []byte) ([]byte, error) {
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("crypto: failed to generate nonce: %w", err)
	}

	return c.aead.Seal(nonce, nonce, plaintext, nil), nil
}

func (c *AESGCMCrypto) Decrypt(ciphertext []byte) ([]byte, error) {
	nonceSize := c.aead.NonceSize()
	if len(ciphertext) < nonceSize+c.aead.Overhead() {
		return nil, ErrCiphertextTooShort
	}

	nonce := ciphertext[:nonceSize]
	encrypted := ciphertext[nonceSize:]

	plaintext, err := c.aead.Open(nil, nonce, encrypted, nil)
	if err != nil {
		return nil, ErrDecryptionFailed
	}

	return plaintext, nil
}
