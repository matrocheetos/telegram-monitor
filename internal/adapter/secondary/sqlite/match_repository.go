package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/matrocheetos/telegram-monitor/internal/port"
)

type MatchHistoryRepository struct {
	db *sql.DB
}

var _ port.MatchHistoryRepository = (*MatchHistoryRepository)(nil)

func NewMatchHistoryRepository(db *sql.DB) *MatchHistoryRepository {
	return &MatchHistoryRepository{db: db}
}

func (r *MatchHistoryRepository) RecordProductMatch(ctx context.Context, userID int64, interestID *int64, channelID int64, price *float64, text string) error {
	query := `
		INSERT INTO matched_products (user_id, interest_id, channel_id, extracted_price, message_text, created_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`
	_, err := r.db.ExecContext(ctx, query, userID, interestID, channelID, price, text, time.Now().UTC())
	if err != nil {
		return fmt.Errorf("insert matched product: %w", err)
	}
	return nil
}
