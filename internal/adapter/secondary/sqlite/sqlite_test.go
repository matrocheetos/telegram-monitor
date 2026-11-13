package sqlite_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/matrocheetos/telegram-monitor/internal/adapter/secondary/sqlite"
	"github.com/matrocheetos/telegram-monitor/internal/domain"
	"github.com/matrocheetos/telegram-monitor/internal/port"
)

func newTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sqlite.OpenDB(context.Background(), ":memory:")
	if err != nil {
		t.Fatalf("OpenDB failed: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})
	return db
}

func TestOpenDB(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := newTestDB(t)

	if err := db.PingContext(ctx); err != nil {
		t.Fatalf("PingContext failed: %v", err)
	}

	var fkEnabled int
	if err := db.QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&fkEnabled); err != nil {
		t.Fatalf("query foreign_keys pragma failed: %v", err)
	}
	if fkEnabled != 1 {
		t.Fatalf("expected foreign_keys PRAGMA to be 1, got %d", fkEnabled)
	}
}

func TestUserRepository(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := newTestDB(t)
	repo := sqlite.NewUserRepository(db)

	count, err := repo.CountUsers(ctx)
	if err != nil {
		t.Fatalf("CountUsers failed: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected 0 users, got %d", count)
	}

	admin := &domain.User{
		Username:     "admin",
		PasswordHash: "$argon2id$dummyhash_admin", // #nosec G101 -- test dummy hash
		Role:         domain.RoleAdmin,
		NtfyTopic:    "promo-user-admin",
	}
	if err := repo.CreateUser(ctx, admin); err != nil {
		t.Fatalf("CreateUser admin failed: %v", err)
	}
	if admin.ID == 0 {
		t.Fatalf("expected positive ID, got %d", admin.ID)
	}

	user := &domain.User{
		Username:     "regular",
		PasswordHash: "$argon2id$dummyhash_regular", // #nosec G101 -- test dummy hash
		Role:         domain.RoleUser,
		NtfyTopic:    "promo-user-regular",
	}
	if err := repo.CreateUser(ctx, user); err != nil {
		t.Fatalf("CreateUser user failed: %v", err)
	}

	count, err = repo.CountUsers(ctx)
	if err != nil {
		t.Fatalf("CountUsers after insert failed: %v", err)
	}
	if count != 2 {
		t.Fatalf("expected 2 users, got %d", count)
	}

	foundByID, err := repo.GetUserByID(ctx, admin.ID)
	if err != nil {
		t.Fatalf("GetUserByID failed: %v", err)
	}
	if foundByID.Username != admin.Username || foundByID.Role != admin.Role {
		t.Fatalf("unexpected user: %+v", foundByID)
	}

	foundByUsername, err := repo.GetUserByUsername(ctx, "regular")
	if err != nil {
		t.Fatalf("GetUserByUsername failed: %v", err)
	}
	if foundByUsername.ID != user.ID {
		t.Fatalf("expected ID %d, got %d", user.ID, foundByUsername.ID)
	}

	_, err = repo.GetUserByID(ctx, 9999)
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for non-existent ID, got %v", err)
	}

	_, err = repo.GetUserByUsername(ctx, "nonexistent")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for non-existent username, got %v", err)
	}

	dup := &domain.User{
		Username:     "admin",
		PasswordHash: "another_hash",
		Role:         domain.RoleUser,
		NtfyTopic:    "topic",
	}
	if err := repo.CreateUser(ctx, dup); !errors.Is(err, domain.ErrDuplicateEntry) {
		t.Fatalf("expected ErrDuplicateEntry, got %v", err)
	}

	users, err := repo.ListUsers(ctx)
	if err != nil {
		t.Fatalf("ListUsers failed: %v", err)
	}
	if len(users) != 2 {
		t.Fatalf("expected 2 users, got %d", len(users))
	}

	if err := repo.UpdatePassword(ctx, user.ID, "updated_hash"); err != nil {
		t.Fatalf("UpdatePassword failed: %v", err)
	}
	updated, err := repo.GetUserByID(ctx, user.ID)
	if err != nil {
		t.Fatalf("GetUserByID failed: %v", err)
	}
	if updated.PasswordHash != "updated_hash" {
		t.Fatalf("expected updated_hash, got %s", updated.PasswordHash)
	}

	if err := repo.UpdatePassword(ctx, 9999, "hash"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for invalid ID update, got %v", err)
	}

	if err := repo.DeleteUser(ctx, user.ID); err != nil {
		t.Fatalf("DeleteUser failed: %v", err)
	}
	if err := repo.DeleteUser(ctx, user.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound after delete, got %v", err)
	}
}

func TestSessionRepository(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := newTestDB(t)
	userRepo := sqlite.NewUserRepository(db)
	sessionRepo := sqlite.NewSessionRepository(db)

	u := &domain.User{
		Username:     "session_user",
		PasswordHash: "pass",
		Role:         domain.RoleUser,
		NtfyTopic:    "promo-user-session",
	}
	if err := userRepo.CreateUser(ctx, u); err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}

	activeHash := "token_hash_active"
	expiredHash := "token_hash_expired"

	if err := sessionRepo.CreateSession(ctx, activeHash, u.ID, time.Now().Add(1*time.Hour)); err != nil {
		t.Fatalf("CreateSession active failed: %v", err)
	}
	if err := sessionRepo.CreateSession(ctx, expiredHash, u.ID, time.Now().Add(-1*time.Hour)); err != nil {
		t.Fatalf("CreateSession expired failed: %v", err)
	}

	sessUser, err := sessionRepo.GetSessionUser(ctx, activeHash)
	if err != nil {
		t.Fatalf("GetSessionUser active failed: %v", err)
	}
	if sessUser.ID != u.ID || sessUser.Username != u.Username {
		t.Fatalf("unexpected session user: %+v", sessUser)
	}

	_, err = sessionRepo.GetSessionUser(ctx, expiredHash)
	if !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatalf("expected ErrUnauthorized for expired session, got %v", err)
	}

	_, err = sessionRepo.GetSessionUser(ctx, "nonexistent_token")
	if !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatalf("expected ErrUnauthorized for nonexistent session, got %v", err)
	}

	if err := sessionRepo.DeleteExpiredSessions(ctx); err != nil {
		t.Fatalf("DeleteExpiredSessions failed: %v", err)
	}

	if err := sessionRepo.DeleteSession(ctx, activeHash); err != nil {
		t.Fatalf("DeleteSession failed: %v", err)
	}

	_, err = sessionRepo.GetSessionUser(ctx, activeHash)
	if !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatalf("expected ErrUnauthorized after delete, got %v", err)
	}
}

