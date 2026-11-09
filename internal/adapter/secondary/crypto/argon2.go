package crypto

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strings"

	"golang.org/x/crypto/argon2"

	"github.com/matrocheetos/telegram-monitor/internal/port"
)

var (
	ErrInvalidHashFormat = errors.New("crypto: invalid argon2id hash format")
	ErrIncompatibleHash  = errors.New("crypto: incompatible argon2id hash")
)

type Argon2idHasher struct {
	time    uint32
	memory  uint32
	threads uint8
	keyLen  uint32
	saltLen uint32
}

var _ port.PasswordHasher = (*Argon2idHasher)(nil)

func NewArgon2idHasher() *Argon2idHasher {
	return &Argon2idHasher{
		time:    3,
		memory:  64 * 1024,
		threads: 4,
		keyLen:  32,
		saltLen: 16,
	}
}

func (h *Argon2idHasher) HashPassword(password string) (string, error) {
	salt := make([]byte, h.saltLen)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return "", fmt.Errorf("crypto: salt generation failed: %w", err)
	}

	hash := argon2.IDKey([]byte(password), salt, h.time, h.memory, h.threads, h.keyLen)

	b64Salt := base64.RawStdEncoding.EncodeToString(salt)
	b64Hash := base64.RawStdEncoding.EncodeToString(hash)

	encoded := fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, h.memory, h.time, h.threads, b64Salt, b64Hash)

	return encoded, nil
}

func (h *Argon2idHasher) VerifyPassword(password, encodedHash string) (bool, error) {
	parts := strings.Split(encodedHash, "$")
	if len(parts) != 6 {
		return false, ErrInvalidHashFormat
	}

	if parts[1] != "argon2id" {
		return false, ErrIncompatibleHash
	}

	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return false, ErrIncompatibleHash
	}

	var memory, iterTime uint32
	var threads uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &iterTime, &threads); err != nil {
		return false, ErrInvalidHashFormat
	}

	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false, ErrInvalidHashFormat
	}

	expectedHash, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return false, ErrInvalidHashFormat
	}

	if len(expectedHash) == 0 || len(expectedHash) > 1024 {
		return false, ErrInvalidHashFormat
	}

	// #nosec G115 -- expectedHash length is strictly checked <= 1024
	computedHash := argon2.IDKey([]byte(password), salt, iterTime, memory, threads, uint32(len(expectedHash)))

	if subtle.ConstantTimeCompare(computedHash, expectedHash) == 1 {
		return true, nil
	}

	return false, nil
}
