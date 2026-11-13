package service_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/matrocheetos/telegram-monitor/internal/domain"
	"github.com/matrocheetos/telegram-monitor/internal/service"
)

type mockUserRepo struct {
	mu    sync.Mutex
	users []*domain.User
}

func (m *mockUserRepo) CountUsers(ctx context.Context) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.users), nil
}

func (m *mockUserRepo) CreateUser(ctx context.Context, u *domain.User) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	u.ID = int64(len(m.users) + 1)
	m.users = append(m.users, u)
	return nil
}

func (m *mockUserRepo) CreateInitialAdmin(ctx context.Context, u *domain.User) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.users) > 0 {
		return domain.ErrSetupAlreadyCompleted
	}
	u.ID = 1
	m.users = append(m.users, u)
	return nil
}

func (m *mockUserRepo) GetUserByID(ctx context.Context, id int64) (*domain.User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, u := range m.users {
		if u.ID == id {
			return u, nil
		}
	}
	return nil, domain.ErrNotFound
}

func (m *mockUserRepo) GetUserByUsername(ctx context.Context, username string) (*domain.User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, u := range m.users {
		if u.Username == username {
			return u, nil
		}
	}
	return nil, domain.ErrNotFound
}

func (m *mockUserRepo) ListUsers(ctx context.Context) ([]*domain.User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make([]*domain.User, len(m.users))
	copy(cp, m.users)
	return cp, nil
}

func (m *mockUserRepo) UpdatePassword(ctx context.Context, id int64, passwordHash string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, u := range m.users {
		if u.ID == id {
			u.PasswordHash = passwordHash
			return nil
		}
	}
	return domain.ErrNotFound
}

func (m *mockUserRepo) DeleteUser(ctx context.Context, id int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, u := range m.users {
		if u.ID == id {
			m.users = append(m.users[:i], m.users[i+1:]...)
			return nil
		}
	}
	return domain.ErrNotFound
}

type mockInterestRepo struct {
	mu        sync.Mutex
	interests []*domain.UserInterest
}

func (m *mockInterestRepo) CreateInterest(ctx context.Context, interest *domain.UserInterest) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	interest.ID = int64(len(m.interests) + 1)
	m.interests = append(m.interests, interest)
	return nil
}

func (m *mockInterestRepo) ListInterestsByUserID(ctx context.Context, userID int64) ([]*domain.UserInterest, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var res []*domain.UserInterest
	for _, it := range m.interests {
		if it.UserID == userID {
			res = append(res, it)
		}
	}
	return res, nil
}

func (m *mockInterestRepo) ListAllActiveInterests(ctx context.Context) ([]*domain.UserInterest, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var res []*domain.UserInterest
	for _, it := range m.interests {
		if it.Active {
			res = append(res, it)
		}
	}
	return res, nil
}

func (m *mockInterestRepo) DeleteInterest(ctx context.Context, userID, id int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, it := range m.interests {
		if it.ID == id && it.UserID == userID {
			m.interests = append(m.interests[:i], m.interests[i+1:]...)
			return nil
		}
	}
	return domain.ErrNotFound
}

func (m *mockInterestRepo) CountActiveInterestsByUser(ctx context.Context) (map[int64]int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	counts := make(map[int64]int)
	for _, it := range m.interests {
		if it.Active {
			counts[it.UserID]++
		}
	}
	return counts, nil
}

type mockChannelRepo struct {
	mu       sync.Mutex
	channels map[int64]*domain.TelegramChannel
}

func newMockChannelRepo() *mockChannelRepo {
	return &mockChannelRepo{channels: make(map[int64]*domain.TelegramChannel)}
}

func (m *mockChannelRepo) UpsertChannel(ctx context.Context, ch *domain.TelegramChannel) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.channels[ch.ChatID] = ch
	return nil
}

func (m *mockChannelRepo) UpsertChannelsBatch(ctx context.Context, channels []*domain.TelegramChannel) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, ch := range channels {
		if ch == nil {
			continue
		}
		if existing, ok := m.channels[ch.ChatID]; ok {
			ch.MonitorProducts = existing.MonitorProducts
			ch.MonitorCoupons = existing.MonitorCoupons
		}
		m.channels[ch.ChatID] = ch
	}
	return nil
}

func (m *mockChannelRepo) GetChannel(ctx context.Context, chatID int64) (*domain.TelegramChannel, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	ch, ok := m.channels[chatID]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return ch, nil
}

func (m *mockChannelRepo) ListChannels(ctx context.Context) ([]*domain.TelegramChannel, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	res := make([]*domain.TelegramChannel, 0, len(m.channels))
	for _, ch := range m.channels {
		res = append(res, ch)
	}
	return res, nil
}

func (m *mockChannelRepo) UpdateChannelFlags(ctx context.Context, chatID int64, monitorProducts, monitorCoupons bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	ch, ok := m.channels[chatID]
	if !ok {
		return domain.ErrNotFound
	}
	ch.MonitorProducts = monitorProducts
	ch.MonitorCoupons = monitorCoupons
	return nil
}

func (m *mockChannelRepo) UpdateChannelsFlagsBatch(ctx context.Context, updates []domain.ChannelFlagUpdate) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, u := range updates {
		if _, ok := m.channels[u.ChatID]; !ok {
			return domain.ErrNotFound
		}
	}
	for _, u := range updates {
		ch := m.channels[u.ChatID]
		ch.MonitorProducts = u.MonitorProducts
		ch.MonitorCoupons = u.MonitorCoupons
	}
	return nil
}