func TestCredentialRepository(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := newTestDB(t)
	repo := sqlite.NewCredentialRepository(db)

	_, err := repo.GetTelegramCredentials(ctx)
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound initially, got %v", err)
	}

	creds := &port.TelegramCredentials{
		EncryptedAPIID:   []byte("enc_api_id"),
		EncryptedAPIHash: []byte("enc_api_hash"),
		EncryptedSession: []byte("enc_session_blob"),
		PhoneMasked:      "+55 11 *****-1234",
	}
	if err := repo.SaveTelegramCredentials(ctx, creds); err != nil {
		t.Fatalf("SaveTelegramCredentials failed: %v", err)
	}

	saved, err := repo.GetTelegramCredentials(ctx)
	if err != nil {
		t.Fatalf("GetTelegramCredentials failed: %v", err)
	}
	if string(saved.EncryptedAPIID) != "enc_api_id" || saved.PhoneMasked != creds.PhoneMasked {
		t.Fatalf("unexpected saved credentials: %+v", saved)
	}

	creds.PhoneMasked = "+55 11 *****-9999"
	if err := repo.SaveTelegramCredentials(ctx, creds); err != nil {
		t.Fatalf("SaveTelegramCredentials update failed: %v", err)
	}

	updated, err := repo.GetTelegramCredentials(ctx)
	if err != nil {
		t.Fatalf("GetTelegramCredentials after update failed: %v", err)
	}
	if updated.PhoneMasked != "+55 11 *****-9999" {
		t.Fatalf("expected updated phone masked, got %s", updated.PhoneMasked)
	}

	if err := repo.DeleteTelegramCredentials(ctx); err != nil {
		t.Fatalf("DeleteTelegramCredentials failed: %v", err)
	}

	_, err = repo.GetTelegramCredentials(ctx)
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound after delete, got %v", err)
	}
}

func TestChannelRepository(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := newTestDB(t)
	repo := sqlite.NewChannelRepository(db)

	ch1 := &domain.TelegramChannel{
		ChatID:          -1001234567890,
		AccessHash:      987654321,
		Username:        "tech_promos",
		Title:           "Tech Promos",
		MonitorProducts: true,
		MonitorCoupons:  false,
	}
	ch2 := &domain.TelegramChannel{
		ChatID:          -1009876543210,
		AccessHash:      123456789,
		Username:        "all_coupons",
		Title:           "All Coupons",
		MonitorProducts: false,
		MonitorCoupons:  true,
	}

	if err := repo.UpsertChannel(ctx, ch1); err != nil {
		t.Fatalf("UpsertChannel ch1 failed: %v", err)
	}
	if err := repo.UpsertChannel(ctx, ch2); err != nil {
		t.Fatalf("UpsertChannel ch2 failed: %v", err)
	}

	found, err := repo.GetChannel(ctx, ch1.ChatID)
	if err != nil {
		t.Fatalf("GetChannel failed: %v", err)
	}
	if found.Title != ch1.Title || !found.MonitorProducts || found.MonitorCoupons {
		t.Fatalf("unexpected channel: %+v", found)
	}

	_, err = repo.GetChannel(ctx, -99999)
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}

	channels, err := repo.ListChannels(ctx)
	if err != nil {
		t.Fatalf("ListChannels failed: %v", err)
	}
	if len(channels) != 2 {
		t.Fatalf("expected 2 channels, got %d", len(channels))
	}
	if channels[0].Title != "All Coupons" {
		t.Fatalf("expected alphabetical sorting, first is %s", channels[0].Title)
	}

	if err := repo.UpdateChannelFlags(ctx, ch1.ChatID, false, true); err != nil {
		t.Fatalf("UpdateChannelFlags failed: %v", err)
	}
	updated, err := repo.GetChannel(ctx, ch1.ChatID)
	if err != nil {
		t.Fatalf("GetChannel after flags update failed: %v", err)
	}
	if updated.MonitorProducts || !updated.MonitorCoupons {
		t.Fatalf("expected updated flags false/true, got %v/%v", updated.MonitorProducts, updated.MonitorCoupons)
	}

	if err := repo.UpdateChannelFlags(ctx, -99999, true, true); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound on invalid chat ID, got %v", err)
	}
}

