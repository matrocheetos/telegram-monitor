package sqlite_test

import (
	"context"
	"testing"
	"time"

	"github.com/matrocheetos/telegram-monitor/internal/adapter/secondary/sqlite"
	"github.com/matrocheetos/telegram-monitor/internal/domain"
)

func TestDebugMessageRepository_RecordAndCount(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := newTestDB(t)
	repo := sqlite.NewDebugMessageRepository(db)

	count, err := repo.CountAnalyzedMessages(ctx)
	if err != nil {
		t.Fatalf("CountAnalyzedMessages failed: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected 0 messages, got %d", count)
	}

	lowestPrice := 1499.90
	maxP := 1600.0
	rec := &domain.AnalyzedMessageDebugRecord{
		TelegramMessageID: 101,
		ChannelID:         -1001234567890,
		ChannelTitle:      "Promo Channel",
		ChannelUsername:   "promochannel",
		MonitorProducts:   true,
		MonitorCoupons:    true,
		RawText:           "Smartphone Top por R$ 1.499,90 ou 10x de R$ 160 cupom CUPOM10 https://store.com/p",
		NormalizedText:    "smartphone top por r$ 1499 90 ou 10x de r$ 160 cupom cupom10 https store com p",
		Tokens:            []string{"smartphone", "top", "por", "r$", "1499", "90"},
		HasMedia:          true,
		MediaType:         "photo",
		TGEntities: []domain.TelegramEntityExcerpt{
			{
				Type:   "code",
				Offset: 48,
				Length: 7,
				Text:   "CUPOM10",
			},
		},
		TGInlineButtons: []domain.TelegramInlineButton{
			{
				Text: "Comprar Agora",
				URL:  "https://store.com/p",
			},
		},
		TGWebPageURL:             "",
		PlainURLs:                []string{"https://store.com/p"},
		FirstURL:                 "https://store.com/p",
		AllPrices:                []float64{1499.90, 160.0},
		PricesFoundCount:         2,
		ExtractedLowestPrice:     &lowestPrice,
		HasCouponIndicator:       true,
		IdentifiedStore:          "Oferta",
		IdentifiedStoreURL:       "https://store.com/p",
		ExtractedCoupons:         []domain.DebugExtractedCouponSummary{{Code: "CUPOM10", StoreName: "Oferta", StoreURL: "https://store.com/p", Deduplicated: false}},
		ExtractedCouponsCount:    1,
		DeduplicatedCouponsCount: 0,
		MatchedInterests:         []domain.DebugMatchedInterestSummary{{InterestID: 1, UserID: 10, Term: "smartphone top", MaxPrice: &maxP}},
		MatchedProductsCount:     1,
		EvaluationOutcome:        domain.OutcomeMatchedProductAndCoupon,
		MessageDate:              time.Now().Add(-5 * time.Minute).UTC(),
		AnalyzedAt:               time.Now().UTC(),
	}

	if err := repo.RecordAnalyzedMessage(ctx, rec); err != nil {
		t.Fatalf("RecordAnalyzedMessage failed: %v", err)
	}

	if rec.ID == 0 {
		t.Fatalf("expected rec.ID to be populated, got 0")
	}

	count, err = repo.CountAnalyzedMessages(ctx)
	if err != nil {
		t.Fatalf("CountAnalyzedMessages after insert failed: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 message, got %d", count)
	}

	// Verify reading directly back from sqlite
	var channelTitle, outcome, tokensJSON, entitiesJSON string
	err = db.QueryRowContext(ctx, `
		SELECT channel_title, evaluation_outcome, tokens_json, tg_entities_json
		FROM debug_analyzed_messages
		WHERE id = ?
	`, rec.ID).Scan(&channelTitle, &outcome, &tokensJSON, &entitiesJSON)
	if err != nil {
		t.Fatalf("QueryRow failed: %v", err)
	}

	if channelTitle != "Promo Channel" {
		t.Fatalf("expected channel_title 'Promo Channel', got %q", channelTitle)
	}
	if outcome != string(domain.OutcomeMatchedProductAndCoupon) {
		t.Fatalf("expected outcome %q, got %q", domain.OutcomeMatchedProductAndCoupon, outcome)
	}
	if tokensJSON == "" || tokensJSON == "[]" {
		t.Fatalf("expected populated tokens_json, got %q", tokensJSON)
	}
	if entitiesJSON == "" || entitiesJSON == "[]" {
		t.Fatalf("expected populated tg_entities_json, got %q", entitiesJSON)
	}
}