type mockMatchRepo struct {
	mu      sync.Mutex
	matches []string
}

func (m *mockMatchRepo) RecordProductMatch(ctx context.Context, userID int64, interestID *int64, channelID int64, price *float64, text string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.matches = append(m.matches, text)
	return nil
}

func (m *mockMatchRepo) ListUserMatchedProducts(ctx context.Context, userID int64, searchQuery string, limit, offset int) ([]domain.UserMatchedProduct, int, error) {
	return nil, 0, nil
}

func (m *mockMatchRepo) PruneOlderThan(ctx context.Context, cutoff time.Time, batchSize int) (int64, error) {
	return 0, nil
}

func (m *mockMatchRepo) PruneExcessAbove(ctx context.Context, maxKeep int64, batchSize int) (int64, error) {
	return 0, nil
}

func (m *mockMatchRepo) CountTotal(ctx context.Context) (int64, error) {
	return 0, nil
}

type mockCouponRepo struct {
	mu      sync.Mutex
	coupons []domain.Coupon
}

func (m *mockCouponRepo) RecordCoupon(ctx context.Context, code, storeName, storeURL string, channelID int64, text string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.coupons = append(m.coupons, domain.Coupon{
		Code:      code,
		StoreName: storeName,
		StoreURL:  storeURL,
		CreatedAt: time.Now().UTC(),
	})
	return nil
}

func (m *mockCouponRepo) HasRecentCoupon(ctx context.Context, code, storeName string, since time.Time) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, c := range m.coupons {
		if strings.EqualFold(c.Code, code) && strings.EqualFold(c.StoreName, storeName) && !c.CreatedAt.Before(since) {
			return true, nil
		}
	}
	return false, nil
}

func (m *mockCouponRepo) ListExtractedCoupons(ctx context.Context, searchQuery string, limit, offset int) ([]domain.ExtractedCoupon, int, error) {
	return nil, 0, nil
}

func (m *mockCouponRepo) PruneOlderThan(ctx context.Context, cutoff time.Time, batchSize int) (int64, error) {
	return 0, nil
}

func (m *mockCouponRepo) PruneExcessAbove(ctx context.Context, maxKeep int64, batchSize int) (int64, error) {
	return 0, nil
}

func (m *mockCouponRepo) CountTotal(ctx context.Context) (int64, error) {
	return 0, nil
}

type mockNotificationGateway struct {
	mu           sync.Mutex
	productNotifs []string
	couponNotifs  []domain.Coupon
}

func (m *mockNotificationGateway) SendProductNotification(ctx context.Context, username, offerURL string, price *float64, text string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.productNotifs = append(m.productNotifs, fmt.Sprintf("%s:%s", username, offerURL))
	return nil
}

func (m *mockNotificationGateway) SendCouponNotification(ctx context.Context, coupon domain.Coupon) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.couponNotifs = append(m.couponNotifs, coupon)
	return nil
}

func TestMonitorService_ProductMatch(t *testing.T) {
	t.Parallel()

	userRepo := &mockUserRepo{
		users: []*domain.User{
			{ID: 1, Username: "mateus", NtfyTopic: "promo/user/mateus"},
		},
	}
	maxPrice := 2000.0
	interest, err := domain.NewUserInterest(1, "RTX 4060 -ti", &maxPrice)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	interest.ID = 10

	interestRepo := &mockInterestRepo{
		interests: []*domain.UserInterest{interest},
	}

	channelRepo := newMockChannelRepo()
	_ = channelRepo.UpsertChannel(context.Background(), &domain.TelegramChannel{
		ChatID:          100,
		Title:           "Hardware Deals",
		MonitorProducts: true,
		MonitorCoupons:  false,
	})

	matchRepo := &mockMatchRepo{}
	couponRepo := &mockCouponRepo{}
	gateway := &mockNotificationGateway{}

	svc := service.NewMonitorService(
		userRepo,
		interestRepo,
		channelRepo,
		matchRepo,
		couponRepo,
		gateway,
		service.MonitorConfig{WorkerCount: 2, QueueSize: 64},
	)

	ctx := context.Background()
	if err := svc.RebuildSnapshot(ctx); err != nil {
		t.Fatalf("rebuild snapshot failed: %v", err)
	}

	svc.Start(ctx)
	defer svc.Stop()

	// 1. Should match: RTX 4060, price 1899 <= 2000, no 'ti'
	msg1 := "Super oferta: Placa de Video RTX 4060 8GB por apenas R$ 1.899,00! Confira em https://loja.com/rtx"
	if err := svc.ProcessIncomingMessage(ctx, 100, msg1); err != nil {
		t.Fatalf("process message failed: %v", err)
	}

	// 2. Should NOT match: contains negative token 'ti'
	msg2 := "Oferta RTX 4060 Ti por R$ 1.950,00 https://loja.com/ti"
	if err := svc.ProcessIncomingMessage(ctx, 100, msg2); err != nil {
		t.Fatalf("process message failed: %v", err)
	}

	// 3. Should NOT match: price 2500 > 2000
	msg3 := "Oferta RTX 4060 por R$ 2.500,00 https://loja.com/high"
	if err := svc.ProcessIncomingMessage(ctx, 100, msg3); err != nil {
		t.Fatalf("process message failed: %v", err)
	}

	// 4. Unmonitored channel
	msg4 := "Placa RTX 4060 por R$ 1.000,00"
	if err := svc.ProcessIncomingMessage(ctx, 999, msg4); err != nil {
		t.Fatalf("process message failed: %v", err)
	}

	// Allow workers to process queue
	time.Sleep(100 * time.Millisecond)

	gateway.mu.Lock()
	defer gateway.mu.Unlock()

	if len(gateway.productNotifs) != 1 {
		t.Fatalf("expected 1 product notification, got %d", len(gateway.productNotifs))
	}

	expectedPrefix := "mateus:https://loja.com/rtx"
	if gateway.productNotifs[0] != expectedPrefix {
		t.Errorf("expected notif %q, got %q", expectedPrefix, gateway.productNotifs[0])
	}

	matchRepo.mu.Lock()
	defer matchRepo.mu.Unlock()
	if len(matchRepo.matches) != 1 {
		t.Fatalf("expected 1 recorded match, got %d", len(matchRepo.matches))
	}
}

