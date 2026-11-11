package sqlite_test

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/matrocheetos/telegram-monitor/internal/adapter/secondary/sqlite"
	"github.com/matrocheetos/telegram-monitor/internal/domain"
)

func TestExplainQueryPlans(t *testing.T) {
	ctx := context.Background()
	dbA := newTestDB(t)
	dbB := newTestDB(t)

	_, _ = dbA.ExecContext(ctx, "INSERT INTO users (username, password_hash, role, ntfy_topic, created_at, updated_at) VALUES ('test_shared', 'h', 'user', 't', datetime('now'), datetime('now'))")
	var countB int
	_ = dbB.QueryRowContext(ctx, "SELECT COUNT(*) FROM users WHERE username = 'test_shared'").Scan(&countB)
	t.Logf("Is :memory: shared between dbA and dbB? count in B = %d", countB)

	db := newTestDB(t)

	// Populate with sample data to ensure query planner has statistics
	userRepo := sqliteNewUserRepo(db)
	channelRepo := sqliteNewChannelRepo(db)
	u := &domain.User{Username: "testuser", PasswordHash: "hash", Role: domain.RoleUser, NtfyTopic: "topic"}
	if err := userRepo.CreateUser(ctx, u); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	ch := &domain.TelegramChannel{ChatID: -1001, Title: "Chan", Username: "chan"}
	if err := channelRepo.UpsertChannel(ctx, ch); err != nil {
		t.Fatalf("UpsertChannel: %v", err)
	}

	now := time.Now().UTC()
	for i := 0; i < 200; i++ {
		_, err := db.ExecContext(ctx, `
			INSERT INTO matched_products (user_id, channel_id, message_text, created_at)
			VALUES (?, ?, ?, ?)
		`, u.ID, ch.ChatID, fmt.Sprintf("Promo item %d", i), now.Add(time.Duration(i)*time.Minute))
		if err != nil {
			t.Fatalf("insert matched_product: %v", err)
		}

		_, err = db.ExecContext(ctx, `
			INSERT INTO coupons (code, store_name, store_url, channel_id, message_text, created_at)
			VALUES (?, ?, ?, ?, ?, ?)
		`, fmt.Sprintf("CODE%d", i), "Store", "http://store.com", ch.ChatID, fmt.Sprintf("Coupon text %d", i), now.Add(time.Duration(i)*time.Minute))
		if err != nil {
			t.Fatalf("insert coupon: %v", err)
		}
	}

	explainQuery := func(q string, args ...any) []string {
		rows, err := db.QueryContext(ctx, "EXPLAIN QUERY PLAN "+q, args...)
		if err != nil {
			t.Fatalf("EXPLAIN QUERY PLAN failed for %s: %v", q, err)
		}
		defer func() { _ = rows.Close() }()

		var plans []string
		for rows.Next() {
			var id, parent, notused int
			var detail string
			if err := rows.Scan(&id, &parent, &notused, &detail); err != nil {
				t.Fatalf("scan plan: %v", err)
			}
			plans = append(plans, detail)
		}
		if err := rows.Err(); err != nil {
			t.Fatalf("iterate plan rows: %v", err)
		}
		return plans
	}

	t.Run("ListUserMatchedProducts_Count", func(t *testing.T) {
		q := `SELECT COUNT(*) FROM matched_products m WHERE m.user_id = ?`
		plans := explainQuery(q, u.ID)
		t.Logf("ListUserMatchedProducts Count Plan:\n%s", strings.Join(plans, "\n"))
		foundIndex := false
		for _, p := range plans {
			if strings.Contains(p, "idx_matched_products_user_created") {
				foundIndex = true
			}
		}
		if !foundIndex {
			t.Errorf("expected count query to use idx_matched_products_user_created, got: %v", plans)
		}
	})

	t.Run("ListUserMatchedProducts_Select", func(t *testing.T) {
		q := `
			SELECT
				m.id, m.user_id, m.interest_id, COALESCE(i.term, ''),
				m.channel_id, COALESCE(c.title, ''), COALESCE(c.username, ''),
				m.extracted_price, m.message_text, m.created_at
			FROM matched_products m
			LEFT JOIN user_interests i ON m.interest_id = i.id
			LEFT JOIN telegram_channels c ON m.channel_id = c.chat_id
			WHERE m.user_id = ?
			ORDER BY m.created_at DESC, m.id DESC
			LIMIT ? OFFSET ?
		`
		plans := explainQuery(q, u.ID, 50, 0)
		t.Logf("ListUserMatchedProducts Select Plan:\n%s", strings.Join(plans, "\n"))
		foundIndex := false
		for _, p := range plans {
			if strings.Contains(p, "idx_matched_products_user_created") {
				foundIndex = true
			}
		}
		if !foundIndex {
			t.Errorf("expected select query to use idx_matched_products_user_created, got: %v", plans)
		}
	})

	t.Run("PruneOlderThan_MatchedProducts", func(t *testing.T) {
		q := `
			SELECT id FROM matched_products
			WHERE created_at < ?
			ORDER BY created_at ASC, id ASC
			LIMIT ?
		`
		plans := explainQuery(q, now, 1000)
		t.Logf("PruneOlderThan MatchedProducts Plan:\n%s", strings.Join(plans, "\n"))
		foundIndex := false
		for _, p := range plans {
			if strings.Contains(p, "idx_matched_products_created_id") {
				foundIndex = true
			}
		}
		if !foundIndex {
			t.Errorf("expected PruneOlderThan to use idx_matched_products_created_id, got: %v", plans)
		}
	})

	t.Run("PruneExcessAbove_MatchedProducts", func(t *testing.T) {
		q := `
			SELECT id FROM matched_products
			ORDER BY created_at ASC, id ASC
			LIMIT ?
		`
		plans := explainQuery(q, 1000)
		t.Logf("PruneExcessAbove MatchedProducts Plan:\n%s", strings.Join(plans, "\n"))
		foundIndex := false
		for _, p := range plans {
			if strings.Contains(p, "idx_matched_products_created_id") {
				foundIndex = true
			}
		}
		if !foundIndex {
			t.Errorf("expected PruneExcessAbove to use idx_matched_products_created_id, got: %v", plans)
		}
	})

	t.Run("PruneOlderThan_Coupons", func(t *testing.T) {
		q := `
			SELECT id FROM coupons
			WHERE created_at < ?
			ORDER BY created_at ASC, id ASC
			LIMIT ?
		`
		plans := explainQuery(q, now, 1000)
		t.Logf("PruneOlderThan Coupons Plan:\n%s", strings.Join(plans, "\n"))
		foundIndex := false
		for _, p := range plans {
			if strings.Contains(p, "idx_coupons_created_id") {
				foundIndex = true
			}
		}
		if !foundIndex {
			t.Errorf("expected PruneOlderThan coupons to use idx_coupons_created_id, got: %v", plans)
		}
	})

	t.Run("PruneExcessAbove_Coupons", func(t *testing.T) {
		q := `
			SELECT id FROM coupons
			ORDER BY created_at ASC, id ASC
			LIMIT ?
		`
		plans := explainQuery(q, 1000)
		t.Logf("PruneExcessAbove Coupons Plan:\n%s", strings.Join(plans, "\n"))
		foundIndex := false
		for _, p := range plans {
			if strings.Contains(p, "idx_coupons_created_id") {
				foundIndex = true
			}
		}
		if !foundIndex {
			t.Errorf("expected PruneExcessAbove coupons to use idx_coupons_created_id, got: %v", plans)
		}
	})

	t.Run("HasRecentCoupon", func(t *testing.T) {
		q := `
			SELECT 1 FROM coupons
			WHERE UPPER(code) = UPPER(?) AND LOWER(store_name) = LOWER(?) AND created_at >= ?
			LIMIT 1
		`
		plans := explainQuery(q, "CODE1", "Store", now)
		t.Logf("HasRecentCoupon Plan:\n%s", strings.Join(plans, "\n"))
	})

	t.Run("Full_Delete_PruneOlderThan_MatchedProducts", func(t *testing.T) {
		q := `
			DELETE FROM matched_products
			WHERE id IN (
				SELECT id FROM matched_products
				WHERE created_at < ?
				ORDER BY created_at ASC, id ASC
				LIMIT ?
			)
		`
		plans := explainQuery(q, now, 1000)
		t.Logf("Full DELETE PruneOlderThan Plan:\n%s", strings.Join(plans, "\n"))
	})

	t.Run("Full_Delete_PruneExcessAbove_MatchedProducts", func(t *testing.T) {
		q := `
			DELETE FROM matched_products
			WHERE id IN (
				SELECT id FROM matched_products
				ORDER BY created_at ASC, id ASC
				LIMIT ?
			)
		`
		plans := explainQuery(q, 1000)
		t.Logf("Full DELETE PruneExcessAbove Plan:\n%s", strings.Join(plans, "\n"))
	})
}

// Helpers
func sqliteNewUserRepo(db *sql.DB) *sqlite.UserRepository {
	return sqlite.NewUserRepository(db)
}
func sqliteNewChannelRepo(db *sql.DB) *sqlite.ChannelRepository {
	return sqlite.NewChannelRepository(db)
}
