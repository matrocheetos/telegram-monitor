package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/matrocheetos/telegram-monitor/internal/domain"
	"github.com/matrocheetos/telegram-monitor/internal/port"
)

type CredentialRepository struct {
	db *sql.DB
}

var _ port.CredentialRepository = (*CredentialRepository)(nil)

func NewCredentialRepository(db *sql.DB) *CredentialRepository {
	return &CredentialRepository{db: db}
}

func (r *CredentialRepository) SaveTelegramCredentials(ctx context.Context, creds *port.TelegramCredentials) error {
	query := `
		INSERT INTO telegram_credentials (id, encrypted_api_id, encrypted_api_hash, encrypted_session, phone_masked, updated_at)
		VALUES (1, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			encrypted_api_id = excluded.encrypted_api_id,
			encrypted_api_hash = excluded.encrypted_api_hash,
			encrypted_session = excluded.encrypted_session,
			phone_masked = excluded.phone_masked,
			updated_at = excluded.updated_at
	`
	creds.ID = 1
	creds.UpdatedAt = time.Now().UTC()

	if creds.EncryptedAPIID == nil {
		creds.EncryptedAPIID = []byte{}
	}
	if creds.EncryptedAPIHash == nil {
		creds.EncryptedAPIHash = []byte{}
	}
	if creds.EncryptedSession == nil {
		creds.EncryptedSession = []byte{}
	}

	_, err := r.db.ExecContext(ctx, query,
		creds.EncryptedAPIID,
		creds.EncryptedAPIHash,
		creds.EncryptedSession,
		creds.PhoneMasked,
		creds.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("sqlite: save telegram credentials failed: %w", err)
	}
	return nil
}

func (r *CredentialRepository) GetTelegramCredentials(ctx context.Context) (*port.TelegramCredentials, error) {
	query := `
		SELECT id, encrypted_api_id, encrypted_api_hash, encrypted_session, phone_masked, updated_at
		FROM telegram_credentials
		WHERE id = 1
	`
	var creds port.TelegramCredentials
	err := r.db.QueryRowContext(ctx, query).Scan(
		&creds.ID,
		&creds.EncryptedAPIID,
		&creds.EncryptedAPIHash,
		&creds.EncryptedSession,
		&creds.PhoneMasked,
		&creds.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("sqlite: get telegram credentials failed: %w", err)
	}
	return &creds, nil
}

func (r *CredentialRepository) DeleteTelegramCredentials(ctx context.Context) error {
	query := "DELETE FROM telegram_credentials WHERE id = 1"
	if _, err := r.db.ExecContext(ctx, query); err != nil {
		return fmt.Errorf("sqlite: delete telegram credentials failed: %w", err)
	}
	return nil
}