func TestInterestRepository(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := newTestDB(t)
	userRepo := sqlite.NewUserRepository(db)
	repo := sqlite.NewInterestRepository(db)

	u := &domain.User{
		Username:     "interest_user",
		PasswordHash: "pass",
		Role:         domain.RoleUser,
		NtfyTopic:    "promo-user-interest",
	}
	if err := userRepo.CreateUser(ctx, u); err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}

	maxPrice := 2500.00
	interest := &domain.UserInterest{
		UserID:         u.ID,
		RawTerm:        "switch oled -capa",
		PositiveTokens: []string{"switch", "oled"},
		NegativeTokens: []string{"capa"},
		MaxPrice:       &maxPrice,
		Active:         true,
	}

	if err := repo.CreateInterest(ctx, interest); err != nil {
		t.Fatalf("CreateInterest failed: %v", err)
	}
	if interest.ID == 0 {
		t.Fatalf("expected ID to be assigned, got 0")
	}

	interests, err := repo.ListInterestsByUserID(ctx, u.ID)
	if err != nil {
		t.Fatalf("ListInterestsByUserID failed: %v", err)
	}
	if len(interests) != 1 {
		t.Fatalf("expected 1 interest, got %d", len(interests))
	}
	if interests[0].RawTerm != "switch oled -capa" || *interests[0].MaxPrice != 2500.00 {
		t.Fatalf("unexpected interest: %+v", interests[0])
	}

	activeInterests, err := repo.ListAllActiveInterests(ctx)
	if err != nil {
		t.Fatalf("ListAllActiveInterests failed: %v", err)
	}
	if len(activeInterests) != 1 {
		t.Fatalf("expected 1 active interest, got %d", len(activeInterests))
	}

	if err := repo.DeleteInterest(ctx, 9999, interest.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound on wrong userID, got %v", err)
	}

	if err := repo.DeleteInterest(ctx, u.ID, interest.ID); err != nil {
		t.Fatalf("DeleteInterest failed: %v", err)
	}

	interests, err = repo.ListInterestsByUserID(ctx, u.ID)
	if err != nil {
		t.Fatalf("ListInterestsByUserID after delete failed: %v", err)
	}
	if len(interests) != 0 {
		t.Fatalf("expected 0 interests, got %d", len(interests))
	}
}

func TestMatchHistoryAndCouponRepositories(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := newTestDB(t)
	userRepo := sqlite.NewUserRepository(db)
	matchRepo := sqlite.NewMatchHistoryRepository(db)
	couponRepo := sqlite.NewCouponRepository(db)

	u := &domain.User{
		Username:     "match_user",
		PasswordHash: "pass",
		Role:         domain.RoleUser,
		NtfyTopic:    "promo-user-match",
	}
	if err := userRepo.CreateUser(ctx, u); err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}

	price := 1999.99
	if err := matchRepo.RecordProductMatch(ctx, u.ID, nil, -1001234567890, &price, "Switch OLED R$ 1.999,99"); err != nil {
		t.Fatalf("RecordProductMatch with price failed: %v", err)
	}
	if err := matchRepo.RecordProductMatch(ctx, u.ID, nil, -1001234567890, nil, "Switch OLED sem preco"); err != nil {
		t.Fatalf("RecordProductMatch without price failed: %v", err)
	}

	if err := couponRepo.RecordCoupon(ctx, "PROMO10", "Amazon", "https://amzn.to/test", -1009876543210, "Cupom: PROMO10"); err != nil {
		t.Fatalf("RecordCoupon failed: %v", err)
	}

	var matchCount int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM matched_products WHERE user_id = ?", u.ID).Scan(&matchCount); err != nil {
		t.Fatalf("count matched products failed: %v", err)
	}
	if matchCount != 2 {
		t.Fatalf("expected 2 matches, got %d", matchCount)
	}

	var couponCount int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM coupons WHERE code = ?", "PROMO10").Scan(&couponCount); err != nil {
		t.Fatalf("count coupons failed: %v", err)
	}
	if couponCount != 1 {
		t.Fatalf("expected 1 coupon, got %d", couponCount)
	}
}

func TestForeignKeyCascades(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := newTestDB(t)
	userRepo := sqlite.NewUserRepository(db)
	sessionRepo := sqlite.NewSessionRepository(db)
	interestRepo := sqlite.NewInterestRepository(db)
	matchRepo := sqlite.NewMatchHistoryRepository(db)

	cascadeUser := &domain.User{
		Username:     "cascade_user",
		PasswordHash: "pass",
		Role:         domain.RoleUser,
		NtfyTopic:    "promo-user-cascade",
	}
	if err := userRepo.CreateUser(ctx, cascadeUser); err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}

	if err := sessionRepo.CreateSession(ctx, "cascade_token", cascadeUser.ID, time.Now().Add(1*time.Hour)); err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	cascadeInterest := &domain.UserInterest{
		UserID:         cascadeUser.ID,
		RawTerm:        "cascade term",
		PositiveTokens: []string{"cascade"},
		NegativeTokens: []string{},
		Active:         true,
	}
	if err := interestRepo.CreateInterest(ctx, cascadeInterest); err != nil {
		t.Fatalf("CreateInterest failed: %v", err)
	}

	if err := matchRepo.RecordProductMatch(ctx, cascadeUser.ID, &cascadeInterest.ID, -1001234567890, nil, "cascade text"); err != nil {
		t.Fatalf("RecordProductMatch failed: %v", err)
	}

	if err := userRepo.DeleteUser(ctx, cascadeUser.ID); err != nil {
		t.Fatalf("DeleteUser failed: %v", err)
	}

	var sessCount int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM sessions WHERE user_id = ?", cascadeUser.ID).Scan(&sessCount); err != nil {
		t.Fatalf("count sessions failed: %v", err)
	}
	if sessCount != 0 {
		t.Fatalf("expected 0 sessions after cascade, got %d", sessCount)
	}

	var intCount int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM user_interests WHERE user_id = ?", cascadeUser.ID).Scan(&intCount); err != nil {
		t.Fatalf("count interests failed: %v", err)
	}
	if intCount != 0 {
		t.Fatalf("expected 0 interests after cascade, got %d", intCount)
	}

	var matchCount int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM matched_products WHERE user_id = ?", cascadeUser.ID).Scan(&matchCount); err != nil {
		t.Fatalf("count matched products failed: %v", err)
	}
	if matchCount != 0 {
		t.Fatalf("expected 0 matches after cascade, got %d", matchCount)
	}
}

func TestMigration002_CompositeIndexes(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := newTestDB(t)

	indexes := []string{
		"idx_matched_products_created_id",
		"idx_matched_products_user_created",
		"idx_coupons_created_id",
		"idx_coupons_store_code_created",
	}

	for _, idx := range indexes {
		var name string
		err := db.QueryRowContext(ctx, "SELECT name FROM sqlite_master WHERE type='index' AND name=?", idx).Scan(&name)
		if err != nil {
			t.Fatalf("expected index %s to exist: %v", idx, err)
		}
		if name != idx {
			t.Fatalf("expected index name %s, got %s", idx, name)
		}
	}
}