func TestMonitorService_CouponMatch(t *testing.T) {
	t.Parallel()

	userRepo := &mockUserRepo{}
	interestRepo := &mockInterestRepo{}
	channelRepo := newMockChannelRepo()
	_ = channelRepo.UpsertChannel(context.Background(), &domain.TelegramChannel{
		ChatID:          200,
		Title:           "Cupons Brasil",
		MonitorProducts: false,
		MonitorCoupons:  true,
	})

	matchRepo := &mockMatchRepo{}
	couponRepo := &mockCouponRepo{}
	gateway := &mockNotificationGateway{}

	svc := service.NewMonitorService(
		userRepo,
		interestRepo,
		channelRepo,
		matchRepo,
		couponRepo,
		gateway,
		service.MonitorConfig{WorkerCount: 2, QueueSize: 64},
	)

	ctx := context.Background()
	if err := svc.RebuildSnapshot(ctx); err != nil {
		t.Fatalf("rebuild snapshot: %v", err)
	}

	svc.Start(ctx)
	defer svc.Stop()

	msg := "🎟️ Cupom Kabum: TECH10 no site https://www.kabum.com.br/oferta"
	if err := svc.ProcessIncomingMessage(ctx, 200, msg); err != nil {
		t.Fatalf("process message failed: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	gateway.mu.Lock()
	defer gateway.mu.Unlock()
	if len(gateway.couponNotifs) != 1 {
		t.Fatalf("expected 1 coupon notif, got %d", len(gateway.couponNotifs))
	}
	if gateway.couponNotifs[0].Code != "TECH10" {
		t.Errorf("expected code TECH10, got %s", gateway.couponNotifs[0].Code)
	}

	couponRepo.mu.Lock()
	defer couponRepo.mu.Unlock()
	if len(couponRepo.coupons) != 1 {
		t.Fatalf("expected 1 recorded coupon, got %d", len(couponRepo.coupons))
	}
}

func TestMonitorService_Concurrency_RebuildAndProcess(t *testing.T) {
	t.Parallel()

	userRepo := &mockUserRepo{
		users: []*domain.User{
			{ID: 1, Username: "user1", NtfyTopic: "promo/user/user1"},
			{ID: 2, Username: "user2", NtfyTopic: "promo/user/user2"},
		},
	}

	interestRepo := &mockInterestRepo{
		interests: []*domain.UserInterest{
			{ID: 1, UserID: 1, RawTerm: "notebook", PositiveTokens: []string{"notebook"}, Active: true},
			{ID: 2, UserID: 2, RawTerm: "iphone", PositiveTokens: []string{"iphone"}, Active: true},
		},
	}

	channelRepo := newMockChannelRepo()
	_ = channelRepo.UpsertChannel(context.Background(), &domain.TelegramChannel{
		ChatID:          300,
		Title:           "Concurrence Channel",
		MonitorProducts: true,
		MonitorCoupons:  true,
	})

	matchRepo := &mockMatchRepo{}
	couponRepo := &mockCouponRepo{}
	gateway := &mockNotificationGateway{}

	svc := service.NewMonitorService(
		userRepo,
		interestRepo,
		channelRepo,
		matchRepo,
		couponRepo,
		gateway,
		service.MonitorConfig{WorkerCount: 8, QueueSize: 10000},
	)

	ctx := context.Background()
	_ = svc.RebuildSnapshot(ctx)
	svc.Start(ctx)
	defer svc.Stop()

	var wg sync.WaitGroup
	var processedCount atomic.Int64

	const messageWorkers = 8
	const messagesPerWorker = 300

	// Launch concurrent message producers
	for w := 0; w < messageWorkers; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for i := 0; i < messagesPerWorker; i++ {
				text := fmt.Sprintf("Promoção de Notebook Gamer Dell %d por R$ 3.500,00 https://dell.com/%d", i, i)
				if i%2 == 0 {
					text = fmt.Sprintf("🎟️ Cupom Dell: NOTE%d em https://dell.com", i)
				}
				_ = svc.ProcessIncomingMessage(ctx, 300, text)
				processedCount.Add(1)
			}
		}(w)
	}

	// Launch concurrent snapshot rebuilders
	stopRebuilders := make(chan struct{})
	var rebuilderWg sync.WaitGroup
	for r := 0; r < 3; r++ {
		rebuilderWg.Add(1)
		go func(rebuilderID int) {
			defer rebuilderWg.Done()
			ticker := time.NewTicker(2 * time.Millisecond)
			defer ticker.Stop()
			for {
				select {
				case <-stopRebuilders:
					return
				case <-ticker.C:
					_ = svc.RebuildSnapshot(ctx)
				}
			}
		}(r)
	}

	wg.Wait()
	close(stopRebuilders)
	rebuilderWg.Wait()

	if processedCount.Load() != messageWorkers*messagesPerWorker {
		t.Fatalf("expected %d messages processed, got %d", messageWorkers*messagesPerWorker, processedCount.Load())
	}
}

