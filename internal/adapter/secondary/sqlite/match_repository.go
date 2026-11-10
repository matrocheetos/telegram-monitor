package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/matrocheetos/telegram-monitor/internal/domain"
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

func (r *MatchHistoryRepository) ListUserMatchedProducts(ctx context.Context, userID int64, searchQuery string, limit, offset int) ([]domain.UserMatchedProduct, int, error) {
	if limit <= 0 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}

	searchQuery = strings.TrimSpace(searchQuery)
	var countQuery string
	var selectQuery string
	var args []any
	var countArgs []any

	if searchQuery != "" {
		filter := "%" + searchQuery + "%"
		whereClause := `
			WHERE m.user_id = ?
			  AND (m.message_text LIKE ? OR COALESCE(i.term, '') LIKE ? OR COALESCE(c.title, '') LIKE ?)
		`
		countQuery = `
			SELECT COUNT(*)
			FROM matched_products m
			LEFT JOIN user_interests i ON m.interest_id = i.id
			LEFT JOIN telegram_channels c ON m.channel_id = c.chat_id
		` + whereClause

		selectQuery = `
			SELECT
				m.id,
				m.user_id,
				m.interest_id,
				COALESCE(i.term, ''),
				m.channel_id,
				COALESCE(c.title, ''),
				COALESCE(c.username, ''),
				m.extracted_price,
				m.message_text,
				m.created_at
			FROM matched_products m
			LEFT JOIN user_interests i ON m.interest_id = i.id
			LEFT JOIN telegram_channels c ON m.channel_id = c.chat_id
		` + whereClause + `
			ORDER BY m.created_at DESC, m.id DESC
			LIMIT ? OFFSET ?
		`
		countArgs = []any{userID, filter, filter, filter}
		args = []any{userID, filter, filter, filter, limit, offset}
	} else {
		whereClause := ` WHERE m.user_id = ? `
		countQuery = `SELECT COUNT(*) FROM matched_products m ` + whereClause
		selectQuery = `
			SELECT
				m.id,
				m.user_id,
				m.interest_id,
				COALESCE(i.term, ''),
				m.channel_id,
				COALESCE(c.title, ''),
				COALESCE(c.username, ''),
				m.extracted_price,
				m.message_text,
				m.created_at
			FROM matched_products m
			LEFT JOIN user_interests i ON m.interest_id = i.id
			LEFT JOIN telegram_channels c ON m.channel_id = c.chat_id
		` + whereClause + `
			ORDER BY m.created_at DESC, m.id DESC
			LIMIT ? OFFSET ?
		`
		countArgs = []any{userID}
		args = []any{userID, limit, offset}
	}

	var total int
	if err := r.db.QueryRowContext(ctx, countQuery, countArgs...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count user matched products: %w", err)
	}

	rows, err := r.db.QueryContext(ctx, selectQuery, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("query user matched products: %w", err)
	}
	defer func() { _ = rows.Close() }()

	items := make([]domain.UserMatchedProduct, 0, limit)
	for rows.Next() {
		var item domain.UserMatchedProduct
		var interestID sql.NullInt64
		var extractedPrice sql.NullFloat64
		var interestTerm, channelTitle, channelHandle string

		if err := rows.Scan(
			&item.ID,
			&item.UserID,
			&interestID,
			&interestTerm,
			&item.ChannelID,
			&channelTitle,
			&channelHandle,
			&extractedPrice,
			&item.MessageText,
			&item.CreatedAt,
		); err != nil {
			return nil, 0, fmt.Errorf("scan user matched product: %w", err)
		}

		if interestID.Valid {
			v := interestID.Int64
			item.InterestID = &v
		}
		if extractedPrice.Valid {
			v := extractedPrice.Float64
			item.ExtractedPrice = &v
		}

		if interestTerm == "" {
			interestTerm = "Termo Removido"
		}
		item.InterestTerm = interestTerm

		if channelTitle == "" {
			channelTitle = "Canal Desconhecido"
		}
		item.ChannelTitle = channelTitle
		item.ChannelHandle = channelHandle

		url := domain.ExtractFirstURL(item.MessageText)
		if url == "" && channelHandle != "" {
			url = "https://t.me/" + strings.TrimPrefix(channelHandle, "@")
		}
		item.OfferURL = url

		items = append(items, item)
	}

	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate user matched products rows: %w", err)
	}

	return items, total, nil
}

func (r *MatchHistoryRepository) CountTotal(ctx context.Context) (int64, error) {
	var count int64
	query := `SELECT COUNT(*) FROM matched_products`
	if err := r.db.QueryRowContext(ctx, query).Scan(&count); err != nil {
		return 0, fmt.Errorf("count total matched products: %w", err)
	}
	return count, nil
}

func (r *MatchHistoryRepository) PruneOlderThan(ctx context.Context, cutoff time.Time, batchSize int) (int64, error) {
	if batchSize <= 0 {
		batchSize = 1000
	}
	query := `
		DELETE FROM matched_products
		WHERE id IN (
			SELECT id FROM matched_products
			WHERE created_at < ?
			ORDER BY created_at ASC, id ASC
			LIMIT ?
		)
	`
	res, err := r.db.ExecContext(ctx, query, cutoff.UTC(), batchSize)
	if err != nil {
		return 0, fmt.Errorf("prune older matched products: %w", err)
	}
	rowsAffected, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("prune older matched products rows affected: %w", err)
	}
	return rowsAffected, nil
}

func (r *MatchHistoryRepository) PruneExcessAbove(ctx context.Context, maxKeep int64, batchSize int) (int64, error) {
	if batchSize <= 0 {
		batchSize = 1000
	}
	total, err := r.CountTotal(ctx)
	if err != nil {
		return 0, err
	}
	if total <= maxKeep {
		return 0, nil
	}

	excess := total - maxKeep
	limit := excess
	if limit > int64(batchSize) {
		limit = int64(batchSize)
	}

	query := `
		DELETE FROM matched_products
		WHERE id IN (
			SELECT id FROM matched_products
			ORDER BY created_at ASC, id ASC
			LIMIT ?
		)
	`
	res, err := r.db.ExecContext(ctx, query, limit)
	if err != nil {
		return 0, fmt.Errorf("prune excess matched products: %w", err)
	}
	rowsAffected, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("prune excess matched products rows affected: %w", err)
	}
	return rowsAffected, nil
}