func TestMatchHistoryRepository_ListAndFilters(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := newTestDB(t)

	userRepo := sqlite.NewUserRepository(db)
	channelRepo := sqlite.NewChannelRepository(db)
	interestRepo := sqlite.NewInterestRepository(db)
	matchRepo := sqlite.NewMatchHistoryRepository(db)

	u1 := &domain.User{Username: "alice", PasswordHash: "h1", Role: domain.RoleUser, NtfyTopic: "promo-user-alice"}
	if err := userRepo.CreateUser(ctx, u1); err != nil {
		t.Fatalf("CreateUser u1 failed: %v", err)
	}
	u2 := &domain.User{Username: "bob", PasswordHash: "h2", Role: domain.RoleUser, NtfyTopic: "promo-user-bob"}
	if err := userRepo.CreateUser(ctx, u2); err != nil {
		t.Fatalf("CreateUser u2 failed: %v", err)
	}

	ch := &domain.TelegramChannel{
		ChatID:   -1001111222,
		Title:    "Promos Tech",
		Username: "promostech",
	}
	if err := channelRepo.UpsertChannel(ctx, ch); err != nil {
		t.Fatalf("UpsertChannel failed: %v", err)
	}

	interest := &domain.UserInterest{
		UserID:         u1.ID,
		RawTerm:        "RTX 4060",
		PositiveTokens: []string{"rtx", "4060"},
		NegativeTokens: []string{},
		Active:         true,
	}
	if err := interestRepo.CreateInterest(ctx, interest); err != nil {
		t.Fatalf("CreateInterest failed: %v", err)
	}

	price1 := 1899.90
	price2 := 1950.00

	if err := matchRepo.RecordProductMatch(ctx, u1.ID, &interest.ID, ch.ChatID, &price1, "RTX 4060 8GB em promo https://amzn.to/abc"); err != nil {
		t.Fatalf("RecordProductMatch 1 failed: %v", err)
	}
	time.Sleep(10 * time.Millisecond)
	if err := matchRepo.RecordProductMatch(ctx, u1.ID, &interest.ID, ch.ChatID, &price2, "Outra RTX 4060 Ti na loja https://kabum.com.br/123"); err != nil {
		t.Fatalf("RecordProductMatch 2 failed: %v", err)
	}
	// Match for bob
	if err := matchRepo.RecordProductMatch(ctx, u2.ID, nil, ch.ChatID, nil, "Bob RTX deal https://meli.la/xyz"); err != nil {
		t.Fatalf("RecordProductMatch bob failed: %v", err)
	}

	// 1. List for Alice - should return 2 items, newest first
	aliceMatches, total, err := matchRepo.ListUserMatchedProducts(ctx, u1.ID, "", 10, 0)
	if err != nil {
		t.Fatalf("ListUserMatchedProducts alice failed: %v", err)
	}
	if total != 2 {
		t.Fatalf("expected total 2 for alice, got %d", total)
	}
	if len(aliceMatches) != 2 {
		t.Fatalf("expected 2 items for alice, got %d", len(aliceMatches))
	}
	if aliceMatches[0].ExtractedPrice == nil || *aliceMatches[0].ExtractedPrice != price2 {
		t.Fatalf("expected newest match price %f, got %v", price2, aliceMatches[0].ExtractedPrice)
	}
	if aliceMatches[0].ChannelTitle != "Promos Tech" {
		t.Fatalf("expected channel title 'Promos Tech', got '%s'", aliceMatches[0].ChannelTitle)
	}
	if aliceMatches[0].InterestTerm != "RTX 4060" {
		t.Fatalf("expected interest term 'RTX 4060', got '%s'", aliceMatches[0].InterestTerm)
	}
	if aliceMatches[0].OfferURL != "https://kabum.com.br/123" {
		t.Fatalf("expected OfferURL https://kabum.com.br/123, got '%s'", aliceMatches[0].OfferURL)
	}

	// 2. Search query filter
	filteredMatches, filteredTotal, err := matchRepo.ListUserMatchedProducts(ctx, u1.ID, "kabum", 10, 0)
	if err != nil {
		t.Fatalf("ListUserMatchedProducts search failed: %v", err)
	}
	if filteredTotal != 1 || len(filteredMatches) != 1 {
		t.Fatalf("expected 1 match for search 'kabum', got total=%d len=%d", filteredTotal, len(filteredMatches))
	}

	// 3. User isolation check
	bobMatches, bobTotal, err := matchRepo.ListUserMatchedProducts(ctx, u2.ID, "", 10, 0)
	if err != nil {
		t.Fatalf("ListUserMatchedProducts bob failed: %v", err)
	}
	if bobTotal != 1 || len(bobMatches) != 1 {
		t.Fatalf("expected 1 match for bob, got total=%d len=%d", bobTotal, len(bobMatches))
	}
	if bobMatches[0].InterestTerm != "Termo Removido" {
		t.Fatalf("expected fallback 'Termo Removido' for nil interest, got '%s'", bobMatches[0].InterestTerm)
	}
}