func TestDebugMessageRepository_PruneOldest(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := newTestDB(t)
	repo := sqlite.NewDebugMessageRepository(db)

	baseTime := time.Date(2026, 11, 10, 10, 0, 0, 0, time.UTC)
	for i := 1; i <= 10; i++ {
		rec := &domain.AnalyzedMessageDebugRecord{
			TelegramMessageID: i,
			ChannelID:         100,
			RawText:           "test msg",
			NormalizedText:    "test msg",
			EvaluationOutcome: domain.OutcomeMonitoredNoMatch,
			MessageDate:       baseTime.Add(time.Duration(i) * time.Minute),
			AnalyzedAt:        baseTime.Add(time.Duration(i) * time.Minute),
		}
		if err := repo.RecordAnalyzedMessage(ctx, rec); err != nil {
			t.Fatalf("RecordAnalyzedMessage %d failed: %v", i, err)
		}
	}

	total, err := repo.CountAnalyzedMessages(ctx)
	if err != nil {
		t.Fatalf("CountAnalyzedMessages failed: %v", err)
	}
	if total != 10 {
		t.Fatalf("expected 10 messages, got %d", total)
	}

	// Prune with maxKeep = 6, batchSize = 10 -> should delete 4 oldest
	deleted, err := repo.PruneOldestDebugMessages(ctx, 6, 10)
	if err != nil {
		t.Fatalf("PruneOldestDebugMessages failed: %v", err)
	}
	if deleted != 4 {
		t.Fatalf("expected 4 deleted rows, got %d", deleted)
	}

	totalAfter, err := repo.CountAnalyzedMessages(ctx)
	if err != nil {
		t.Fatalf("CountAnalyzedMessages after prune failed: %v", err)
	}
	if totalAfter != 6 {
		t.Fatalf("expected 6 messages remaining, got %d", totalAfter)
	}

	// Verify remaining are the newest (IDs 5, 6, 7, 8, 9, 10)
	var minID int
	if err := db.QueryRowContext(ctx, "SELECT MIN(id) FROM debug_analyzed_messages").Scan(&minID); err != nil {
		t.Fatalf("SELECT MIN(id) failed: %v", err)
	}
	if minID != 5 {
		t.Fatalf("expected min id to be 5, got %d", minID)
	}
}

