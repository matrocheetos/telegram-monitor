package service_test

import (
	"context"
	"fmt"
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
	})
	return nil
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