func TestMatchHistoryRepository_RetentionPruning(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := newTestDB(t)

	userRepo := sqlite.NewUserRepository(db)
	matchRepo := sqlite.NewMatchHistoryRepository(db)

	u := &domain.User{Username: "retention_user", PasswordHash: "h", Role: domain.RoleUser, NtfyTopic: "promo-user-retention"}
	if err := userRepo.CreateUser(ctx, u); err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}

	// Insert 5 records older than 90 days and 3 recent records directly
	oldDate := time.Now().UTC().AddDate(0, 0, -100)
	recentDate := time.Now().UTC().AddDate(0, 0, -5)

	for i := 1; i <= 5; i++ {
		_, err := db.ExecContext(ctx, `
			INSERT INTO matched_products (user_id, channel_id, message_text, created_at)
			VALUES (?, ?, ?, ?)
		`, u.ID, -100, fmt.Sprintf("old item %d", i), oldDate.Add(time.Duration(i)*time.Minute))
		if err != nil {
			t.Fatalf("insert old item failed: %v", err)
		}
	}

	for i := 1; i <= 3; i++ {
		_, err := db.ExecContext(ctx, `
			INSERT INTO matched_products (user_id, channel_id, message_text, created_at)
			VALUES (?, ?, ?, ?)
		`, u.ID, -100, fmt.Sprintf("recent item %d", i), recentDate.Add(time.Duration(i)*time.Minute))
		if err != nil {
			t.Fatalf("insert recent item failed: %v", err)
		}
	}

	count, err := matchRepo.CountTotal(ctx)
	if err != nil {
		t.Fatalf("CountTotal failed: %v", err)
	}
	if count != 8 {
		t.Fatalf("expected initial count 8, got %d", count)
	}

	// 1. Batch prune older than 90 days in batches of 2
	cutoff := time.Now().UTC().AddDate(0, 0, -90)
	deleted1, err := matchRepo.PruneOlderThan(ctx, cutoff, 2)
	if err != nil {
		t.Fatalf("PruneOlderThan batch 1 failed: %v", err)
	}
	if deleted1 != 2 {
		t.Fatalf("expected 2 rows pruned in batch 1, got %d", deleted1)
	}

	deleted2, err := matchRepo.PruneOlderThan(ctx, cutoff, 2)
	if err != nil {
		t.Fatalf("PruneOlderThan batch 2 failed: %v", err)
	}
	if deleted2 != 2 {
		t.Fatalf("expected 2 rows pruned in batch 2, got %d", deleted2)
	}

	deleted3, err := matchRepo.PruneOlderThan(ctx, cutoff, 2)
	if err != nil {
		t.Fatalf("PruneOlderThan batch 3 failed: %v", err)
	}
	if deleted3 != 1 {
		t.Fatalf("expected 1 remaining row pruned in batch 3, got %d", deleted3)
	}

	deleted4, err := matchRepo.PruneOlderThan(ctx, cutoff, 2)
	if err != nil {
		t.Fatalf("PruneOlderThan batch 4 failed: %v", err)
	}
	if deleted4 != 0 {
		t.Fatalf("expected 0 rows pruned in batch 4, got %d", deleted4)
	}

	// Remaining should be exactly the 3 recent records
	count, err = matchRepo.CountTotal(ctx)
	if err != nil {
		t.Fatalf("CountTotal after age pruning failed: %v", err)
	}
	if count != 3 {
		t.Fatalf("expected 3 remaining records, got %d", count)
	}

	// 2. Test PruneExcessAbove
	// Insert 7 more recent items to reach 10 items
	for i := 4; i <= 10; i++ {
		_, err := db.ExecContext(ctx, `
			INSERT INTO matched_products (user_id, channel_id, message_text, created_at)
			VALUES (?, ?, ?, ?)
		`, u.ID, -100, fmt.Sprintf("recent item %d", i), recentDate.Add(time.Duration(i)*time.Minute))
		if err != nil {
			t.Fatalf("insert additional item failed: %v", err)
		}
	}

	count, _ = matchRepo.CountTotal(ctx)
	if count != 10 {
		t.Fatalf("expected 10 records, got %d", count)
	}

	// Ceiling = 6, batchSize = 2 -> prunes 2 oldest
	pruned, err := matchRepo.PruneExcessAbove(ctx, 6, 2)
	if err != nil {
		t.Fatalf("PruneExcessAbove batch 1 failed: %v", err)
	}
	if pruned != 2 {
		t.Fatalf("expected 2 pruned, got %d", pruned)
	}

	// Ceiling = 6, batchSize = 2 -> prunes remaining 2 excess
	pruned, err = matchRepo.PruneExcessAbove(ctx, 6, 2)
	if err != nil {
		t.Fatalf("PruneExcessAbove batch 2 failed: %v", err)
	}
	if pruned != 2 {
		t.Fatalf("expected 2 pruned, got %d", pruned)
	}

	// Now count should be 6
	count, _ = matchRepo.CountTotal(ctx)
	if count != 6 {
		t.Fatalf("expected 6 records remaining, got %d", count)
	}

	// Next call should prune 0
	pruned, err = matchRepo.PruneExcessAbove(ctx, 6, 2)
	if err != nil {
		t.Fatalf("PruneExcessAbove noop failed: %v", err)
	}
	if pruned != 0 {
		t.Fatalf("expected 0 pruned when count <= maxKeep, got %d", pruned)
	}
}

func TestCouponRepository_ListAndFilters(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := newTestDB(t)

	channelRepo := sqlite.NewChannelRepository(db)
	couponRepo := sqlite.NewCouponRepository(db)

	ch := &domain.TelegramChannel{
		ChatID:   -100999888,
		Title:    "Canal Cupons",
		Username: "canalcupons",
	}
	if err := channelRepo.UpsertChannel(ctx, ch); err != nil {
		t.Fatalf("UpsertChannel failed: %v", err)
	}

	if err := couponRepo.RecordCoupon(ctx, "AMAZON10", "Amazon", "https://amazon.com.br", ch.ChatID, "10% off em livros"); err != nil {
		t.Fatalf("RecordCoupon 1 failed: %v", err)
	}
	time.Sleep(10 * time.Millisecond)
	if err := couponRepo.RecordCoupon(ctx, "MAGALU20", "Magalu", "https://magalu.com", ch.ChatID, "20 reais de desconto no app"); err != nil {
		t.Fatalf("RecordCoupon 2 failed: %v", err)
	}

	// List all
	coupons, total, err := couponRepo.ListExtractedCoupons(ctx, "", 10, 0)
	if err != nil {
		t.Fatalf("ListExtractedCoupons failed: %v", err)
	}
	if total != 2 || len(coupons) != 2 {
		t.Fatalf("expected 2 coupons, got total=%d len=%d", total, len(coupons))
	}
	if coupons[0].Code != "MAGALU20" {
		t.Fatalf("expected newest coupon MAGALU20 first, got %s", coupons[0].Code)
	}
	if coupons[0].ChannelTitle != "Canal Cupons" {
		t.Fatalf("expected channel title 'Canal Cupons', got '%s'", coupons[0].ChannelTitle)
	}

	// Filter by code
	filtered, totalFiltered, err := couponRepo.ListExtractedCoupons(ctx, "AMAZON", 10, 0)
	if err != nil {
		t.Fatalf("ListExtractedCoupons search failed: %v", err)
	}
	if totalFiltered != 1 || len(filtered) != 1 || filtered[0].Code != "AMAZON10" {
		t.Fatalf("unexpected filtered coupons: %+v", filtered)
	}
}

