package service_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/matrocheetos/telegram-monitor/internal/domain"
	"github.com/matrocheetos/telegram-monitor/internal/service"
)

type mockOfferMatchRepo struct {
	mu             sync.Mutex
	matchCalls     int
	totalCount     int64
	oldRecords     int64
	listResult     []domain.UserMatchedProduct
	listTotal      int
	listErr        error
	countErr       error
	pruneOldErr    error
	pruneExcessErr error
}

func (m *mockOfferMatchRepo) RecordProductMatch(ctx context.Context, userID int64, interestID *int64, channelID int64, price *float64, text string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.matchCalls++
	return nil
}

func (m *mockOfferMatchRepo) ListUserMatchedProducts(ctx context.Context, userID int64, searchQuery string, limit, offset int) ([]domain.UserMatchedProduct, int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.listErr != nil {
		return nil, 0, m.listErr
	}
	return m.listResult, m.listTotal, nil
}

func (m *mockOfferMatchRepo) PruneOlderThan(ctx context.Context, cutoff time.Time, batchSize int) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.pruneOldErr != nil {
		return 0, m.pruneOldErr
	}
	if m.oldRecords <= 0 {
		return 0, nil
	}
	deleted := int64(batchSize)
	if deleted > m.oldRecords {
		deleted = m.oldRecords
	}
	m.oldRecords -= deleted
	m.totalCount -= deleted
	return deleted, nil
}

func (m *mockOfferMatchRepo) PruneExcessAbove(ctx context.Context, maxKeep int64, batchSize int) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.pruneExcessErr != nil {
		return 0, m.pruneExcessErr
	}
	if m.totalCount <= maxKeep {
		return 0, nil
	}
	excess := m.totalCount - maxKeep
	deleted := int64(batchSize)
	if deleted > excess {
		deleted = excess
	}
	m.totalCount -= deleted
	return deleted, nil
}

func (m *mockOfferMatchRepo) CountTotal(ctx context.Context) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.countErr != nil {
		return 0, m.countErr
	}
	return m.totalCount, nil
}

type mockOfferCouponRepo struct {
	mu             sync.Mutex
	couponCalls    int
	totalCount     int64
	oldRecords     int64
	listResult     []domain.ExtractedCoupon
	listTotal      int
	listErr        error
	countErr       error
	pruneOldErr    error
	pruneExcessErr error
}

func (m *mockOfferCouponRepo) RecordCoupon(ctx context.Context, code, storeName, storeURL string, channelID int64, text string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.couponCalls++
	return nil
}

func (m *mockOfferCouponRepo) HasRecentCoupon(ctx context.Context, code, storeName string, since time.Time) (bool, error) {
	return false, nil
}

func (m *mockOfferCouponRepo) ListExtractedCoupons(ctx context.Context, searchQuery string, limit, offset int) ([]domain.ExtractedCoupon, int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.listErr != nil {
		return nil, 0, m.listErr
	}
	return m.listResult, m.listTotal, nil
}

func (m *mockOfferCouponRepo) PruneOlderThan(ctx context.Context, cutoff time.Time, batchSize int) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.pruneOldErr != nil {
		return 0, m.pruneOldErr
	}
	if m.oldRecords <= 0 {
		return 0, nil
	}
	deleted := int64(batchSize)
	if deleted > m.oldRecords {
		deleted = m.oldRecords
	}
	m.oldRecords -= deleted
	m.totalCount -= deleted
	return deleted, nil
}

func (m *mockOfferCouponRepo) PruneExcessAbove(ctx context.Context, maxKeep int64, batchSize int) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.pruneExcessErr != nil {
		return 0, m.pruneExcessErr
	}
	if m.totalCount <= maxKeep {
		return 0, nil
	}
	excess := m.totalCount - maxKeep
	deleted := int64(batchSize)
	if deleted > excess {
		deleted = excess
	}
	m.totalCount -= deleted
	return deleted, nil
}

func (m *mockOfferCouponRepo) CountTotal(ctx context.Context) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.countErr != nil {
		return 0, m.countErr
	}
	return m.totalCount, nil
}

func TestOfferService_ListMethods(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	matchRepo := &mockOfferMatchRepo{
		listResult: []domain.UserMatchedProduct{
			{ID: 1, UserID: 10, InterestTerm: "RTX 4060", MessageText: "Promo RTX 4060"},
		},
		listTotal: 1,
	}
	couponRepo := &mockOfferCouponRepo{
		listResult: []domain.ExtractedCoupon{
			{ID: 2, Code: "DESC10", StoreName: "Amazon"},
		},
		listTotal: 1,
	}

	svc := service.NewOfferService(matchRepo, couponRepo, service.OfferServiceConfig{})

	products, total, err := svc.ListUserMatchedProducts(ctx, 10, "RTX", 50, 0)
	if err != nil {
		t.Fatalf("ListUserMatchedProducts failed: %v", err)
	}
	if total != 1 || len(products) != 1 || products[0].InterestTerm != "RTX 4060" {
		t.Fatalf("unexpected products result: %+v", products)
	}

	coupons, cTotal, err := svc.ListExtractedCoupons(ctx, "DESC", 50, 0)
	if err != nil {
		t.Fatalf("ListExtractedCoupons failed: %v", err)
	}
	if cTotal != 1 || len(coupons) != 1 || coupons[0].Code != "DESC10" {
		t.Fatalf("unexpected coupons result: %+v", coupons)
	}
}