func TestMonitorService_CouponDeduplication(t *testing.T) {
	t.Parallel()

	userRepo := &mockUserRepo{}
	interestRepo := &mockInterestRepo{}
	channelRepo := newMockChannelRepo()
	_ = channelRepo.UpsertChannel(context.Background(), &domain.TelegramChannel{
		ChatID:         400,
		Title:          "Cupons Canal 1",
		MonitorCoupons: true,
	})
	_ = channelRepo.UpsertChannel(context.Background(), &domain.TelegramChannel{
		ChatID:         401,
		Title:          "Cupons Canal 2",
		MonitorCoupons: true,
	})

	matchRepo := &mockMatchRepo{}
	couponRepo := &mockCouponRepo{}
	gateway := &mockNotificationGateway{}

	// Configure a short dedup window for testing expiry: 100ms
	cfg := service.MonitorConfig{
		WorkerCount:       4,
		QueueSize:         128,
		CouponDedupWindow: 100 * time.Millisecond,
	}

	svc := service.NewMonitorService(
		userRepo,
		interestRepo,
		channelRepo,
		matchRepo,
		couponRepo,
		gateway,
		cfg,
	)

	ctx := context.Background()
	_ = svc.RebuildSnapshot(ctx)
	svc.Start(ctx)
	defer svc.Stop()

	// 1. Send coupon for Amazon multiple times across different channels
	msg1 := "🎟️ Cupom: DESCONTO10 na Amazon https://www.amazon.com.br/deal"
	_ = svc.ProcessIncomingMessage(ctx, 400, msg1)
	_ = svc.ProcessIncomingMessage(ctx, 401, msg1)
	_ = svc.ProcessIncomingMessage(ctx, 400, msg1)

	// 2. Send same coupon code for a different store (Mercado Livre)
	msg2 := "🎟️ Cupom: DESCONTO10 no Mercado Livre https://www.mercadolivre.com.br/oferta"
	_ = svc.ProcessIncomingMessage(ctx, 400, msg2)

	// Wait for worker queue to settle
	time.Sleep(50 * time.Millisecond)

	couponRepo.mu.Lock()
	gateway.mu.Lock()

	if len(couponRepo.coupons) != 2 {
		t.Fatalf("expected exactly 2 coupons recorded (Amazon + ML), got %d: %+v", len(couponRepo.coupons), couponRepo.coupons)
	}
	if len(gateway.couponNotifs) != 2 {
		t.Fatalf("expected exactly 2 coupon notifications (Amazon + ML), got %d: %+v", len(gateway.couponNotifs), gateway.couponNotifs)
	}

	gateway.mu.Unlock()
	couponRepo.mu.Unlock()

	// 3. Wait for the dedup window (100ms) to expire
	time.Sleep(120 * time.Millisecond)

	// Send the Amazon coupon again after window has expired
	_ = svc.ProcessIncomingMessage(ctx, 400, msg1)
	time.Sleep(50 * time.Millisecond)

	couponRepo.mu.Lock()
	gateway.mu.Lock()

	if len(couponRepo.coupons) != 3 {
		t.Fatalf("expected 3 coupons after window expiry, got %d", len(couponRepo.coupons))
	}
	if len(gateway.couponNotifs) != 3 {
		t.Fatalf("expected 3 notifications after window expiry, got %d", len(gateway.couponNotifs))
	}

	gateway.mu.Unlock()
	couponRepo.mu.Unlock()
}

func TestMonitorService_CouponDeduplication_ConcurrentBurst(t *testing.T) {
	t.Parallel()

	userRepo := &mockUserRepo{}
	interestRepo := &mockInterestRepo{}
	channelRepo := newMockChannelRepo()
	_ = channelRepo.UpsertChannel(context.Background(), &domain.TelegramChannel{
		ChatID:         500,
		Title:          "Burst Channel",
		MonitorCoupons: true,
	})

	matchRepo := &mockMatchRepo{}
	couponRepo := &mockCouponRepo{}
	gateway := &mockNotificationGateway{}

	cfg := service.MonitorConfig{
		WorkerCount:       8,
		QueueSize:         1024,
		CouponDedupWindow: 4 * time.Hour,
	}

	svc := service.NewMonitorService(
		userRepo,
		interestRepo,
		channelRepo,
		matchRepo,
		couponRepo,
		gateway,
		cfg,
	)

	ctx := context.Background()
	_ = svc.RebuildSnapshot(ctx)
	svc.Start(ctx)
	defer svc.Stop()

	// 20 concurrent goroutines firing the exact same coupon at the exact same instant
	var wg sync.WaitGroup
	const concurrentCouriers = 20
	msg := "🎟️ Super Cupom Shopee: SHOPEE20 no site https://shopee.com.br/deal"

	for i := 0; i < concurrentCouriers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = svc.ProcessIncomingMessage(ctx, 500, msg)
		}()
	}
	wg.Wait()

	// Wait for worker pool to finish processing
	time.Sleep(100 * time.Millisecond)

	couponRepo.mu.Lock()
	defer couponRepo.mu.Unlock()
	gateway.mu.Lock()
	defer gateway.mu.Unlock()

	if len(couponRepo.coupons) != 1 {
		t.Fatalf("expected exactly 1 coupon recorded in burst, got %d", len(couponRepo.coupons))
	}
	if len(gateway.couponNotifs) != 1 {
		t.Fatalf("expected exactly 1 coupon notification in burst, got %d", len(gateway.couponNotifs))
	}
}

