package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

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
