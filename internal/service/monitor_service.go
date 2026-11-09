package service

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"

	"github.com/matrocheetos/telegram-monitor/internal/domain"
	"github.com/matrocheetos/telegram-monitor/internal/port"
)

var (
	_ port.MessageProcessor = (*MonitorService)(nil)
	_ port.SnapshotRebuilder = (*MonitorService)(nil)
)

type ChannelFlags struct {
	MonitorProducts bool
	MonitorCoupons  bool
}

type CompiledInterest struct {
	ID             int64
	UserID         int64
	Username       string
	NtfyTopic      string
	RawTerm        string
	PositiveTokens []string
	NegativeTokens []string
	MaxPrice       *float64
}

type MatcherSnapshot struct {
	Channels  map[int64]ChannelFlags
	Interests []CompiledInterest
}

type MonitorConfig struct {
	WorkerCount int
	QueueSize   int
}

type jobKind int

const (
	jobKindProduct jobKind = iota
	jobKindCoupon
)

type monitorJob struct {
	kind       jobKind
	userID     int64
	interestID int64
	channelID  int64
	username   string
	price      *float64
	text       string
	offerURL   string
	coupon     domain.Coupon
}

type MonitorService struct {
	userRepo            port.UserRepository
	interestRepo        port.InterestRepository
	channelRepo         port.ChannelRepository
	matchRepo           port.MatchHistoryRepository
	couponRepo          port.CouponRepository
	notificationGateway port.NotificationGateway
	cfg                 MonitorConfig

	snapshot atomic.Pointer[MatcherSnapshot]
	jobs     chan monitorJob

	mu       sync.Mutex
	running  atomic.Bool
	cancelFn context.CancelFunc
	wg       sync.WaitGroup
}

func NewMonitorService(
	userRepo port.UserRepository,
	interestRepo port.InterestRepository,
	channelRepo port.ChannelRepository,
	matchRepo port.MatchHistoryRepository,
	couponRepo port.CouponRepository,
	notificationGateway port.NotificationGateway,
	cfg MonitorConfig,
) *MonitorService {
	if cfg.WorkerCount <= 0 {
		cfg.WorkerCount = 4
	}
	if cfg.QueueSize <= 0 {
		cfg.QueueSize = 1024
	}

	s := &MonitorService{
		userRepo:            userRepo,
		interestRepo:        interestRepo,
		channelRepo:         channelRepo,
		matchRepo:           matchRepo,
		couponRepo:          couponRepo,
		notificationGateway: notificationGateway,
		cfg:                 cfg,
		jobs:                make(chan monitorJob, cfg.QueueSize),
	}

	s.snapshot.Store(&MatcherSnapshot{
		Channels:  make(map[int64]ChannelFlags),
		Interests: nil,
	})

	return s
}

func (s *MonitorService) Start(ctx context.Context) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.running.Load() {
		return
	}

	workerCtx, cancel := context.WithCancel(ctx)
	s.cancelFn = cancel
	s.running.Store(true)

	for i := 0; i < s.cfg.WorkerCount; i++ {
		s.wg.Add(1)
		go s.worker(workerCtx)
	}
}

func (s *MonitorService) Stop() {
	s.mu.Lock()
	if !s.running.Load() {
		s.mu.Unlock()
		return
	}
	s.running.Store(false)
	cancel := s.cancelFn
	s.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	s.wg.Wait()
}

func (s *MonitorService) worker(ctx context.Context) {
	defer s.wg.Done()

	for {
		select {
		case <-ctx.Done():
			return
		case job, ok := <-s.jobs:
			if !ok {
				return
			}
			s.processJob(ctx, job)
		}
	}
}

func (s *MonitorService) processJob(ctx context.Context, job monitorJob) {
	switch job.kind {
	case jobKindProduct:
		if s.notificationGateway != nil {
			if err := s.notificationGateway.SendProductNotification(ctx, job.username, job.offerURL, job.price, job.text); err != nil {
				slog.Error("failed to send product notification",
					"username", job.username,
					"error", err,
				)
			}
		}
		if s.matchRepo != nil {
			if err := s.matchRepo.RecordProductMatch(ctx, job.userID, &job.interestID, job.channelID, job.price, job.text); err != nil {
				slog.Error("failed to record product match",
					"user_id", job.userID,
					"interest_id", job.interestID,
					"channel_id", job.channelID,
					"error", err,
				)
			}
		}
	case jobKindCoupon:
		if s.notificationGateway != nil {
			if err := s.notificationGateway.SendCouponNotification(ctx, job.coupon); err != nil {
				slog.Error("failed to send coupon notification",
					"code", job.coupon.Code,
					"store", job.coupon.StoreName,
					"error", err,
				)
			}
		}
		if s.couponRepo != nil {
			if err := s.couponRepo.RecordCoupon(ctx, job.coupon.Code, job.coupon.StoreName, job.coupon.StoreURL, job.channelID, job.text); err != nil {
				slog.Error("failed to record coupon",
					"code", job.coupon.Code,
					"store", job.coupon.StoreName,
					"channel_id", job.channelID,
					"error", err,
				)
			}
		}
	}
}

