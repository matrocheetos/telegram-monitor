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

type SessionRepository struct {
	db *sql.DB
}

var _ port.SessionRepository = (*SessionRepository)(nil)

func NewSessionRepository(db *sql.DB) *SessionRepository {
	return &SessionRepository{db: db}
}

func (r *SessionRepository) CreateSession(ctx context.Context, tokenHash string, userID int64, expiresAt time.Time) error {
	query := `
		INSERT INTO sessions (token_hash, user_id, expires_at, created_at)
		VALUES (?, ?, ?, ?)
	`
	_, err := r.db.ExecContext(ctx, query, tokenHash, userID, expiresAt.UTC(), time.Now().UTC())
	if err != nil {
		return fmt.Errorf("sqlite: create session failed: %w", err)
	}
	return nil
}

func (r *SessionRepository) GetSessionUser(ctx context.Context, tokenHash string) (*domain.User, error) {
	query := `
		SELECT u.id, u.username, u.password_hash, u.role, u.ntfy_topic, u.created_at, u.updated_at
		FROM sessions s
		JOIN users u ON s.user_id = u.id
		WHERE s.token_hash = ? AND s.expires_at > ?
	`
	var row userRow
	err := r.db.QueryRowContext(ctx, query, tokenHash, time.Now().UTC()).Scan(
		&row.ID,
		&row.Username,
		&row.PasswordHash,
		&row.Role,
		&row.NtfyTopic,
		&row.CreatedAt,
		&row.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrUnauthorized
		}
		return nil, fmt.Errorf("sqlite: get session user failed: %w", err)
	}

	return row.toDomain(), nil
}

func (r *SessionRepository) DeleteSession(ctx context.Context, tokenHash string) error {
	query := "DELETE FROM sessions WHERE token_hash = ?"
	if _, err := r.db.ExecContext(ctx, query, tokenHash); err != nil {
		return fmt.Errorf("sqlite: delete session failed: %w", err)
	}
	return nil
}

func (r *SessionRepository) DeleteUserSessions(ctx context.Context, userID int64) error {
	query := "DELETE FROM sessions WHERE user_id = ?"
	if _, err := r.db.ExecContext(ctx, query, userID); err != nil {
		return fmt.Errorf("sqlite: delete user sessions failed: %w", err)
	}
	return nil
}

func (r *SessionRepository) DeleteExpiredSessions(ctx context.Context) error {
	query := "DELETE FROM sessions WHERE expires_at <= ?"
	if _, err := r.db.ExecContext(ctx, query, time.Now().UTC()); err != nil {
		return fmt.Errorf("sqlite: delete expired sessions failed: %w", err)
	}
	return nil
}