func TestCouponRepository_RetentionPruning(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := newTestDB(t)

	couponRepo := sqlite.NewCouponRepository(db)

	oldDate := time.Now().UTC().AddDate(0, 0, -95)
	recentDate := time.Now().UTC().AddDate(0, 0, -2)

	for i := 1; i <= 4; i++ {
		_, err := db.ExecContext(ctx, `
			INSERT INTO coupons (code, store_name, store_url, channel_id, message_text, created_at)
			VALUES (?, ?, ?, ?, ?, ?)
		`, fmt.Sprintf("OLD%d", i), "Store", "https://store.com", -100, "text", oldDate.Add(time.Duration(i)*time.Minute))
		if err != nil {
			t.Fatalf("insert old coupon failed: %v", err)
		}
	}

	for i := 1; i <= 3; i++ {
		_, err := db.ExecContext(ctx, `
			INSERT INTO coupons (code, store_name, store_url, channel_id, message_text, created_at)
			VALUES (?, ?, ?, ?, ?, ?)
		`, fmt.Sprintf("REC%d", i), "Store", "https://store.com", -100, "text", recentDate.Add(time.Duration(i)*time.Minute))
		if err != nil {
			t.Fatalf("insert recent coupon failed: %v", err)
		}
	}

	cutoff := time.Now().UTC().AddDate(0, 0, -90)
	deleted, err := couponRepo.PruneOlderThan(ctx, cutoff, 10)
	if err != nil {
		t.Fatalf("PruneOlderThan failed: %v", err)
	}
	if deleted != 4 {
		t.Fatalf("expected 4 deleted, got %d", deleted)
	}

	total, err := couponRepo.CountTotal(ctx)
	if err != nil {
		t.Fatalf("CountTotal failed: %v", err)
	}
	if total != 3 {
		t.Fatalf("expected 3 remaining, got %d", total)
	}

	// Prune ceiling test
	pruned, err := couponRepo.PruneExcessAbove(ctx, 2, 10)
	if err != nil {
		t.Fatalf("PruneExcessAbove failed: %v", err)
	}
	if pruned != 1 {
		t.Fatalf("expected 1 pruned, got %d", pruned)
	}

	total, _ = couponRepo.CountTotal(ctx)
	if total != 2 {
		t.Fatalf("expected 2 coupons left, got %d", total)
	}
}

func TestCouponRepository_HasRecentCoupon(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := newTestDB(t)
	couponRepo := sqlite.NewCouponRepository(db)

	now := time.Now().UTC()
	// Insert coupon created 2 hours ago
	_, err := db.ExecContext(ctx, `
		INSERT INTO coupons (code, store_name, store_url, channel_id, message_text, created_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`, "DESCONTO10", "Amazon", "https://amazon.com.br", -100, "10% off", now.Add(-2*time.Hour))
	if err != nil {
		t.Fatalf("insert coupon failed: %v", err)
	}

	// 1. Check with since = 4 hours ago -> should return true (was created 2h ago >= 4h ago)
	has, err := couponRepo.HasRecentCoupon(ctx, "DESCONTO10", "Amazon", now.Add(-4*time.Hour))
	if err != nil {
		t.Fatalf("HasRecentCoupon failed: %v", err)
	}
	if !has {
		t.Errorf("expected true for coupon created 2h ago with cutoff 4h ago")
	}

	// 2. Case insensitive check -> should return true
	has, err = couponRepo.HasRecentCoupon(ctx, "desconto10", "amazon", now.Add(-4*time.Hour))
	if err != nil {
		t.Fatalf("HasRecentCoupon case-insensitive failed: %v", err)
	}
	if !has {
		t.Errorf("expected true for case-insensitive match")
	}

	// 3. Check with since = 1 hour ago -> should return false (created 2h ago < 1h ago)
	has, err = couponRepo.HasRecentCoupon(ctx, "DESCONTO10", "Amazon", now.Add(-1*time.Hour))
	if err != nil {
		t.Fatalf("HasRecentCoupon failed: %v", err)
	}
	if has {
		t.Errorf("expected false for coupon created 2h ago with cutoff 1h ago")
	}

	// 4. Mismatched store -> should return false
	has, err = couponRepo.HasRecentCoupon(ctx, "DESCONTO10", "Mercado Livre", now.Add(-4*time.Hour))
	if err != nil {
		t.Fatalf("HasRecentCoupon failed: %v", err)
	}
	if has {
		t.Errorf("expected false for different store")
	}

	// 5. Mismatched code -> should return false
	has, err = couponRepo.HasRecentCoupon(ctx, "OUTRO10", "Amazon", now.Add(-4*time.Hour))
	if err != nil {
		t.Fatalf("HasRecentCoupon failed: %v", err)
	}
	if has {
		t.Errorf("expected false for different code")
	}
}

