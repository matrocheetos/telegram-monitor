package gotd

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/gotd/td/session"

	"github.com/matrocheetos/telegram-monitor/internal/domain"
	"github.com/matrocheetos/telegram-monitor/internal/port"
)

type SessionStorage struct {
	repo   port.CredentialRepository
	crypto port.CryptoService
	mu     sync.Mutex
}

var _ session.Storage = (*SessionStorage)(nil)

func NewSessionStorage(repo port.CredentialRepository, crypto port.CryptoService) *SessionStorage {
	return &SessionStorage{
		repo:   repo,
		crypto: crypto,
	}
}

func (s *SessionStorage) LoadSession(ctx context.Context) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	creds, err := s.repo.GetTelegramCredentials(ctx)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return nil, session.ErrNotFound
		}
		return nil, fmt.Errorf("session storage: get credentials failed: %w", err)
	}

	if len(creds.EncryptedSession) == 0 {
		return nil, session.ErrNotFound
	}

	decrypted, err := s.crypto.Decrypt(creds.EncryptedSession)
	if err != nil {
		return nil, fmt.Errorf("session storage: decrypt failed: %w", err)
	}

	return decrypted, nil
}

func (s *SessionStorage) StoreSession(ctx context.Context, data []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	encrypted, err := s.crypto.Encrypt(data)
	if err != nil {
		return fmt.Errorf("session storage: encrypt failed: %w", err)
	}

	creds, err := s.repo.GetTelegramCredentials(ctx)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			creds = &port.TelegramCredentials{
				EncryptedAPIID:   []byte{},
				EncryptedAPIHash: []byte{},
				PhoneMasked:      "",
			}
		} else {
			return fmt.Errorf("session storage: get credentials failed: %w", err)
		}
	}

	if creds.EncryptedAPIID == nil {
		creds.EncryptedAPIID = []byte{}
	}
	if creds.EncryptedAPIHash == nil {
		creds.EncryptedAPIHash = []byte{}
	}

	creds.EncryptedSession = encrypted
	creds.UpdatedAt = time.Now().UTC()

	if err := s.repo.SaveTelegramCredentials(ctx, creds); err != nil {
		return fmt.Errorf("session storage: save credentials failed: %w", err)
	}

	return nil
}
