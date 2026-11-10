package sqlite_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/matrocheetos/telegram-monitor/internal/adapter/secondary/sqlite"
	"github.com/matrocheetos/telegram-monitor/internal/domain"
)

func TestPruneOlderThan_EdgeCases(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	cases := []struct {
		name       string
		oldCount   int
		batchSize  int
		wantLoops  int
		wantPruned int64
	}{
		{
			name:       "zero_old_records",
			oldCount:   0,
			batchSize:  1000,
			wantLoops:  1,
			wantPruned: 0,
		},
		{
			name:       "exactly_1000_old_records",
			oldCount:   1000,
			batchSize:  1000,
			wantLoops:  2, // loop 1 deletes 1000, loop 2 deletes 0 and terminates
			wantPruned: 1000,
		},
		{
			name:       "exactly_2500_old_records",
			oldCount:   2500,
			batchSize:  1000,
			wantLoops:  3, // loop 1: 1000, loop 2: 1000, loop 3: 500 (<1000, terminates)
			wantPruned: 2500,
		},
		{
			name:       "exactly_3000_old_records",
			oldCount:   3000,
			batchSize:  1000,
			wantLoops:  4, // loop 1: 1000, loop 2: 1000, loop 3: 1000, loop 4: 0 (terminates)
			wantPruned: 3000,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			db := newTestDB(t)
			userRepo := sqlite.NewUserRepository(db)
			matchRepo := sqlite.NewMatchHistoryRepository(db)
			couponRepo := sqlite.NewCouponRepository(db)

			u := &domain.User{
				Username:     fmt.Sprintf("user_%s", tc.name),
				PasswordHash: "pass",
				Role:         domain.RoleUser,
				NtfyTopic:    fmt.Sprintf("topic_%s", tc.name),
			}
			if err := userRepo.CreateUser(ctx, u); err != nil {
				t.Fatalf("CreateUser failed: %v", err)
			}

			// Insert old records (>90 days old)
			if tc.oldCount > 0 {
				tx, err := db.BeginTx(ctx, nil)
				if err != nil {
					t.Fatalf("BeginTx failed: %v", err)
				}
				matchStmt, err := tx.PrepareContext(ctx, `
					INSERT INTO matched_products (user_id, channel_id, message_text, created_at)
					VALUES (?, ?, ?, ?)
				`)
				if err != nil {
					t.Fatalf("Prepare matchStmt failed: %v", err)
				}
				defer func() { _ = matchStmt.Close() }()

				couponStmt, err := tx.PrepareContext(ctx, `
					INSERT INTO coupons (code, store_name, store_url, channel_id, message_text, created_at)
					VALUES (?, ?, ?, ?, ?, ?)
				`)
				if err != nil {
					t.Fatalf("Prepare couponStmt failed: %v", err)
				}
				defer func() { _ = couponStmt.Close() }()

				oldBase := time.Now().UTC().AddDate(0, 0, -100)
				for i := 1; i <= tc.oldCount; i++ {
					createdAt := oldBase.Add(time.Duration(i) * time.Second)
					if _, err := matchStmt.ExecContext(ctx, u.ID, -100, fmt.Sprintf("old match %d", i), createdAt); err != nil {
						t.Fatalf("insert match failed: %v", err)
					}
					if _, err := couponStmt.ExecContext(ctx, fmt.Sprintf("CUP%d", i), "Store", "https://s.com", -100, "txt", createdAt); err != nil {
						t.Fatalf("insert coupon failed: %v", err)
					}
				}
				if err := tx.Commit(); err != nil {
					t.Fatalf("Commit failed: %v", err)
				}
			}

			// Insert 5 fresh records (<90 days old) to verify they are never touched
			freshDate := time.Now().UTC().AddDate(0, 0, -5)
			for i := 1; i <= 5; i++ {
				_, err := db.ExecContext(ctx, `
					INSERT INTO matched_products (user_id, channel_id, message_text, created_at)
					VALUES (?, ?, ?, ?)
				`, u.ID, -100, fmt.Sprintf("fresh match %d", i), freshDate.Add(time.Duration(i)*time.Minute))
				if err != nil {
					t.Fatalf("insert fresh match failed: %v", err)
				}
				_, err = db.ExecContext(ctx, `
					INSERT INTO coupons (code, store_name, store_url, channel_id, message_text, created_at)
					VALUES (?, ?, ?, ?, ?, ?)
				`, fmt.Sprintf("FRESH%d", i), "Store", "https://s.com", -100, "txt", freshDate.Add(time.Duration(i)*time.Minute))
				if err != nil {
					t.Fatalf("insert fresh coupon failed: %v", err)
				}
			}

			// Cutoff is 90 days ago
			cutoff := time.Now().UTC().AddDate(0, 0, -90)

			// Test matched_products loop
			var matchDeletedTotal int64
			var matchLoops int
			for {
				matchLoops++
				deleted, err := matchRepo.PruneOlderThan(ctx, cutoff, tc.batchSize)
				if err != nil {
					t.Fatalf("PruneOlderThan matchRepo failed on loop %d: %v", matchLoops, err)
				}
				matchDeletedTotal += deleted
				if deleted < int64(tc.batchSize) {
					break
				}
			}

			if matchDeletedTotal != tc.wantPruned {
				t.Fatalf("matched_products: expected %d total pruned, got %d", tc.wantPruned, matchDeletedTotal)
			}
			if matchLoops != tc.wantLoops {
				t.Fatalf("matched_products: expected %d loops, got %d", tc.wantLoops, matchLoops)
			}

			remainingMatches, err := matchRepo.CountTotal(ctx)
			if err != nil {
				t.Fatalf("CountTotal matches failed: %v", err)
			}
			if remainingMatches != 5 {
				t.Fatalf("matched_products: expected 5 fresh records remaining, got %d", remainingMatches)
			}

			// Test coupons loop
			var couponDeletedTotal int64
			var couponLoops int
			for {
				couponLoops++
				deleted, err := couponRepo.PruneOlderThan(ctx, cutoff, tc.batchSize)
				if err != nil {
					t.Fatalf("PruneOlderThan couponRepo failed on loop %d: %v", couponLoops, err)
				}
				couponDeletedTotal += deleted
				if deleted < int64(tc.batchSize) {
					break
				}
			}

			if couponDeletedTotal != tc.wantPruned {
				t.Fatalf("coupons: expected %d total pruned, got %d", tc.wantPruned, couponDeletedTotal)
			}
			if couponLoops != tc.wantLoops {
				t.Fatalf("coupons: expected %d loops, got %d", tc.wantLoops, couponLoops)
			}

			remainingCoupons, err := couponRepo.CountTotal(ctx)
			if err != nil {
				t.Fatalf("CountTotal coupons failed: %v", err)
			}
			if remainingCoupons != 5 {
				t.Fatalf("coupons: expected 5 fresh records remaining, got %d", remainingCoupons)
			}
		})
	}
}