func (s *MonitorService) RebuildSnapshot(ctx context.Context) error {
	channels, err := s.channelRepo.ListChannels(ctx)
	if err != nil {
		return fmt.Errorf("list channels: %w", err)
	}

	interests, err := s.interestRepo.ListAllActiveInterests(ctx)
	if err != nil {
		return fmt.Errorf("list interests: %w", err)
	}

	users, err := s.userRepo.ListUsers(ctx)
	if err != nil {
		return fmt.Errorf("list users: %w", err)
	}

	userMap := make(map[int64]*domain.User, len(users))
	for _, u := range users {
		userMap[u.ID] = u
	}

	channelMap := make(map[int64]ChannelFlags, len(channels))
	for _, ch := range channels {
		channelMap[ch.ChatID] = ChannelFlags{
			MonitorProducts: ch.MonitorProducts,
			MonitorCoupons:  ch.MonitorCoupons,
		}
	}

	compiledInterests := make([]CompiledInterest, 0, len(interests))
	for _, it := range interests {
		u, ok := userMap[it.UserID]
		if !ok {
			continue
		}
		compiledInterests = append(compiledInterests, CompiledInterest{
			ID:             it.ID,
			UserID:         it.UserID,
			Username:       u.Username,
			NtfyTopic:      u.NtfyTopic,
			RawTerm:        it.RawTerm,
			PositiveTokens: it.PositiveTokens,
			NegativeTokens: it.NegativeTokens,
			MaxPrice:       it.MaxPrice,
		})
	}

	newSnap := &MatcherSnapshot{
		Channels:  channelMap,
		Interests: compiledInterests,
	}

	s.snapshot.Store(newSnap)
	return nil
}

func (s *MonitorService) Snapshot() *MatcherSnapshot {
	return s.snapshot.Load()
}

func (s *MonitorService) ProcessIncomingMessage(ctx context.Context, channelID int64, messageText string) error {
	snap := s.snapshot.Load()
	if snap == nil {
		return nil
	}

	flags, ok := snap.Channels[channelID]
	if !ok || (!flags.MonitorProducts && !flags.MonitorCoupons) {
		return nil
	}

	if flags.MonitorProducts && len(snap.Interests) > 0 {
		s.matchProducts(ctx, snap.Interests, channelID, messageText)
	}

	if flags.MonitorCoupons {
		s.matchCoupons(ctx, channelID, messageText)
	}

	return nil
}

func (s *MonitorService) matchProducts(ctx context.Context, interests []CompiledInterest, channelID int64, messageText string) {
	msgTokens := domain.Tokenize(messageText)
	if len(msgTokens) == 0 {
		return
	}

	tokenSet := domain.BuildTokenSet(msgTokens)
	var lowestPrice *float64
	var priceExtracted bool

	var offerURL string
	var offerURLExtracted bool

	for i := range interests {
		interest := &interests[i]

		negMatched := false
		for _, neg := range interest.NegativeTokens {
			if _, exists := tokenSet[neg]; exists {
				negMatched = true
				break
			}
		}
		if negMatched {
			continue
		}

		posMatched := true
		for _, pos := range interest.PositiveTokens {
			tokFound := false
			if _, exists := tokenSet[pos]; exists {
				tokFound = true
			} else if len(pos) >= 5 {
				for tok := range tokenSet {
					if domain.IsLevenshteinLE1(pos, tok) {
						tokFound = true
						break
					}
				}
			}
			if !tokFound {
				posMatched = false
				break
			}
		}
		if !posMatched {
			continue
		}

		if !priceExtracted {
			if p, found := domain.ExtractLowestPrice(messageText); found {
				lowestPrice = &p
			}
			priceExtracted = true
		}

		if interest.MaxPrice != nil {
			if lowestPrice == nil || *lowestPrice > *interest.MaxPrice {
				continue
			}
		}

		if !offerURLExtracted {
			offerURL = domain.ExtractFirstURL(messageText)
			offerURLExtracted = true
		}

		job := monitorJob{
			kind:       jobKindProduct,
			userID:     interest.UserID,
			interestID: interest.ID,
			channelID:  channelID,
			username:   interest.Username,
			price:      lowestPrice,
			text:       messageText,
			offerURL:   offerURL,
		}
		s.enqueue(ctx, job)
	}
}

func (s *MonitorService) matchCoupons(ctx context.Context, channelID int64, messageText string) {
	coupons := domain.ExtractCoupons(messageText)
	for _, coupon := range coupons {
		job := monitorJob{
			kind:      jobKindCoupon,
			channelID: channelID,
			text:      messageText,
			coupon:    coupon,
		}
		s.enqueue(ctx, job)
	}
}

func (s *MonitorService) enqueue(ctx context.Context, job monitorJob) {
	select {
	case <-ctx.Done():
		return
	case s.jobs <- job:
	default:
		slog.Warn("monitor worker queue is full, dropping job",
			"kind", job.kind,
			"channel_id", job.channelID,
		)
	}
}
