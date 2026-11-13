package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/matrocheetos/telegram-monitor/internal/domain"
	"github.com/matrocheetos/telegram-monitor/internal/port"
)

type InterestRepository struct {
	db *sql.DB
}

var _ port.InterestRepository = (*InterestRepository)(nil)

func NewInterestRepository(db *sql.DB) *InterestRepository {
	return &InterestRepository{db: db}
}

type interestRow struct {
	ID             int64
	UserID         int64
	Term           string
	PositiveTokens string
	NegativeTokens string
	MaxPrice       *float64
	Active         bool
	CreatedAt      time.Time
}

func (r *interestRow) toDomain() (*domain.UserInterest, error) {
	var posTokens []string
	if err := json.Unmarshal([]byte(r.PositiveTokens), &posTokens); err != nil {
		return nil, fmt.Errorf("unmarshal positive tokens: %w", err)
	}

	var negTokens []string
	if err := json.Unmarshal([]byte(r.NegativeTokens), &negTokens); err != nil {
		return nil, fmt.Errorf("unmarshal negative tokens: %w", err)
	}

	return &domain.UserInterest{
		ID:             r.ID,
		UserID:         r.UserID,
		RawTerm:        r.Term,
		PositiveTokens: posTokens,
		NegativeTokens: negTokens,
		MaxPrice:       r.MaxPrice,
		Active:         r.Active,
		CreatedAt:      r.CreatedAt,
	}, nil
}

func (r *InterestRepository) CreateInterest(ctx context.Context, interest *domain.UserInterest) error {
	posJSON, err := json.Marshal(interest.PositiveTokens)
	if err != nil {
		return fmt.Errorf("sqlite: marshal positive tokens: %w", err)
	}

	negJSON, err := json.Marshal(interest.NegativeTokens)
	if err != nil {
		return fmt.Errorf("sqlite: marshal negative tokens: %w", err)
	}

	query := `
		INSERT INTO user_interests (user_id, term, positive_tokens, negative_tokens, max_price, active, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`
	interest.CreatedAt = time.Now().UTC()
	res, err := r.db.ExecContext(ctx, query,
		interest.UserID,
		interest.RawTerm,
		string(posJSON),
		string(negJSON),
		interest.MaxPrice,
		interest.Active,
		interest.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("sqlite: create interest failed: %w", err)
	}

	id, err := res.LastInsertId()
	if err != nil {
		return fmt.Errorf("sqlite: failed to get last insert id: %w", err)
	}
	interest.ID = id

	return nil
}

func (r *InterestRepository) ListInterestsByUserID(ctx context.Context, userID int64) ([]*domain.UserInterest, error) {
	query := `
		SELECT id, user_id, term, positive_tokens, negative_tokens, max_price, active, created_at
		FROM user_interests
		WHERE user_id = ?
		ORDER BY id DESC
	`
	rows, err := r.db.QueryContext(ctx, query, userID)
	if err != nil {
		return nil, fmt.Errorf("sqlite: list interests by user failed: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var interests []*domain.UserInterest
	for rows.Next() {
		var row interestRow
		if err := rows.Scan(
			&row.ID,
			&row.UserID,
			&row.Term,
			&row.PositiveTokens,
			&row.NegativeTokens,
			&row.MaxPrice,
			&row.Active,
			&row.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("sqlite: scan interest row failed: %w", err)
		}
		item, err := row.toDomain()
		if err != nil {
			return nil, fmt.Errorf("sqlite: parse interest row: %w", err)
		}
		interests = append(interests, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sqlite: iterate interest rows failed: %w", err)
	}

	return interests, nil
}

func (r *InterestRepository) ListAllActiveInterests(ctx context.Context) ([]*domain.UserInterest, error) {
	query := `
		SELECT id, user_id, term, positive_tokens, negative_tokens, max_price, active, created_at
		FROM user_interests
		WHERE active = 1
		ORDER BY id ASC
	`
	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("sqlite: list active interests failed: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var interests []*domain.UserInterest
	for rows.Next() {
		var row interestRow
		if err := rows.Scan(
			&row.ID,
			&row.UserID,
			&row.Term,
			&row.PositiveTokens,
			&row.NegativeTokens,
			&row.MaxPrice,
			&row.Active,
			&row.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("sqlite: scan interest row failed: %w", err)
		}
		item, err := row.toDomain()
		if err != nil {
			return nil, fmt.Errorf("sqlite: parse interest row: %w", err)
		}
		interests = append(interests, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sqlite: iterate interest rows failed: %w", err)
	}

	return interests, nil
}

func (r *InterestRepository) CountActiveInterestsByUser(ctx context.Context) (map[int64]int, error) {
	query := `
		SELECT user_id, COUNT(*)
		FROM user_interests
		WHERE active = 1
		GROUP BY user_id
	`
	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("sqlite: count active interests by user failed: %w", err)
	}
	defer func() { _ = rows.Close() }()

	counts := make(map[int64]int)
	for rows.Next() {
		var userID int64
		var count int
		if err := rows.Scan(&userID, &count); err != nil {
			return nil, fmt.Errorf("sqlite: scan active interest count failed: %w", err)
		}
		counts[userID] = count
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sqlite: iterate active interest count rows failed: %w", err)
	}

	return counts, nil
}

func (r *InterestRepository) DeleteInterest(ctx context.Context, userID, id int64) error {
	query := "DELETE FROM user_interests WHERE id = ? AND user_id = ?"
	res, err := r.db.ExecContext(ctx, query, id, userID)
	if err != nil {
		return fmt.Errorf("sqlite: delete interest failed: %w", err)
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