func TestPruneExcessAbove_EdgeCases(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("boundary_99999_and_100000", func(t *testing.T) {
		t.Parallel()
		db := newTestDB(t)
		userRepo := sqlite.NewUserRepository(db)
		u := &domain.User{Username: "boundary_user", PasswordHash: "p", Role: domain.RoleUser, NtfyTopic: "t"}
		if err := userRepo.CreateUser(ctx, u); err != nil {
			t.Fatalf("CreateUser failed: %v", err)
		}

		matchRepo := sqlite.NewMatchHistoryRepository(db)
		couponRepo := sqlite.NewCouponRepository(db)

		// Test on a scaled threshold first: maxKeep = 100, count = 99 and count = 100
		for i := 1; i <= 99; i++ {
			if _, err := db.ExecContext(ctx, `
				INSERT INTO matched_products (user_id, channel_id, message_text, created_at)
				VALUES (?, ?, ?, ?)
			`, u.ID, -100, fmt.Sprintf("m%d", i), time.Now().UTC()); err != nil {
				t.Fatalf("insert match failed: %v", err)
			}
			if _, err := db.ExecContext(ctx, `
				INSERT INTO coupons (code, store_name, store_url, channel_id, message_text, created_at)
				VALUES (?, ?, ?, ?, ?, ?)
			`, fmt.Sprintf("C%d", i), "Store", "https://s.com", -100, "txt", time.Now().UTC()); err != nil {
				t.Fatalf("insert coupon failed: %v", err)
			}
		}

		// At 99 records (less than 100) -> PruneExcessAbove must return 0
		pruned, err := matchRepo.PruneExcessAbove(ctx, 100, 1000)
		if err != nil {
			t.Fatalf("PruneExcessAbove failed at 99: %v", err)
		}
		if pruned != 0 {
			t.Fatalf("expected 0 pruned at 99, got %d", pruned)
		}

		cPruned, err := couponRepo.PruneExcessAbove(ctx, 100, 1000)
		if err != nil {
			t.Fatalf("PruneExcessAbove coupon failed at 99: %v", err)
		}
		if cPruned != 0 {
			t.Fatalf("expected 0 pruned at 99, got %d", cPruned)
		}

		// Insert 1 more record to reach exactly 100
		if _, err := db.ExecContext(ctx, `
			INSERT INTO matched_products (user_id, channel_id, message_text, created_at)
			VALUES (?, ?, ?, ?)
		`, u.ID, -100, "m100", time.Now().UTC()); err != nil {
			t.Fatalf("insert match 100 failed: %v", err)
		}
		if _, err := db.ExecContext(ctx, `
			INSERT INTO coupons (code, store_name, store_url, channel_id, message_text, created_at)
			VALUES (?, ?, ?, ?, ?, ?)
		`, "C100", "Store", "https://s.com", -100, "txt", time.Now().UTC()); err != nil {
			t.Fatalf("insert coupon 100 failed: %v", err)
		}

		// At exactly 100 records (equal to 100) -> PruneExcessAbove must return 0
		pruned, err = matchRepo.PruneExcessAbove(ctx, 100, 1000)
		if err != nil {
			t.Fatalf("PruneExcessAbove failed at 100: %v", err)
		}
		if pruned != 0 {
			t.Fatalf("expected 0 pruned at 100, got %d", pruned)
		}

		cPruned, err = couponRepo.PruneExcessAbove(ctx, 100, 1000)
		if err != nil {
			t.Fatalf("PruneExcessAbove coupon failed at 100: %v", err)
		}
		if cPruned != 0 {
			t.Fatalf("expected 0 pruned at 100, got %d", cPruned)
		}
	})

	t.Run("full_scale_102500_records_ceiling_enforcement", func(t *testing.T) {
		// Test the exact 102,500 record count with maxKeep = 100,000 and batchSize = 1,000
		db := newTestDB(t)
		userRepo := sqlite.NewUserRepository(db)
		u := &domain.User{Username: "user_102500", PasswordHash: "p", Role: domain.RoleUser, NtfyTopic: "t"}
		if err := userRepo.CreateUser(ctx, u); err != nil {
			t.Fatalf("CreateUser failed: %v", err)
		}

		matchRepo := sqlite.NewMatchHistoryRepository(db)

		// Fast bulk insert using recursive CTE
		query := fmt.Sprintf(`
			WITH RECURSIVE cnt(x) AS (
				SELECT 1
				UNION ALL
				SELECT x + 1 FROM cnt WHERE x < 102500
			)
			INSERT INTO matched_products (id, user_id, channel_id, message_text, created_at)
			SELECT
				x,
				%d,
				-100,
				'Match item ' || x,
				datetime('2026-01-01 00:00:00', '+' || x || ' seconds')
			FROM cnt;
		`, u.ID)
		startInsert := time.Now()
		if _, err := db.ExecContext(ctx, query); err != nil {
			t.Fatalf("bulk insert 102500 records failed: %v", err)
		}
		t.Logf("Bulk inserted 102,500 records in %v", time.Since(startInsert))

		initialCount, err := matchRepo.CountTotal(ctx)
		if err != nil {
			t.Fatalf("CountTotal failed: %v", err)
		}
		if initialCount != 102500 {
			t.Fatalf("expected initial count 102,500, got %d", initialCount)
		}

		// Prune loop as executed by OfferService
		maxKeep := int64(100000)
		batchSize := 1000
		var totalPruned int64
		var loopCount int

		startPrune := time.Now()
		for {
			loopCount++
			count, err := matchRepo.CountTotal(ctx)
			if err != nil {
				t.Fatalf("CountTotal failed on loop %d: %v", loopCount, err)
			}
			if count <= maxKeep {
				break
			}
			pruned, err := matchRepo.PruneExcessAbove(ctx, maxKeep, batchSize)
			if err != nil {
				t.Fatalf("PruneExcessAbove failed on loop %d: %v", loopCount, err)
			}
			if pruned == 0 {
				break
			}
			totalPruned += pruned
		}
		t.Logf("Pruned %d records in %d loops in %v", totalPruned, loopCount, time.Since(startPrune))

		// Verification assertions
		if totalPruned != 2500 {
			t.Fatalf("expected exactly 2,500 records pruned, got %d", totalPruned)
		}
		if loopCount != 4 { // loop 1: 1000, loop 2: 1000, loop 3: 500, loop 4: check count <= 100000 -> break
			t.Fatalf("expected 4 loops, got %d", loopCount)
		}

		finalCount, err := matchRepo.CountTotal(ctx)
		if err != nil {
			t.Fatalf("CountTotal after pruning failed: %v", err)
		}
		if finalCount != 100000 {
			t.Fatalf("expected exactly 100,000 records remaining, got %d", finalCount)
		}

		// Verify that the 2,500 OLDEST records (id 1..2500) were deleted, and id 2501..102500 remain
		var minID, maxID int64
		if err := db.QueryRowContext(ctx, "SELECT MIN(id), MAX(id) FROM matched_products").Scan(&minID, &maxID); err != nil {
			t.Fatalf("query min/max id failed: %v", err)
		}
		if minID != 2501 {
			t.Fatalf("expected min id to be 2501 (oldest 2500 pruned), got %d", minID)
		}
		if maxID != 102500 {
			t.Fatalf("expected max id to be 102500, got %d", maxID)
		}
	})
}

