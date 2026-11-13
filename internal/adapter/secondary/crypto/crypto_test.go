package crypto_test

import (
	"bytes"
	"errors"
	"testing"

	"github.com/matrocheetos/telegram-monitor/internal/adapter/secondary/crypto"
)

func TestAESGCMCrypto(t *testing.T) {
	t.Parallel()

	secretKey := "test-secret-key-32-bytes-long!!"

	t.Run("successful roundtrip", func(t *testing.T) {
		t.Parallel()
		crypt, err := crypto.NewAESGCMCrypto(secretKey)
		if err != nil {
			t.Fatalf("unexpected error creating crypto: %v", err)
		}

		plaintext := []byte("confidential-telegram-session-token-12345")
		ciphertext, err := crypt.Encrypt(plaintext)
		if err != nil {
			t.Fatalf("encryption failed: %v", err)
		}

		if bytes.Equal(ciphertext, plaintext) {
			t.Fatalf("ciphertext matches plaintext")
		}

		decrypted, err := crypt.Decrypt(ciphertext)
		if err != nil {
			t.Fatalf("decryption failed: %v", err)
		}

		if !bytes.Equal(decrypted, plaintext) {
			t.Fatalf("decrypted %s does not match plaintext %s", decrypted, plaintext)
		}
	})

	t.Run("empty secret key", func(t *testing.T) {
		t.Parallel()
		_, err := crypto.NewAESGCMCrypto("")
		if !errors.Is(err, crypto.ErrEmptySecretKey) {
			t.Fatalf("expected ErrEmptySecretKey, got %v", err)
		}
	})

	t.Run("tampered ciphertext fails", func(t *testing.T) {
		t.Parallel()
		crypt, err := crypto.NewAESGCMCrypto(secretKey)
		if err != nil {
			t.Fatalf("unexpected error creating crypto: %v", err)
		}

		plaintext := []byte("secret-payload")
		ciphertext, err := crypt.Encrypt(plaintext)
		if err != nil {
			t.Fatalf("encryption failed: %v", err)
		}

		// Tamper with last byte
		tampered := make([]byte, len(ciphertext))
		copy(tampered, ciphertext)
		tampered[len(tampered)-1] ^= 0xFF

		_, err = crypt.Decrypt(tampered)
		if !errors.Is(err, crypto.ErrDecryptionFailed) {
			t.Fatalf("expected ErrDecryptionFailed on tampered ciphertext, got %v", err)
		}
	})

	t.Run("ciphertext too short", func(t *testing.T) {
		t.Parallel()
		crypt, err := crypto.NewAESGCMCrypto(secretKey)
		if err != nil {
			t.Fatalf("unexpected error creating crypto: %v", err)
		}

		tooShort := []byte("short")
		_, err = crypt.Decrypt(tooShort)
		if !errors.Is(err, crypto.ErrCiphertextTooShort) {
			t.Fatalf("expected ErrCiphertextTooShort, got %v", err)
		}
	})

	t.Run("nonces are randomized", func(t *testing.T) {
		t.Parallel()
		crypt, err := crypto.NewAESGCMCrypto(secretKey)
		if err != nil {
			t.Fatalf("unexpected error creating crypto: %v", err)
		}

		plaintext := []byte("same-plaintext")
		c1, err := crypt.Encrypt(plaintext)
		if err != nil {
			t.Fatalf("encrypt 1 failed: %v", err)
		}
		c2, err := crypt.Encrypt(plaintext)
		if err != nil {
			t.Fatalf("encrypt 2 failed: %v", err)
		}

		if bytes.Equal(c1, c2) {
			t.Fatalf("subsequent encryptions produced identical ciphertexts (reused nonce)")
		}
	})
}

func TestArgon2idHasher(t *testing.T) {
	t.Parallel()

	hasher := crypto.NewArgon2idHasher()
	password := "SecretP@ssw0rd123!"

	t.Run("hash and verify success", func(t *testing.T) {
		t.Parallel()
		encoded, err := hasher.HashPassword(password)
		if err != nil {
			t.Fatalf("hash failed: %v", err)
		}

		match, err := hasher.VerifyPassword(password, encoded)
		if err != nil {
			t.Fatalf("verification error: %v", err)
		}
		if !match {
			t.Fatalf("password should match")
		}
	})

	t.Run("wrong password rejected", func(t *testing.T) {
		t.Parallel()
		encoded, err := hasher.HashPassword(password)
		if err != nil {
			t.Fatalf("hash failed: %v", err)
		}

		match, err := hasher.VerifyPassword("WrongPassword123!", encoded)
		if err != nil {
			t.Fatalf("verification error: %v", err)
		}
		if match {
			t.Fatalf("wrong password should not match")
		}
	})

	t.Run("invalid hash format", func(t *testing.T) {
		t.Parallel()
		_, err := hasher.VerifyPassword(password, "not-a-valid-argon2id-hash")
		if !errors.Is(err, crypto.ErrInvalidHashFormat) {
			t.Fatalf("expected ErrInvalidHashFormat, got %v", err)
		}
	})

	t.Run("zero argon2 parameters rejected safely", func(t *testing.T) {
		t.Parallel()
		_, err := hasher.VerifyPassword(password, "$argon2id$v=19$m=65536,t=0,p=0$c2FsdA$aGFzaA")
		if !errors.Is(err, crypto.ErrInvalidHashFormat) {
			t.Fatalf("expected ErrInvalidHashFormat, got %v", err)
		}
	})

	t.Run("incompatible algorithm", func(t *testing.T) {
		t.Parallel()
		_, err := hasher.VerifyPassword(password, "$bcrypt$v=19$m=65536,t=3,p=4$salt$hash")
		if !errors.Is(err, crypto.ErrIncompatibleHash) {
			t.Fatalf("expected ErrIncompatibleHash, got %v", err)
		}
	})

	t.Run("salts are unique across hashes", func(t *testing.T) {
		t.Parallel()
		h1, err := hasher.HashPassword(password)
		if err != nil {
			t.Fatalf("hash 1 failed: %v", err)
		}
		h2, err := hasher.HashPassword(password)
		if err != nil {
			t.Fatalf("hash 2 failed: %v", err)
		}

		if h1 == h2 {
			t.Fatalf("identical passwords produced identical hashes (reused salt)")
		}
	})
}