type mockDebugMessageRepo struct {
	mu      sync.Mutex
	records []*domain.AnalyzedMessageDebugRecord
}

func (m *mockDebugMessageRepo) RecordAnalyzedMessage(ctx context.Context, rec *domain.AnalyzedMessageDebugRecord) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.records = append(m.records, rec)
	return nil
}

func (m *mockDebugMessageRepo) CountAnalyzedMessages(ctx context.Context) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return int64(len(m.records)), nil
}

func (m *mockDebugMessageRepo) PruneOldestDebugMessages(ctx context.Context, maxKeep int64, batchSize int) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if int64(len(m.records)) <= maxKeep {
		return 0, nil
	}
	excess := int64(len(m.records)) - maxKeep
	if excess > int64(batchSize) {
		excess = int64(batchSize)
	}
	m.records = m.records[excess:]
	return excess, nil
}

func TestMonitorService_DebugCollectionDisabledByDefault(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	userRepo := &mockUserRepo{
		users: []*domain.User{
			{ID: 1, Username: "alice", NtfyTopic: "promo-alice"},
		},
	}
	interestRepo := &mockInterestRepo{
		interests: []*domain.UserInterest{
			{
				ID:             1,
				UserID:         1,
				RawTerm:        "nintendo switch",
				PositiveTokens: []string{"nintendo", "switch"},
				Active:         true,
			},
		},
	}
	channelRepo := &mockChannelRepo{
		channels: map[int64]*domain.TelegramChannel{
			-1001: {ChatID: -1001, Title: "Deals", MonitorProducts: true, MonitorCoupons: true},
		},
	}
	matchRepo := &mockMatchRepo{}
	couponRepo := &mockCouponRepo{}
	gateway := &mockNotificationGateway{}

	svc := service.NewMonitorService(
		userRepo,
		interestRepo,
		channelRepo,
		matchRepo,
		couponRepo,
		gateway,
		service.MonitorConfig{WorkerCount: 2, QueueSize: 100},
	)

	if svc.DebugRepository() != nil {
		t.Fatalf("expected DebugRepository to be nil by default")
	}

	_ = svc.RebuildSnapshot(ctx)
	svc.Start(ctx)
	defer svc.Stop()

	// Send message
	_ = svc.ProcessIncomingMessage(ctx, -1001, "Console Nintendo Switch OLED por R$ 1.999 no link https://store.com")
	time.Sleep(50 * time.Millisecond)

	matchRepo.mu.Lock()
	defer matchRepo.mu.Unlock()
	if len(matchRepo.matches) != 1 {
		t.Fatalf("expected 1 product match, got %d", len(matchRepo.matches))
	}
}