func TestDebugMessageRepository_AnalyticalViews(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := newTestDB(t)
	repo := sqlite.NewDebugMessageRepository(db)

	// 1. Coupon false negative: has indicator or code entity, but extracted_coupons_count = 0
	recFN := &domain.AnalyzedMessageDebugRecord{
		TelegramMessageID:     1,
		ChannelID:             1,
		ChannelTitle:          "False Negative Test",
		RawText:               "Use o cupom especial e aproveite!",
		NormalizedText:        "use o cupom especial e aproveite",
		HasCouponIndicator:    true,
		ExtractedCouponsCount: 0,
		EvaluationOutcome:     domain.OutcomeCouponIndicatorNoCode,
		MessageDate:           time.Now().UTC(),
		AnalyzedAt:            time.Now().UTC(),
	}
	// 2. Normal coupon: has indicator AND extracted_coupons_count = 1 (should NOT appear in v_debug_coupon_false_negatives)
	recNormalCoupon := &domain.AnalyzedMessageDebugRecord{
		TelegramMessageID:     2,
		ChannelID:             1,
		ChannelTitle:          "Normal Coupon",
		RawText:               "Cupom: DESCONTO10",
		NormalizedText:        "cupom desconto10",
		HasCouponIndicator:    true,
		ExtractedCouponsCount: 1,
		EvaluationOutcome:     domain.OutcomeExtractedCoupon,
		MessageDate:           time.Now().UTC(),
		AnalyzedAt:            time.Now().UTC(),
	}
	// 3. Multi-price ambiguity: prices_found_count > 1
	p1 := 1000.0
	recMultiPrice := &domain.AnalyzedMessageDebugRecord{
		TelegramMessageID:    3,
		ChannelID:            1,
		ChannelTitle:         "Multi Price",
		RawText:              "De R$ 1.500 por R$ 1.000",
		NormalizedText:       "de r$ 1500 por r$ 1000",
		AllPrices:            []float64{1500.0, 1000.0},
		PricesFoundCount:     2,
		ExtractedLowestPrice: &p1,
		EvaluationOutcome:    domain.OutcomeMonitoredNoMatch,
		MessageDate:          time.Now().UTC(),
		AnalyzedAt:           time.Now().UTC(),
	}
	// 4. Unrecognized store: has link/button/webpage, but identified_store = 'Oferta'
	recUnrecStore := &domain.AnalyzedMessageDebugRecord{
		TelegramMessageID:  4,
		ChannelID:          1,
		ChannelTitle:       "Unrecognized Store",
		RawText:            "Oferta exclusiva https://unknown-shortener.xyz/item",
		NormalizedText:     "oferta exclusiva https unknown shortener xyz item",
		FirstURL:           "https://unknown-shortener.xyz/item",
		IdentifiedStore:    "Oferta",
		IdentifiedStoreURL: "https://unknown-shortener.xyz/item",
		EvaluationOutcome:  domain.OutcomeMonitoredNoMatch,
		MessageDate:        time.Now().UTC(),
		AnalyzedAt:         time.Now().UTC(),
	}
	// 5. Hidden metadata link: first_url = '', but tg_entities has url
	recHiddenLink := &domain.AnalyzedMessageDebugRecord{
		TelegramMessageID: 5,
		ChannelID:         1,
		ChannelTitle:      "Hidden Link",
		RawText:           "Clique aqui para ver o produto",
		NormalizedText:    "clique aqui para ver o produto",
		FirstURL:          "",
		TGEntities: []domain.TelegramEntityExcerpt{
			{
				Type:   "text_url",
				Offset: 0,
				Length: 11,
				Text:   "Clique aqui",
				URL:    "https://amzn.to/hidden",
			},
		},
		EvaluationOutcome: domain.OutcomeMonitoredNoMatch,
		MessageDate:       time.Now().UTC(),
		AnalyzedAt:        time.Now().UTC(),
	}

	for _, rec := range []*domain.AnalyzedMessageDebugRecord{recFN, recNormalCoupon, recMultiPrice, recUnrecStore, recHiddenLink} {
		if err := repo.RecordAnalyzedMessage(ctx, rec); err != nil {
			t.Fatalf("RecordAnalyzedMessage failed: %v", err)
		}
	}

	// Verify v_debug_coupon_false_negatives
	var fnCount int
	err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM v_debug_coupon_false_negatives").Scan(&fnCount)
	if err != nil {
		t.Fatalf("query v_debug_coupon_false_negatives failed: %v", err)
	}
	if fnCount != 1 {
		t.Fatalf("expected 1 row in v_debug_coupon_false_negatives, got %d", fnCount)
	}

	// Verify v_debug_multi_price_ambiguity
	var multiPriceCount int
	err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM v_debug_multi_price_ambiguity").Scan(&multiPriceCount)
	if err != nil {
		t.Fatalf("query v_debug_multi_price_ambiguity failed: %v", err)
	}
	if multiPriceCount != 1 {
		t.Fatalf("expected 1 row in v_debug_multi_price_ambiguity, got %d", multiPriceCount)
	}

	// Verify v_debug_unrecognized_stores
	var unrecCount int
	err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM v_debug_unrecognized_stores").Scan(&unrecCount)
	if err != nil {
		t.Fatalf("query v_debug_unrecognized_stores failed: %v", err)
	}
	if unrecCount != 1 {
		t.Fatalf("expected 1 row in v_debug_unrecognized_stores, got %d", unrecCount)
	}

	// Verify v_debug_hidden_metadata_links
	var hiddenCount int
	err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM v_debug_hidden_metadata_links").Scan(&hiddenCount)
	if err != nil {
		t.Fatalf("query v_debug_hidden_metadata_links failed: %v", err)
	}
	if hiddenCount != 1 {
		t.Fatalf("expected 1 row in v_debug_hidden_metadata_links, got %d", hiddenCount)
	}
}
