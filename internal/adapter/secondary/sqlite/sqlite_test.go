package sqlite_test

import (
	"context"
	"database/sql"
	"errors"
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