func TestMonitorService_DebugCollectionOutcomes(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	maxPrice := 2500.0
	userRepo := &mockUserRepo{
		users: []*domain.User{
			{ID: 1, Username: "alice", NtfyTopic: "promo-alice"},
		},
	}
	interestRepo := &mockInterestRepo{
		interests: []*domain.UserInterest{
			{
				ID:             1,
				UserID:         1,
				RawTerm:        "nintendo switch",
				PositiveTokens: []string{"nintendo", "switch"},
				MaxPrice:       &maxPrice,
				Active:         true,
			},
		},
	}
	channelRepo := &mockChannelRepo{
		channels: map[int64]*domain.TelegramChannel{
			-1001: {ChatID: -1001, Title: "All Monitors", Username: "allmonitors", MonitorProducts: true, MonitorCoupons: true},
			-1002: {ChatID: -1002, Title: "Products Only", Username: "prodsonly", MonitorProducts: true, MonitorCoupons: false},
			-1003: {ChatID: -1003, Title: "Coupons Only", Username: "couponsonly", MonitorProducts: false, MonitorCoupons: true},
		},
	}
	matchRepo := &mockMatchRepo{}
	couponRepo := &mockCouponRepo{}
	gateway := &mockNotificationGateway{}
	debugRepo := &mockDebugMessageRepo{}

	svc := service.NewMonitorService(
		userRepo,
		interestRepo,
		channelRepo,
		matchRepo,
		couponRepo,
		gateway,
		service.MonitorConfig{WorkerCount: 4, QueueSize: 500},
	)
	svc.SetDebugRepository(debugRepo)

	_ = svc.RebuildSnapshot(ctx)
	svc.Start(ctx)
	defer svc.Stop()

	tests := []struct {
		name            string
		msg             domain.IncomingTelegramMessage
		expectedOutcome domain.EvaluationOutcome
		checkRecord     func(t *testing.T, rec *domain.AnalyzedMessageDebugRecord)
	}{
		{
			name: "product match only",
			msg: domain.IncomingTelegramMessage{
				ChannelID:         -1001,
				TelegramMessageID: 1,
				Text:              "Oferta Nintendo Switch por R$ 2.199 https://amzn.to/sw",
			},
			expectedOutcome: domain.OutcomeMatchedProduct,
			checkRecord: func(t *testing.T, rec *domain.AnalyzedMessageDebugRecord) {
				if rec.MatchedProductsCount != 1 {
					t.Errorf("expected 1 matched product, got %d", rec.MatchedProductsCount)
				}
				if rec.ExtractedLowestPrice == nil || *rec.ExtractedLowestPrice != 2199.0 {
					t.Errorf("expected lowest price 2199.0, got %v", rec.ExtractedLowestPrice)
				}
				if rec.FirstURL != "https://amzn.to/sw" {
					t.Errorf("expected first url 'https://amzn.to/sw', got %q", rec.FirstURL)
				}
			},
		},
		{
			name: "coupon extracted only",
			msg: domain.IncomingTelegramMessage{
				ChannelID:         -1001,
				TelegramMessageID: 2,
				Text:              "Cupom Shopee: DESCONTO10 no link https://shopee.com.br",
			},
			expectedOutcome: domain.OutcomeExtractedCoupon,
			checkRecord: func(t *testing.T, rec *domain.AnalyzedMessageDebugRecord) {
				if rec.ExtractedCouponsCount != 1 {
					t.Errorf("expected 1 extracted coupon, got %d", rec.ExtractedCouponsCount)
				}
				if len(rec.ExtractedCoupons) > 0 && rec.ExtractedCoupons[0].Code != "DESCONTO10" {
					t.Errorf("expected code DESCONTO10, got %q", rec.ExtractedCoupons[0].Code)
				}
			},
		},
		{
			name: "matched product and coupon",
			msg: domain.IncomingTelegramMessage{
				ChannelID:         -1001,
				TelegramMessageID: 3,
				Text:              "Console Nintendo Switch por R$ 1.999 usando cupom: GAMES100 na Amazon https://amazon.com.br/dp/switch",
			},
			expectedOutcome: domain.OutcomeMatchedProductAndCoupon,
			checkRecord: func(t *testing.T, rec *domain.AnalyzedMessageDebugRecord) {
				if rec.MatchedProductsCount != 1 || rec.ExtractedCouponsCount != 1 {
					t.Errorf("expected 1 match and 1 coupon, got %d matches and %d coupons", rec.MatchedProductsCount, rec.ExtractedCouponsCount)
				}
			},
		},
		{
			name: "coupon indicator but no code extracted",
			msg: domain.IncomingTelegramMessage{
				ChannelID:         -1001,
				TelegramMessageID: 4,
				Text:              "Aproveite o cupom no carrinho para garantir o melhor preço!",
			},
			expectedOutcome: domain.OutcomeCouponIndicatorNoCode,
			checkRecord: func(t *testing.T, rec *domain.AnalyzedMessageDebugRecord) {
				if !rec.HasCouponIndicator {
					t.Errorf("expected HasCouponIndicator = true")
				}
				if rec.ExtractedCouponsCount != 0 {
					t.Errorf("expected ExtractedCouponsCount = 0")
				}
			},
		},
		{
			name: "monitored channel no match",
			msg: domain.IncomingTelegramMessage{
				ChannelID:         -1001,
				TelegramMessageID: 5,
				Text:              "Smartphone Galaxy S24 por R$ 4.299 no PIX https://samsung.com",
			},
			expectedOutcome: domain.OutcomeMonitoredNoMatch,
			checkRecord: func(t *testing.T, rec *domain.AnalyzedMessageDebugRecord) {
				if rec.MatchedProductsCount != 0 || rec.ExtractedCouponsCount != 0 {
					t.Errorf("expected 0 matches and coupons")
				}
			},
		},
		{
			name: "unmonitored channel",
			msg: domain.IncomingTelegramMessage{
				ChannelID:         -9999, // Unmonitored
				TelegramMessageID: 6,
				Text:              "Promoção em canal não monitorado",
			},
			expectedOutcome: domain.OutcomeUnmonitoredChannel,
			checkRecord: func(t *testing.T, rec *domain.AnalyzedMessageDebugRecord) {
				if rec.MonitorProducts || rec.MonitorCoupons {
					t.Errorf("expected channel flags to be false")
				}
			},
		},
		{
			name: "empty text",
			msg: domain.IncomingTelegramMessage{
				ChannelID:         -1001,
				TelegramMessageID: 7,
				Text:              "   ",
			},
			expectedOutcome: domain.OutcomeEmptyText,
			checkRecord: func(t *testing.T, rec *domain.AnalyzedMessageDebugRecord) {
				if rec.RawText != "   " {
					t.Errorf("expected raw text '   ', got %q", rec.RawText)
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			beforeCount := len(debugRepo.records)
			if err := svc.ProcessIncomingTelegramMessage(ctx, tc.msg); err != nil {
				t.Fatalf("ProcessIncomingTelegramMessage failed: %v", err)
			}

			// Wait briefly for async worker to record
			deadline := time.Now().Add(500 * time.Millisecond)
			for time.Now().Before(deadline) {
				debugRepo.mu.Lock()
				n := len(debugRepo.records)
				debugRepo.mu.Unlock()
				if n > beforeCount {
					break
				}
				time.Sleep(10 * time.Millisecond)
			}

			debugRepo.mu.Lock()
			defer debugRepo.mu.Unlock()
			if len(debugRepo.records) <= beforeCount {
				t.Fatalf("expected new record in debug repository, count stayed at %d", beforeCount)
			}
			last := debugRepo.records[len(debugRepo.records)-1]
			if last.EvaluationOutcome != tc.expectedOutcome {
				t.Errorf("expected outcome %q, got %q", tc.expectedOutcome, last.EvaluationOutcome)
			}
			if tc.checkRecord != nil {
				tc.checkRecord(t, last)
			}
		})
	}
}

func TestMonitorService_DebugCollectionConcurrentBurst(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	channelRepo := &mockChannelRepo{
		channels: map[int64]*domain.TelegramChannel{
			-1001: {ChatID: -1001, Title: "Deals", MonitorProducts: true, MonitorCoupons: true},
		},
	}
	debugRepo := &mockDebugMessageRepo{}

	svc := service.NewMonitorService(
		&mockUserRepo{},
		&mockInterestRepo{},
		channelRepo,
		&mockMatchRepo{},
		&mockCouponRepo{},
		&mockNotificationGateway{},
		service.MonitorConfig{WorkerCount: 8, QueueSize: 500},
	)
	svc.SetDebugRepository(debugRepo)

	_ = svc.RebuildSnapshot(ctx)
	svc.Start(ctx)
	defer svc.Stop()

	const burstSize = 50
	var wg sync.WaitGroup

	for i := 0; i < burstSize; i++ {
		wg.Add(1)
		msgID := i
		go func() {
			defer wg.Done()
			msg := domain.IncomingTelegramMessage{
				ChannelID:         -1001,
				TelegramMessageID: msgID,
				Text:              fmt.Sprintf("Oferta teste %d cupom CUPOM%d https://store.com/%d", msgID, msgID, msgID),
			}
			_ = svc.ProcessIncomingTelegramMessage(ctx, msg)
		}()
	}
	wg.Wait()

	// Wait for queue flush
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		debugRepo.mu.Lock()
		count := len(debugRepo.records)
		debugRepo.mu.Unlock()
		if count == burstSize {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	debugRepo.mu.Lock()
	defer debugRepo.mu.Unlock()
	if len(debugRepo.records) != burstSize {
		t.Fatalf("expected %d debug records recorded in burst, got %d", burstSize, len(debugRepo.records))
	}
}

type slowMatchRepo struct {
	mu      sync.Mutex
	delay   time.Duration
	matches []string
}

func (m *slowMatchRepo) RecordProductMatch(ctx context.Context, userID int64, interestID *int64, channelID int64, price *float64, text string) error {
	select {
	case <-time.After(m.delay):
	case <-ctx.Done():
		return ctx.Err()
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.matches = append(m.matches, text)
	return nil
}

func (m *slowMatchRepo) ListUserMatchedProducts(ctx context.Context, userID int64, searchQuery string, limit, offset int) ([]domain.UserMatchedProduct, int, error) {
	return nil, 0, nil
}

func (m *slowMatchRepo) PruneOlderThan(ctx context.Context, cutoff time.Time, batchSize int) (int64, error) {
	return 0, nil
}

func (m *slowMatchRepo) PruneExcessAbove(ctx context.Context, maxKeep int64, batchSize int) (int64, error) {
	return 0, nil
}

func (m *slowMatchRepo) CountTotal(ctx context.Context) (int64, error) {
	return 0, nil
}

func TestMonitorService_GracefulDrainOnStop(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	userRepo := &mockUserRepo{
		users: []*domain.User{{ID: 1, Username: "alice", NtfyTopic: "promo/user/alice"}},
	}
	interestRepo := &mockInterestRepo{
		interests: []*domain.UserInterest{
			{ID: 1, UserID: 1, RawTerm: "switch", PositiveTokens: []string{"switch"}, Active: true},
		},
	}
	channelRepo := newMockChannelRepo()
	_ = channelRepo.UpsertChannel(ctx, &domain.TelegramChannel{
		ChatID:          -100,
		Title:           "Deals",
		MonitorProducts: true,
	})

	matchRepo := &slowMatchRepo{delay: 15 * time.Millisecond}
	gateway := &mockNotificationGateway{}

	svc := service.NewMonitorService(
		userRepo,
		interestRepo,
		channelRepo,
		matchRepo,
		&mockCouponRepo{},
		gateway,
		service.MonitorConfig{WorkerCount: 2, QueueSize: 32},
	)
	if err := svc.RebuildSnapshot(ctx); err != nil {
		t.Fatalf("RebuildSnapshot failed: %v", err)
	}

	svc.Start(ctx)
	const totalJobs = 10
	for i := 0; i < totalJobs; i++ {
		_ = svc.ProcessIncomingMessage(ctx, -100, fmt.Sprintf("Switch OLED deal %d", i))
	}

	// Stop immediately without sleeping; all 10 queued jobs must drain cleanly.
	svc.Stop()

	matchRepo.mu.Lock()
	recorded := len(matchRepo.matches)
	matchRepo.mu.Unlock()
	if recorded != totalJobs {
		t.Fatalf("expected all %d queued jobs to drain on Stop, got %d", totalJobs, recorded)
	}

	// Restart cycle works cleanly and dropped counter stays accessible.
	svc.Start(ctx)
	_ = svc.ProcessIncomingMessage(ctx, -100, "Switch OLED restart cycle")
	svc.Stop()

	matchRepo.mu.Lock()
	recordedAfterRestart := len(matchRepo.matches)
	matchRepo.mu.Unlock()
	if recordedAfterRestart != totalJobs+1 {
		t.Fatalf("expected %d total jobs after restart cycle, got %d", totalJobs+1, recordedAfterRestart)
	}
	if svc.DroppedJobsCount() != 0 {
		t.Fatalf("expected 0 dropped jobs, got %d", svc.DroppedJobsCount())
	}
}

type failingCouponRepo struct {
	mu      sync.Mutex
	failErr error
	coupons []domain.Coupon
}

func (m *failingCouponRepo) RecordCoupon(ctx context.Context, code, storeName, storeURL string, channelID int64, text string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failErr != nil {
		return m.failErr
	}
	m.coupons = append(m.coupons, domain.Coupon{
		Code:      code,
		StoreName: storeName,
		StoreURL:  storeURL,
		CreatedAt: time.Now().UTC(),
	})
	return nil
}

func (m *failingCouponRepo) HasRecentCoupon(ctx context.Context, code, storeName string, since time.Time) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, c := range m.coupons {
		if strings.EqualFold(c.Code, code) && strings.EqualFold(c.StoreName, storeName) && !c.CreatedAt.Before(since) {
			return true, nil
		}
	}
	return false, nil
}

func (m *failingCouponRepo) ListExtractedCoupons(ctx context.Context, searchQuery string, limit, offset int) ([]domain.ExtractedCoupon, int, error) {
	return nil, 0, nil
}

func (m *failingCouponRepo) PruneOlderThan(ctx context.Context, cutoff time.Time, batchSize int) (int64, error) {
	return 0, nil
}

func (m *failingCouponRepo) PruneExcessAbove(ctx context.Context, maxKeep int64, batchSize int) (int64, error) {
	return 0, nil
}

func (m *failingCouponRepo) CountTotal(ctx context.Context) (int64, error) {
	return 0, nil
}

func TestMonitorService_CouponDedup_NonPoisoningOnRepoFailure(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	channelRepo := newMockChannelRepo()
	_ = channelRepo.UpsertChannel(ctx, &domain.TelegramChannel{
		ChatID:         600,
		Title:          "Coupons",
		MonitorCoupons: true,
	})

	couponRepo := &failingCouponRepo{failErr: errors.New("sqlite busy")}
	gateway := &mockNotificationGateway{}

	svc := service.NewMonitorService(
		&mockUserRepo{},
		&mockInterestRepo{},
		channelRepo,
		&mockMatchRepo{},
		couponRepo,
		gateway,
		service.MonitorConfig{WorkerCount: 1, QueueSize: 32, CouponDedupWindow: 4 * time.Hour},
	)
	if err := svc.RebuildSnapshot(ctx); err != nil {
		t.Fatalf("RebuildSnapshot failed: %v", err)
	}

	svc.Start(ctx)

	msg := "🎟️ Cupom Amazon: RETRY10 https://www.amazon.com.br/deal"
	_ = svc.ProcessIncomingMessage(ctx, 600, msg)

	// Drain worker so first failed attempt finishes before clearing failErr.
	svc.Stop()

	gateway.mu.Lock()
	notifsAfterFailure := len(gateway.couponNotifs)
	gateway.mu.Unlock()
	if notifsAfterFailure != 0 {
		t.Fatalf("expected 0 notifications when RecordCoupon fails, got %d", notifsAfterFailure)
	}

	// Recover repository and send the same coupon again; dedup cache must NOT be poisoned.
	couponRepo.mu.Lock()
	couponRepo.failErr = nil
	couponRepo.mu.Unlock()

	svc.Start(ctx)
	_ = svc.ProcessIncomingMessage(ctx, 600, msg)
	svc.Stop()

	couponRepo.mu.Lock()
	recorded := len(couponRepo.coupons)
	couponRepo.mu.Unlock()
	if recorded != 1 {
		t.Fatalf("expected 1 recorded coupon after recovery, got %d", recorded)
	}

	gateway.mu.Lock()
	notifsAfterRecovery := len(gateway.couponNotifs)
	gateway.mu.Unlock()
	if notifsAfterRecovery != 1 {
		t.Fatalf("expected 1 coupon notification after recovery, got %d", notifsAfterRecovery)
	}
}

func TestMonitorService_DebugMode_DeliversCouponNotification(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	channelRepo := newMockChannelRepo()
	_ = channelRepo.UpsertChannel(ctx, &domain.TelegramChannel{
		ChatID:         -1001,
		Title:          "Coupons Debug",
		MonitorCoupons: true,
	})

	couponRepo := &mockCouponRepo{}
	gateway := &mockNotificationGateway{}
	debugRepo := &mockDebugMessageRepo{}

	svc := service.NewMonitorService(
		&mockUserRepo{},
		&mockInterestRepo{},
		channelRepo,
		&mockMatchRepo{},
		couponRepo,
		gateway,
		service.MonitorConfig{WorkerCount: 2, QueueSize: 32, CouponDedupWindow: 4 * time.Hour},
	)
	svc.SetDebugRepository(debugRepo)
	_ = svc.RebuildSnapshot(ctx)

	svc.Start(ctx)
	_ = svc.ProcessIncomingTelegramMessage(ctx, domain.IncomingTelegramMessage{
		ChannelID:         -1001,
		TelegramMessageID: 42,
		Text:              "Cupom Kabum: DEBUG10 no site https://www.kabum.com.br",
	})
	svc.Stop()

	couponRepo.mu.Lock()
	couponsCount := len(couponRepo.coupons)
	couponRepo.mu.Unlock()
	if couponsCount != 1 {
		t.Fatalf("expected 1 coupon recorded in debug mode, got %d", couponsCount)
	}

	gateway.mu.Lock()
	notifsCount := len(gateway.couponNotifs)
	gateway.mu.Unlock()
	if notifsCount != 1 {
		t.Fatalf("expected 1 coupon notification delivered in debug mode, got %d", notifsCount)
	}

	debugRepo.mu.Lock()
	debugCount := len(debugRepo.records)
	debugRepo.mu.Unlock()
	if debugCount != 1 {
		t.Fatalf("expected 1 debug record, got %d", debugCount)
	}
}