func TestSearchAndPagination_EdgeCases(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := newTestDB(t)

	userRepo := sqlite.NewUserRepository(db)
	channelRepo := sqlite.NewChannelRepository(db)
	interestRepo := sqlite.NewInterestRepository(db)
	matchRepo := sqlite.NewMatchHistoryRepository(db)
	couponRepo := sqlite.NewCouponRepository(db)

	u := &domain.User{Username: "search_tester", PasswordHash: "p", Role: domain.RoleUser, NtfyTopic: "t"}
	if err := userRepo.CreateUser(ctx, u); err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}

	ch := &domain.TelegramChannel{ChatID: -1001, Title: "Special Deals & Promos", Username: "specialdeals"}
	if err := channelRepo.UpsertChannel(ctx, ch); err != nil {
		t.Fatalf("UpsertChannel failed: %v", err)
	}

	interest := &domain.UserInterest{
		UserID:         u.ID,
		RawTerm:        "RTX 4070 Ti",
		PositiveTokens: []string{"rtx", "4070", "ti"},
		Active:         true,
	}
	if err := interestRepo.CreateInterest(ctx, interest); err != nil {
		t.Fatalf("CreateInterest failed: %v", err)
	}

	// Insert test records with various special characters
	testTexts := []string{
		"Placa de Vídeo RTX 4070 Ti 12GB - R$ 4.299,00 à vista",
		"Notebook 100% novo com 50% de desconto!",
		"Produto_com_underlines_e_traços",
		"O'Connor Livro raro em promoção na Livraria",
		"Promoção com aspas \"especiais\" e barra \\ invertida",
		"SQL Injection test: '; DROP TABLE matched_products; --",
	}

	for _, txt := range testTexts {
		if err := matchRepo.RecordProductMatch(ctx, u.ID, &interest.ID, ch.ChatID, nil, txt); err != nil {
			t.Fatalf("RecordProductMatch failed for %q: %v", txt, err)
		}
		if err := couponRepo.RecordCoupon(ctx, "DISC_10%", "Store & Co.", "https://store.com?ref=1&b=2", ch.ChatID, txt); err != nil {
			t.Fatalf("RecordCoupon failed for %q: %v", txt, err)
		}
	}

	tests := []struct {
		name        string
		query       string
		wantMinHits int
	}{
		{
			name:        "empty_string_returns_all",
			query:       "",
			wantMinHits: len(testTexts),
		},
		{
			name:        "whitespace_only_trimmed_to_empty",
			query:       "    \t  \n  ",
			wantMinHits: len(testTexts),
		},
		{
			name:        "percent_wildcard_escaped_safely",
			query:       "%",
			wantMinHits: 1, // matches "100% novo com 50% de desconto!"
		},
		{
			name:        "underscore_wildcard_escaped_safely",
			query:       "_",
			wantMinHits: 1, // matches "Produto_com_underlines_e_traços"
		},
		{
			name:        "single_quote_safe",
			query:       "'",
			wantMinHits: 1, // matches "O'Connor..."
		},
		{
			name:        "double_quote_safe",
			query:       "\"",
			wantMinHits: 1,
		},
		{
			name:        "sql_injection_attempt_safe",
			query:       "'; DROP TABLE",
			wantMinHits: 1, // matches literal string
		},
		{
			name:        "backslash_safe",
			query:       "\\",
			wantMinHits: 1,
		},
		{
			name:        "unicode_search",
			query:       "traços",
			wantMinHits: 1,
		},
		{
			name:        "non_matching_search",
			query:       "nonexistent_xyz_999",
			wantMinHits: 0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Matched products
			items, total, err := matchRepo.ListUserMatchedProducts(ctx, u.ID, tc.query, 50, 0)
			if err != nil {
				t.Fatalf("ListUserMatchedProducts failed for query %q: %v", tc.query, err)
			}
			switch {
			case tc.query == "" || tc.query == "    \t  \n  ":
				if total != len(testTexts) || len(items) != len(testTexts) {
					t.Fatalf("expected all %d items, got total=%d, len=%d", len(testTexts), total, len(items))
				}
			case tc.name == "non_matching_search":
				if total != 0 || len(items) != 0 {
					t.Fatalf("expected 0 items, got total=%d, len=%d", total, len(items))
				}
			default:
				if total < tc.wantMinHits || len(items) < tc.wantMinHits {
					t.Fatalf("query %q: expected at least %d hits, got total=%d len=%d", tc.query, tc.wantMinHits, total, len(items))
				}
			}

			// Extracted coupons
			cItems, cTotal, err := couponRepo.ListExtractedCoupons(ctx, tc.query, 50, 0)
			if err != nil {
				t.Fatalf("ListExtractedCoupons failed for query %q: %v", tc.query, err)
			}
			switch {
			case tc.query == "" || tc.query == "    \t  \n  ":
				if cTotal != len(testTexts) || len(cItems) != len(testTexts) {
					t.Fatalf("expected all %d coupons, got total=%d, len=%d", len(testTexts), cTotal, len(cItems))
				}
			case tc.name == "non_matching_search":
				if cTotal != 0 || len(cItems) != 0 {
					t.Fatalf("expected 0 coupons, got total=%d, len=%d", cTotal, len(cItems))
				}
			}
		})
	}
}