func TestOfferService_RunRetentionCleanup_TwoSteps(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	matchRepo := &mockOfferMatchRepo{
		totalCount: 35,
		oldRecords: 12,
	}
	couponRepo := &mockOfferCouponRepo{
		totalCount: 25,
		oldRecords: 8,
	}

	cfg := service.OfferServiceConfig{
		RetentionDays: 90,
		MaxKeep:       10,
		BatchSize:     5,
	}

	svc := service.NewOfferService(matchRepo, couponRepo, cfg)

	if err := svc.RunRetentionCleanup(ctx); err != nil {
		t.Fatalf("RunRetentionCleanup failed: %v", err)
	}

	// Verify old records pruned
	if matchRepo.oldRecords != 0 {
		t.Fatalf("expected all old match records to be pruned, got %d", matchRepo.oldRecords)
	}
	if couponRepo.oldRecords != 0 {
		t.Fatalf("expected all old coupon records to be pruned, got %d", couponRepo.oldRecords)
	}

	// Verify ceiling enforced
	if matchRepo.totalCount > cfg.MaxKeep {
		t.Fatalf("expected matchRepo total <= %d, got %d", cfg.MaxKeep, matchRepo.totalCount)
	}
	if couponRepo.totalCount > cfg.MaxKeep {
		t.Fatalf("expected couponRepo total <= %d, got %d", cfg.MaxKeep, couponRepo.totalCount)
	}
}

func TestOfferService_RunRetentionCleanup_ContextCancelled(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-cancelled

	matchRepo := &mockOfferMatchRepo{
		totalCount: 50,
		oldRecords: 20,
	}
	couponRepo := &mockOfferCouponRepo{}

	svc := service.NewOfferService(matchRepo, couponRepo, service.OfferServiceConfig{BatchSize: 5})

	err := svc.RunRetentionCleanup(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

func TestOfferService_StartRetentionJob_Lifecycle(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	matchRepo := &mockOfferMatchRepo{totalCount: 5}
	couponRepo := &mockOfferCouponRepo{totalCount: 5}

	svc := service.NewOfferService(matchRepo, couponRepo, service.OfferServiceConfig{})

	// Start job with 50ms ticker
	svc.StartRetentionJob(ctx, 50*time.Millisecond)

	// Second start is no-op
	svc.StartRetentionJob(ctx, 50*time.Millisecond)

	time.Sleep(120 * time.Millisecond)

	// Clean stop
	svc.Stop()

	// Calling Stop again is idempotent
	svc.Stop()
}

type mockCleanupSessionRepo struct {
	mu           sync.Mutex
	expiredCalls int
	err          error
}

func (m *mockCleanupSessionRepo) CreateSession(ctx context.Context, tokenHash string, userID int64, expiresAt time.Time) error {
	return nil
}

func (m *mockCleanupSessionRepo) GetSessionUser(ctx context.Context, tokenHash string) (*domain.User, error) {
	return nil, domain.ErrUnauthorized
}

func (m *mockCleanupSessionRepo) DeleteSession(ctx context.Context, tokenHash string) error {
	return nil
}

func (m *mockCleanupSessionRepo) DeleteUserSessions(ctx context.Context, userID int64) error {
	return nil
}

func (m *mockCleanupSessionRepo) DeleteExpiredSessions(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.expiredCalls++
	return m.err
}

func TestOfferService_RunRetentionCleanup_PrunesExpiredSessions(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	matchRepo := &mockOfferMatchRepo{totalCount: 1}
	couponRepo := &mockOfferCouponRepo{totalCount: 1}
	sessionRepo := &mockCleanupSessionRepo{}

	svc := service.NewOfferService(matchRepo, couponRepo, service.OfferServiceConfig{})
	svc.SetSessionRepository(sessionRepo)

	if err := svc.RunRetentionCleanup(ctx); err != nil {
		t.Fatalf("RunRetentionCleanup failed: %v", err)
	}

	sessionRepo.mu.Lock()
	calls := sessionRepo.expiredCalls
	sessionRepo.mu.Unlock()
	if calls != 1 {
		t.Fatalf("expected 1 DeleteExpiredSessions call, got %d", calls)
	}

	expectedErr := errors.New("db failure")
	sessionRepo.mu.Lock()
	sessionRepo.err = expectedErr
	sessionRepo.mu.Unlock()

	err := svc.RunRetentionCleanup(ctx)
	if !errors.Is(err, expectedErr) {
		t.Fatalf("expected wrapped session prune error, got %v", err)
	}
}
