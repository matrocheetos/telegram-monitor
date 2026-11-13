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

type DebugMessageRepository struct {
	db *sql.DB
}

var _ port.DebugMessageRepository = (*DebugMessageRepository)(nil)

func NewDebugMessageRepository(db *sql.DB) *DebugMessageRepository {
	return &DebugMessageRepository{db: db}
}

func marshalJSONOrEmptyArray[T any](items []T) string {
	if len(items) == 0 {
		return "[]"
	}
	data, err := json.Marshal(items)
	if err != nil {
		return "[]"
	}
	return string(data)
}

func (r *DebugMessageRepository) RecordAnalyzedMessage(ctx context.Context, rec *domain.AnalyzedMessageDebugRecord) error {
	if rec == nil {
		return nil
	}

	tokensJSON := marshalJSONOrEmptyArray(rec.Tokens)
	entitiesJSON := marshalJSONOrEmptyArray(rec.TGEntities)
	buttonsJSON := marshalJSONOrEmptyArray(rec.TGInlineButtons)
	plainURLsJSON := marshalJSONOrEmptyArray(rec.PlainURLs)
	pricesJSON := marshalJSONOrEmptyArray(rec.AllPrices)
	couponsJSON := marshalJSONOrEmptyArray(rec.ExtractedCoupons)
	interestsJSON := marshalJSONOrEmptyArray(rec.MatchedInterests)

	messageDate := rec.MessageDate
	if messageDate.IsZero() {
		messageDate = time.Now().UTC()
	}
	analyzedAt := rec.AnalyzedAt
	if analyzedAt.IsZero() {
		analyzedAt = time.Now().UTC()
	}

	query := `
		INSERT INTO debug_analyzed_messages (
			telegram_message_id,
			channel_id,
			channel_title,
			channel_username,
			monitor_products,
			monitor_coupons,
			raw_text,
			normalized_text,
			tokens_json,
			has_media,
			media_type,
			tg_entities_json,
			tg_inline_buttons_json,
			tg_webpage_url,
			plain_urls_json,
			first_url,
			all_prices_json,
			prices_found_count,
			extracted_lowest_price,
			has_coupon_indicator,
			identified_store,
			identified_store_url,
			extracted_coupons_json,
			extracted_coupons_count,
			deduplicated_coupons_count,
			matched_interests_json,
			matched_products_count,
			evaluation_outcome,
			message_date,
			analyzed_at
		) VALUES (
			?, ?, ?, ?, ?, ?, ?, ?, ?, ?,
			?, ?, ?, ?, ?, ?, ?, ?, ?, ?,
			?, ?, ?, ?, ?, ?, ?, ?, ?, ?
		)
	`

	res, err := r.db.ExecContext(ctx, query,
		rec.TelegramMessageID,
		rec.ChannelID,
		rec.ChannelTitle,
		rec.ChannelUsername,
		rec.MonitorProducts,
		rec.MonitorCoupons,
		rec.RawText,
		rec.NormalizedText,
		tokensJSON,
		rec.HasMedia,
		rec.MediaType,
		entitiesJSON,
		buttonsJSON,
		rec.TGWebPageURL,
		plainURLsJSON,
		rec.FirstURL,
		pricesJSON,
		rec.PricesFoundCount,
		rec.ExtractedLowestPrice,
		rec.HasCouponIndicator,
		rec.IdentifiedStore,
		rec.IdentifiedStoreURL,
		couponsJSON,
		rec.ExtractedCouponsCount,
		rec.DeduplicatedCouponsCount,
		interestsJSON,
		rec.MatchedProductsCount,
		string(rec.EvaluationOutcome),
		messageDate.UTC(),
		analyzedAt.UTC(),
	)
	if err != nil {
		return fmt.Errorf("insert debug analyzed message: %w", err)
	}

	if id, err := res.LastInsertId(); err == nil {
		rec.ID = id
	}

	return nil
}

func (r *DebugMessageRepository) CountAnalyzedMessages(ctx context.Context) (int64, error) {
	var count int64
	query := `SELECT COUNT(*) FROM debug_analyzed_messages`
	if err := r.db.QueryRowContext(ctx, query).Scan(&count); err != nil {
		return 0, fmt.Errorf("count debug analyzed messages: %w", err)
	}
	return count, nil
}

func (r *DebugMessageRepository) PruneOldestDebugMessages(ctx context.Context, maxKeep int64, batchSize int) (int64, error) {
	if batchSize <= 0 {
		batchSize = 1000
	}
	total, err := r.CountAnalyzedMessages(ctx)
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
		DELETE FROM debug_analyzed_messages
		WHERE id IN (
			SELECT id FROM debug_analyzed_messages
			ORDER BY analyzed_at ASC, id ASC
			LIMIT ?
		)
	`
	res, err := r.db.ExecContext(ctx, query, limit)
	if err != nil {
		return 0, fmt.Errorf("prune oldest debug messages: %w", err)
	}
	rowsAffected, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("prune oldest debug messages rows affected: %w", err)
	}
	return rowsAffected, nil
}
