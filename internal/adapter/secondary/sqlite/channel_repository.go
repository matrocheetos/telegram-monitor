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

type ChannelRepository struct {
	db *sql.DB
}

var _ port.ChannelRepository = (*ChannelRepository)(nil)

func NewChannelRepository(db *sql.DB) *ChannelRepository {
	return &ChannelRepository{db: db}
}

type channelRow struct {
	ChatID          int64
	AccessHash      int64
	Username        string
	Title           string
	MonitorProducts bool
	MonitorCoupons  bool
	UpdatedAt       time.Time
}

func (r *channelRow) toDomain() *domain.TelegramChannel {
	return &domain.TelegramChannel{
		ChatID:          r.ChatID,
		AccessHash:      r.AccessHash,
		Username:        r.Username,
		Title:           r.Title,
		MonitorProducts: r.MonitorProducts,
		MonitorCoupons:  r.MonitorCoupons,
		UpdatedAt:       r.UpdatedAt,
	}
}

func (r *ChannelRepository) UpsertChannel(ctx context.Context, ch *domain.TelegramChannel) error {
	query := `
		INSERT INTO telegram_channels (chat_id, access_hash, username, title, monitor_products, monitor_coupons, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(chat_id) DO UPDATE SET
			access_hash = excluded.access_hash,
			username = excluded.username,
			title = excluded.title,
			updated_at = excluded.updated_at
	`
	ch.UpdatedAt = time.Now().UTC()
	_, err := r.db.ExecContext(ctx, query,
		ch.ChatID,
		ch.AccessHash,
		ch.Username,
		ch.Title,
		ch.MonitorProducts,
		ch.MonitorCoupons,
		ch.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("sqlite: upsert channel failed: %w", err)
	}
	return nil
}

func (r *ChannelRepository) UpsertChannelsBatch(ctx context.Context, channels []*domain.TelegramChannel) error {
	if len(channels) == 0 {
		return nil
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("sqlite: begin upsert channels batch tx failed: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	query := `
		INSERT INTO telegram_channels (chat_id, access_hash, username, title, monitor_products, monitor_coupons, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(chat_id) DO UPDATE SET
			access_hash = excluded.access_hash,
			username = excluded.username,
			title = excluded.title,
			updated_at = excluded.updated_at
	`
	stmt, err := tx.PrepareContext(ctx, query)
	if err != nil {
		return fmt.Errorf("sqlite: prepare upsert channels batch failed: %w", err)
	}
	defer func() { _ = stmt.Close() }()

	now := time.Now().UTC()
	for _, ch := range channels {
		if ch == nil {
			continue
		}
		ch.UpdatedAt = now
		if _, err := stmt.ExecContext(ctx,
			ch.ChatID,
			ch.AccessHash,
			ch.Username,
			ch.Title,
			ch.MonitorProducts,
			ch.MonitorCoupons,
			ch.UpdatedAt,
		); err != nil {
			return fmt.Errorf("sqlite: upsert channel %d in batch failed: %w", ch.ChatID, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("sqlite: commit upsert channels batch tx failed: %w", err)
	}
	return nil
}

func (r *ChannelRepository) GetChannel(ctx context.Context, chatID int64) (*domain.TelegramChannel, error) {
	query := `
		SELECT chat_id, access_hash, username, title, monitor_products, monitor_coupons, updated_at
		FROM telegram_channels
		WHERE chat_id = ?
	`
	var row channelRow
	err := r.db.QueryRowContext(ctx, query, chatID).Scan(
		&row.ChatID,
		&row.AccessHash,
		&row.Username,
		&row.Title,
		&row.MonitorProducts,
		&row.MonitorCoupons,
		&row.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("sqlite: get channel failed: %w", err)
	}
	return row.toDomain(), nil
}

func (r *ChannelRepository) ListChannels(ctx context.Context) ([]*domain.TelegramChannel, error) {
	query := `
		SELECT chat_id, access_hash, username, title, monitor_products, monitor_coupons, updated_at
		FROM telegram_channels
		ORDER BY title COLLATE NOCASE ASC
	`
	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("sqlite: list channels failed: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var channels []*domain.TelegramChannel
	for rows.Next() {
		var row channelRow
		if err := rows.Scan(
			&row.ChatID,
			&row.AccessHash,
			&row.Username,
			&row.Title,
			&row.MonitorProducts,
			&row.MonitorCoupons,
			&row.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("sqlite: scan channel row failed: %w", err)
		}
		channels = append(channels, row.toDomain())
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sqlite: iterate channel rows failed: %w", err)
	}

	return channels, nil
}

func (r *ChannelRepository) UpdateChannelFlags(
	ctx context.Context,
	chatID int64,
	monitorProducts bool,
	monitorCoupons bool,
) error {
	query := `
		UPDATE telegram_channels
		SET monitor_products = ?, monitor_coupons = ?, updated_at = ?
		WHERE chat_id = ?
	`
	res, err := r.db.ExecContext(ctx, query, monitorProducts, monitorCoupons, time.Now().UTC(), chatID)
	if err != nil {
		return fmt.Errorf("sqlite: update channel flags failed: %w", err)
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

func (r *ChannelRepository) UpdateChannelsFlagsBatch(ctx context.Context, updates []domain.ChannelFlagUpdate) error {
	if len(updates) == 0 {
		return nil
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("sqlite: begin update channels flags batch tx failed: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	query := `
		UPDATE telegram_channels
		SET monitor_products = ?, monitor_coupons = ?, updated_at = ?
		WHERE chat_id = ?
	`
	stmt, err := tx.PrepareContext(ctx, query)
	if err != nil {
		return fmt.Errorf("sqlite: prepare update channels flags batch failed: %w", err)
	}
	defer func() { _ = stmt.Close() }()

	now := time.Now().UTC()
	for _, u := range updates {
		res, err := stmt.ExecContext(ctx, u.MonitorProducts, u.MonitorCoupons, now, u.ChatID)
		if err != nil {
			return fmt.Errorf("sqlite: update channel %d flags in batch failed: %w", u.ChatID, err)
		}
		rows, err := res.RowsAffected()
		if err != nil {
			return fmt.Errorf("sqlite: check rows affected for channel %d failed: %w", u.ChatID, err)
		}
		if rows == 0 {
			return domain.ErrNotFound
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("sqlite: commit update channels flags batch tx failed: %w", err)
	}
	return nil
}