func TestCouponRepository_ListExtractedCoupons_Dedup4Hours(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := newTestDB(t)
	couponRepo := sqlite.NewCouponRepository(db)

	baseTime := time.Date(2026, 11, 10, 10, 0, 0, 0, time.UTC)

	// A1: Amazon, CUPOM10 at 10:00 (first)
	_, _ = db.ExecContext(ctx, `
		INSERT INTO coupons (id, code, store_name, store_url, channel_id, message_text, created_at)
		VALUES (1, 'CUPOM10', 'Amazon', 'https://amazon.com.br', -100, 'text 1', ?)
	`, baseTime)

	// A2: Amazon, CUPOM10 at 11:00 (duplicate, within 4h of A1)
	_, _ = db.ExecContext(ctx, `
		INSERT INTO coupons (id, code, store_name, store_url, channel_id, message_text, created_at)
		VALUES (2, 'CUPOM10', 'Amazon', 'https://amazon.com.br', -100, 'text 2', ?)
	`, baseTime.Add(1*time.Hour))

	// A3: Amazon, CUPOM10 at 12:00 (duplicate, within 4h of A1)
	_, _ = db.ExecContext(ctx, `
		INSERT INTO coupons (id, code, store_name, store_url, channel_id, message_text, created_at)
		VALUES (3, 'CUPOM10', 'Amazon', 'https://amazon.com.br', -100, 'text 3', ?)
	`, baseTime.Add(2*time.Hour))

	// A4: Amazon, CUPOM10 at 17:00 (>4h after A3 at 12:00 and A1 at 10:00)
	_, _ = db.ExecContext(ctx, `
		INSERT INTO coupons (id, code, store_name, store_url, channel_id, message_text, created_at)
		VALUES (4, 'CUPOM10', 'Amazon', 'https://amazon.com.br', -100, 'text 4', ?)
	`, baseTime.Add(7*time.Hour))

	// M1: Magalu, CUPOM10 at 11:00 (different store, should be preserved!)
	_, _ = db.ExecContext(ctx, `
		INSERT INTO coupons (id, code, store_name, store_url, channel_id, message_text, created_at)
		VALUES (5, 'CUPOM10', 'Magalu', 'https://magalu.com', -100, 'text magalu', ?)
	`, baseTime.Add(1*time.Hour))

	coupons, total, err := couponRepo.ListExtractedCoupons(ctx, "", 50, 0)
	if err != nil {
		t.Fatalf("ListExtractedCoupons failed: %v", err)
	}

	// Should have exactly:
	// - id 4: Amazon at 16:00
	// - id 5: Magalu at 11:00
	// - id 1: Amazon at 10:00
	// (id 2 and id 3 are excluded because id 1 exists within 4h before them)
	if total != 3 || len(coupons) != 3 {
		t.Fatalf("expected total=3, len=3, got total=%d len=%d", total, len(coupons))
	}

	expectedIDs := []int64{4, 5, 1}
	for i, expID := range expectedIDs {
		if coupons[i].ID != expID {
			t.Errorf("at index %d: expected id %d, got %d", i, expID, coupons[i].ID)
		}
	}

	// Test with search query
	filtered, filteredTotal, err := couponRepo.ListExtractedCoupons(ctx, "Amazon", 50, 0)
	if err != nil {
		t.Fatalf("ListExtractedCoupons with search failed: %v", err)
	}
	if filteredTotal != 2 || len(filtered) != 2 {
		t.Fatalf("expected filteredTotal=2, got %d", filteredTotal)
	}
	if filtered[0].ID != 4 || filtered[1].ID != 1 {
		t.Errorf("expected IDs 4 and 1, got %d and %d", filtered[0].ID, filtered[1].ID)
	}
}

func TestUserRepository_CreateInitialAdmin(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := newTestDB(t)
	repo := sqlite.NewUserRepository(db)

	admin := &domain.User{
		Username:     "rootadmin",
		PasswordHash: "$argon2id$dummyhash", // #nosec G101 -- test dummy hash
		Role:         domain.RoleAdmin,
		NtfyTopic:    "promo/user/rootadmin",
	}
	if err := repo.CreateInitialAdmin(ctx, admin); err != nil {
		t.Fatalf("CreateInitialAdmin failed: %v", err)
	}
	if admin.ID == 0 {
		t.Fatalf("expected non-zero admin ID")
	}

	second := &domain.User{
		Username:     "secondadmin",
		PasswordHash: "$argon2id$dummyhash2", // #nosec G101 -- test dummy hash
		Role:         domain.RoleAdmin,
		NtfyTopic:    "promo/user/secondadmin",
	}
	if err := repo.CreateInitialAdmin(ctx, second); !errors.Is(err, domain.ErrSetupAlreadyCompleted) {
		t.Fatalf("expected ErrSetupAlreadyCompleted on second CreateInitialAdmin, got %v", err)
	}

	count, err := repo.CountUsers(ctx)
	if err != nil {
		t.Fatalf("CountUsers failed: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 user after rejected second admin, got %d", count)
	}
}

func TestSessionRepository_DeleteUserSessions(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := newTestDB(t)
	userRepo := sqlite.NewUserRepository(db)
	sessionRepo := sqlite.NewSessionRepository(db)

	u1 := &domain.User{Username: "u1", PasswordHash: "p1", Role: domain.RoleUser, NtfyTopic: "t1"}
	u2 := &domain.User{Username: "u2", PasswordHash: "p2", Role: domain.RoleUser, NtfyTopic: "t2"}
	if err := userRepo.CreateUser(ctx, u1); err != nil {
		t.Fatalf("CreateUser u1 failed: %v", err)
	}
	if err := userRepo.CreateUser(ctx, u2); err != nil {
		t.Fatalf("CreateUser u2 failed: %v", err)
	}

	exp := time.Now().UTC().Add(time.Hour)
	if err := sessionRepo.CreateSession(ctx, "u1_tok1", u1.ID, exp); err != nil {
		t.Fatalf("CreateSession u1_tok1 failed: %v", err)
	}
	if err := sessionRepo.CreateSession(ctx, "u1_tok2", u1.ID, exp); err != nil {
		t.Fatalf("CreateSession u1_tok2 failed: %v", err)
	}
	if err := sessionRepo.CreateSession(ctx, "u2_tok1", u2.ID, exp); err != nil {
		t.Fatalf("CreateSession u2_tok1 failed: %v", err)
	}

	if err := sessionRepo.DeleteUserSessions(ctx, u1.ID); err != nil {
		t.Fatalf("DeleteUserSessions failed: %v", err)
	}

	if _, err := sessionRepo.GetSessionUser(ctx, "u1_tok1"); !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatalf("expected u1_tok1 revoked, got %v", err)
	}
	if _, err := sessionRepo.GetSessionUser(ctx, "u1_tok2"); !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatalf("expected u1_tok2 revoked, got %v", err)
	}
	if _, err := sessionRepo.GetSessionUser(ctx, "u2_tok1"); err != nil {
		t.Fatalf("expected u2_tok1 preserved, got %v", err)
	}
}

