package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/matrocheetos/telegram-monitor/internal/domain"
	"github.com/matrocheetos/telegram-monitor/internal/port"
)

type UserRepository struct {
	db *sql.DB
}

var _ port.UserRepository = (*UserRepository)(nil)

func NewUserRepository(db *sql.DB) *UserRepository {
	return &UserRepository{db: db}
}

type userRow struct {
	ID           int64
	Username     string
	PasswordHash string
	Role         string
	NtfyTopic    string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

func (r *userRow) toDomain() *domain.User {
	return &domain.User{
		ID:           r.ID,
		Username:     r.Username,
		PasswordHash: r.PasswordHash,
		Role:         domain.Role(r.Role),
		NtfyTopic:    r.NtfyTopic,
		CreatedAt:    r.CreatedAt,
		UpdatedAt:    r.UpdatedAt,
	}
}

func (r *UserRepository) CountUsers(ctx context.Context) (int, error) {
	var count int
	query := "SELECT COUNT(*) FROM users"
	if err := r.db.QueryRowContext(ctx, query).Scan(&count); err != nil {
		return 0, fmt.Errorf("sqlite: count users failed: %w", err)
	}
	return count, nil
}

func (r *UserRepository) CreateUser(ctx context.Context, user *domain.User) error {
	query := `
		INSERT INTO users (username, password_hash, role, ntfy_topic, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`
	now := time.Now().UTC()
	user.CreatedAt = now
	user.UpdatedAt = now

	res, err := r.db.ExecContext(ctx, query,
		user.Username,
		user.PasswordHash,
		string(user.Role),
		user.NtfyTopic,
		user.CreatedAt,
		user.UpdatedAt,
	)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed") {
			return domain.ErrDuplicateEntry
		}
		return fmt.Errorf("sqlite: create user failed: %w", err)
	}

	id, err := res.LastInsertId()
	if err != nil {
		return fmt.Errorf("sqlite: failed to get last insert id: %w", err)
	}
	user.ID = id

	return nil
}

func (r *UserRepository) CreateInitialAdmin(ctx context.Context, user *domain.User) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("sqlite: begin initial admin tx failed: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var count int
	if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM users").Scan(&count); err != nil {
		return fmt.Errorf("sqlite: count users in tx failed: %w", err)
	}
	if count > 0 {
		return domain.ErrSetupAlreadyCompleted
	}

	now := time.Now().UTC()
	user.CreatedAt = now
	user.UpdatedAt = now

	query := `
		INSERT INTO users (username, password_hash, role, ntfy_topic, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`
	res, err := tx.ExecContext(ctx, query,
		user.Username,
		user.PasswordHash,
		string(user.Role),
		user.NtfyTopic,
		user.CreatedAt,
		user.UpdatedAt,
	)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed") {
			return domain.ErrDuplicateEntry
		}
		return fmt.Errorf("sqlite: insert initial admin failed: %w", err)
	}

	id, err := res.LastInsertId()
	if err != nil {
		return fmt.Errorf("sqlite: failed to get initial admin insert id: %w", err)
	}
	user.ID = id

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("sqlite: commit initial admin tx failed: %w", err)
	}

	return nil
}

func (r *UserRepository) GetUserByID(ctx context.Context, id int64) (*domain.User, error) {
	query := `
		SELECT id, username, password_hash, role, ntfy_topic, created_at, updated_at
		FROM users
		WHERE id = ?
	`
	var row userRow
	err := r.db.QueryRowContext(ctx, query, id).Scan(
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
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("sqlite: get user by id failed: %w", err)
	}
	return row.toDomain(), nil
}

func (r *UserRepository) GetUserByUsername(ctx context.Context, username string) (*domain.User, error) {
	query := `
		SELECT id, username, password_hash, role, ntfy_topic, created_at, updated_at
		FROM users
		WHERE username = ?
	`
	var row userRow
	err := r.db.QueryRowContext(ctx, query, username).Scan(
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
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("sqlite: get user by username failed: %w", err)
	}
	return row.toDomain(), nil
}

func (r *UserRepository) ListUsers(ctx context.Context) ([]*domain.User, error) {
	query := `
		SELECT id, username, password_hash, role, ntfy_topic, created_at, updated_at
		FROM users
		ORDER BY id ASC
	`
	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("sqlite: list users failed: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var users []*domain.User
	for rows.Next() {
		var row userRow
		if err := rows.Scan(
			&row.ID,
			&row.Username,
			&row.PasswordHash,
			&row.Role,
			&row.NtfyTopic,
			&row.CreatedAt,
			&row.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("sqlite: scan user row failed: %w", err)
		}
		users = append(users, row.toDomain())
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sqlite: iterate user rows failed: %w", err)
	}

	return users, nil
}

func (r *UserRepository) UpdatePassword(ctx context.Context, id int64, passwordHash string) error {
	query := `
		UPDATE users
		SET password_hash = ?, updated_at = ?
		WHERE id = ?
	`
	res, err := r.db.ExecContext(ctx, query, passwordHash, time.Now().UTC(), id)
	if err != nil {
		return fmt.Errorf("sqlite: update password failed: %w", err)
	}

	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("sqlite: check rows affected failed: %w", err)
	}
	if rows == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (r *UserRepository) DeleteUser(ctx context.Context, id int64) error {
	query := "DELETE FROM users WHERE id = ?"
	res, err := r.db.ExecContext(ctx, query, id)
	if err != nil {
		return fmt.Errorf("sqlite: delete user failed: %w", err)
	}

	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("sqlite: check rows affected failed: %w", err)
	}
	if rows == 0 {
		return domain.ErrNotFound
	}
	return nil
}
