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

type CouponRepository struct {
	db *sql.DB
}

var _ port.CouponRepository = (*CouponRepository)(nil)

func NewCouponRepository(db *sql.DB) *CouponRepository {
	return &CouponRepository{db: db}
}

func (r *CouponRepository) RecordCoupon(ctx context.Context, code, storeName, storeURL string, channelID int64, text string) error {
	query := `
		INSERT INTO coupons (code, store_name, store_url, channel_id, message_text, created_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`
	_, err := r.db.ExecContext(ctx, query, code, storeName, storeURL, channelID, text, time.Now().UTC())
	if err != nil {
		return fmt.Errorf("insert coupon: %w", err)
	}
	return nil
}

func (r *CouponRepository) ListExtractedCoupons(ctx context.Context, searchQuery string, limit, offset int) ([]domain.ExtractedCoupon, int, error) {
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
			WHERE (cp.code LIKE ? OR cp.store_name LIKE ? OR cp.message_text LIKE ? OR COALESCE(c.title, '') LIKE ?)
		`
		countQuery = `
			SELECT COUNT(*)
			FROM coupons cp
			LEFT JOIN telegram_channels c ON cp.channel_id = c.chat_id
		` + whereClause

		selectQuery = `
			SELECT
				cp.id,
				cp.code,
				cp.store_name,
				cp.store_url,
				cp.channel_id,
				COALESCE(c.title, ''),
				COALESCE(c.username, ''),
				cp.message_text,
				cp.created_at
			FROM coupons cp
			LEFT JOIN telegram_channels c ON cp.channel_id = c.chat_id
		` + whereClause + `
			ORDER BY cp.created_at DESC, cp.id DESC
			LIMIT ? OFFSET ?
		`
		countArgs = []any{filter, filter, filter, filter}
		args = []any{filter, filter, filter, filter, limit, offset}
	} else {
		countQuery = `SELECT COUNT(*) FROM coupons`
		selectQuery = `
			SELECT
				cp.id,
				cp.code,
				cp.store_name,
				cp.store_url,
				cp.channel_id,
				COALESCE(c.title, ''),
				COALESCE(c.username, ''),
				cp.message_text,
				cp.created_at
			FROM coupons cp
			LEFT JOIN telegram_channels c ON cp.channel_id = c.chat_id
			ORDER BY cp.created_at DESC, cp.id DESC
			LIMIT ? OFFSET ?
		`
		args = []any{limit, offset}
	}

	var total int
	if err := r.db.QueryRowContext(ctx, countQuery, countArgs...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count extracted coupons: %w", err)
	}

	rows, err := r.db.QueryContext(ctx, selectQuery, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("query extracted coupons: %w", err)
	}
	defer func() { _ = rows.Close() }()

	items := make([]domain.ExtractedCoupon, 0, limit)
	for rows.Next() {
		var item domain.ExtractedCoupon
		var channelTitle, channelHandle string

		if err := rows.Scan(
			&item.ID,
			&item.Code,
			&item.StoreName,
			&item.StoreURL,
			&item.ChannelID,
			&channelTitle,
			&channelHandle,
			&item.MessageText,
			&item.CreatedAt,
		); err != nil {
			return nil, 0, fmt.Errorf("scan extracted coupon: %w", err)
		}

		if channelTitle == "" {
			channelTitle = "Canal Desconhecido"
		}
		item.ChannelTitle = channelTitle
		item.ChannelHandle = channelHandle

		items = append(items, item)
	}

	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate extracted coupons rows: %w", err)
	}

	return items, total, nil
}

func (r *CouponRepository) CountTotal(ctx context.Context) (int64, error) {
	var count int64
	query := `SELECT COUNT(*) FROM coupons`
	if err := r.db.QueryRowContext(ctx, query).Scan(&count); err != nil {
		return 0, fmt.Errorf("count total coupons: %w", err)
	}
	return count, nil
}

func (r *CouponRepository) PruneOlderThan(ctx context.Context, cutoff time.Time, batchSize int) (int64, error) {
	if batchSize <= 0 {
		batchSize = 1000
	}
	query := `
		DELETE FROM coupons
		WHERE id IN (
			SELECT id FROM coupons
			WHERE created_at < ?
			ORDER BY created_at ASC, id ASC
			LIMIT ?
		)
	`
	res, err := r.db.ExecContext(ctx, query, cutoff.UTC(), batchSize)
	if err != nil {
		return 0, fmt.Errorf("prune older coupons: %w", err)
	}
	rowsAffected, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("prune older coupons rows affected: %w", err)
	}
	return rowsAffected, nil
}

func (r *CouponRepository) PruneExcessAbove(ctx context.Context, maxKeep int64, batchSize int) (int64, error) {
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
		DELETE FROM coupons
		WHERE id IN (
			SELECT id FROM coupons
			ORDER BY created_at ASC, id ASC
			LIMIT ?
		)
	`
	res, err := r.db.ExecContext(ctx, query, limit)
	if err != nil {
		return 0, fmt.Errorf("prune excess coupons: %w", err)
	}
	rowsAffected, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("prune excess coupons rows affected: %w", err)
	}
	return rowsAffected, nil
}