func TestUserIsolation_StrictEnforcement(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := newTestDB(t)

	userRepo := sqlite.NewUserRepository(db)
	matchRepo := sqlite.NewMatchHistoryRepository(db)

	u1 := &domain.User{Username: "user_alice", PasswordHash: "p", Role: domain.RoleUser, NtfyTopic: "t1"}
	u2 := &domain.User{Username: "user_bob", PasswordHash: "p", Role: domain.RoleUser, NtfyTopic: "t2"}

	if err := userRepo.CreateUser(ctx, u1); err != nil {
		t.Fatalf("CreateUser u1 failed: %v", err)
	}
	if err := userRepo.CreateUser(ctx, u2); err != nil {
		t.Fatalf("CreateUser u2 failed: %v", err)
	}

	// Insert 10 secret records for Bob
	for i := 1; i <= 10; i++ {
		text := fmt.Sprintf("Bob Private Deal %d: Confidential Promotion Code %d", i, i)
		if err := matchRepo.RecordProductMatch(ctx, u2.ID, nil, -200, nil, text); err != nil {
			t.Fatalf("RecordProductMatch for Bob failed: %v", err)
		}
	}

	// Alice has 0 records
	// Alice attempts to list with empty query
	aliceItems, aliceTotal, err := matchRepo.ListUserMatchedProducts(ctx, u1.ID, "", 50, 0)
	if err != nil {
		t.Fatalf("ListUserMatchedProducts alice empty query failed: %v", err)
	}
	if aliceTotal != 0 || len(aliceItems) != 0 {
		t.Fatalf("ISOLATION LEAK: Alice saw Bob's records with empty query! total=%d len=%d", aliceTotal, len(aliceItems))
	}

	// Alice attempts to search explicitly for "Bob Private Deal"
	aliceSearchItems, aliceSearchTotal, err := matchRepo.ListUserMatchedProducts(ctx, u1.ID, "Bob Private Deal", 50, 0)
	if err != nil {
		t.Fatalf("ListUserMatchedProducts alice targeted search failed: %v", err)
	}
	if aliceSearchTotal != 0 || len(aliceSearchItems) != 0 {
		t.Fatalf("ISOLATION LEAK: Alice saw Bob's records with targeted search! total=%d len=%d", aliceSearchTotal, len(aliceSearchItems))
	}

	// Alice attempts to search with wildcard "%"
	aliceWildcardItems, aliceWildcardTotal, err := matchRepo.ListUserMatchedProducts(ctx, u1.ID, "%", 50, 0)
	if err != nil {
		t.Fatalf("ListUserMatchedProducts alice wildcard failed: %v", err)
	}
	if aliceWildcardTotal != 0 || len(aliceWildcardItems) != 0 {
		t.Fatalf("ISOLATION LEAK: Alice saw Bob's records with wildcard search! total=%d len=%d", aliceWildcardTotal, len(aliceWildcardItems))
	}

	// Bob lists his records - should see all 10
	bobItems, bobTotal, err := matchRepo.ListUserMatchedProducts(ctx, u2.ID, "", 50, 0)
	if err != nil {
		t.Fatalf("ListUserMatchedProducts bob failed: %v", err)
	}
	if bobTotal != 10 || len(bobItems) != 10 {
		t.Fatalf("Bob failed to see his own records: total=%d len=%d", bobTotal, len(bobItems))
	}
	for _, it := range bobItems {
		if it.UserID != u2.ID {
			t.Fatalf("Bob record has mismatched UserID: expected %d, got %d", u2.ID, it.UserID)
		}
	}
}