func TestChannelRepository_BatchMethods(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := newTestDB(t)
	repo := sqlite.NewChannelRepository(db)

	if err := repo.UpsertChannel(ctx, &domain.TelegramChannel{
		ChatID:          -1001,
		AccessHash:      111,
		Username:        "old_handle",
		Title:           "Old Title",
		MonitorProducts: true,
		MonitorCoupons:  true,
	}); err != nil {
		t.Fatalf("UpsertChannel seed failed: %v", err)
	}

	batch := []*domain.TelegramChannel{
		{
			ChatID:          -1001,
			AccessHash:      222,
			Username:        "new_handle",
			Title:           "New Title",
			MonitorProducts: false,
			MonitorCoupons:  false,
		},
		{
			ChatID:          -1002,
			AccessHash:      333,
			Username:        "second_chan",
			Title:           "Second Channel",
			MonitorProducts: false,
			MonitorCoupons:  false,
		},
	}

	if err := repo.UpsertChannelsBatch(ctx, batch); err != nil {
		t.Fatalf("UpsertChannelsBatch failed: %v", err)
	}

	ch1, err := repo.GetChannel(ctx, -1001)
	if err != nil {
		t.Fatalf("GetChannel -1001 failed: %v", err)
	}
	if ch1.Title != "New Title" || ch1.Username != "new_handle" || ch1.AccessHash != 222 {
		t.Fatalf("expected metadata updated, got %+v", ch1)
	}
	if !ch1.MonitorProducts || !ch1.MonitorCoupons {
		t.Fatalf("expected existing flags preserved on conflict, got %v/%v", ch1.MonitorProducts, ch1.MonitorCoupons)
	}

	if err := repo.UpdateChannelsFlagsBatch(ctx, []domain.ChannelFlagUpdate{
		{ChatID: -1001, MonitorProducts: false, MonitorCoupons: true},
		{ChatID: -1002, MonitorProducts: true, MonitorCoupons: false},
	}); err != nil {
		t.Fatalf("UpdateChannelsFlagsBatch failed: %v", err)
	}

	ch1, _ = repo.GetChannel(ctx, -1001)
	ch2, _ := repo.GetChannel(ctx, -1002)
	if ch1.MonitorProducts || !ch1.MonitorCoupons {
		t.Fatalf("unexpected flags for -1001: %v/%v", ch1.MonitorProducts, ch1.MonitorCoupons)
	}
	if !ch2.MonitorProducts || ch2.MonitorCoupons {
		t.Fatalf("unexpected flags for -1002: %v/%v", ch2.MonitorProducts, ch2.MonitorCoupons)
	}

	// Missing channel in batch must rollback entire transaction
	err = repo.UpdateChannelsFlagsBatch(ctx, []domain.ChannelFlagUpdate{
		{ChatID: -1001, MonitorProducts: true, MonitorCoupons: false},
		{ChatID: -999999, MonitorProducts: true, MonitorCoupons: true},
	})
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound on missing channel in batch, got %v", err)
	}

	ch1AfterRollback, _ := repo.GetChannel(ctx, -1001)
	if ch1AfterRollback.MonitorProducts || !ch1AfterRollback.MonitorCoupons {
		t.Fatalf("expected transaction rollback for -1001, got %v/%v", ch1AfterRollback.MonitorProducts, ch1AfterRollback.MonitorCoupons)
	}
}

func TestInterestRepository_CountActiveInterestsByUser(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := newTestDB(t)
	userRepo := sqlite.NewUserRepository(db)
	repo := sqlite.NewInterestRepository(db)

	u1 := &domain.User{Username: "cnt_u1", PasswordHash: "p1", Role: domain.RoleUser, NtfyTopic: "t1"}
	u2 := &domain.User{Username: "cnt_u2", PasswordHash: "p2", Role: domain.RoleUser, NtfyTopic: "t2"}
	if err := userRepo.CreateUser(ctx, u1); err != nil {
		t.Fatalf("CreateUser u1 failed: %v", err)
	}
	if err := userRepo.CreateUser(ctx, u2); err != nil {
		t.Fatalf("CreateUser u2 failed: %v", err)
	}

	items := []*domain.UserInterest{
		{UserID: u1.ID, RawTerm: "switch", PositiveTokens: []string{"switch"}, NegativeTokens: []string{}, Active: true},
		{UserID: u1.ID, RawTerm: "ps5", PositiveTokens: []string{"ps5"}, NegativeTokens: []string{}, Active: true},
		{UserID: u1.ID, RawTerm: "inactive", PositiveTokens: []string{"inactive"}, NegativeTokens: []string{}, Active: false},
		{UserID: u2.ID, RawTerm: "rtx", PositiveTokens: []string{"rtx"}, NegativeTokens: []string{}, Active: true},
	}
	for _, it := range items {
		if err := repo.CreateInterest(ctx, it); err != nil {
			t.Fatalf("CreateInterest failed: %v", err)
		}
	}

	counts, err := repo.CountActiveInterestsByUser(ctx)
	if err != nil {
		t.Fatalf("CountActiveInterestsByUser failed: %v", err)
	}
	if counts[u1.ID] != 2 {
		t.Fatalf("expected 2 active interests for u1, got %d", counts[u1.ID])
	}
	if counts[u2.ID] != 1 {
		t.Fatalf("expected 1 active interest for u2, got %d", counts[u2.ID])
	}
}

