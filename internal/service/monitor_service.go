package service

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/matrocheetos/telegram-monitor/internal/domain"
	"github.com/matrocheetos/telegram-monitor/internal/port"
)

var (
	_ port.MessageProcessor = (*MonitorService)(nil)
	_ port.SnapshotRebuilder = (*MonitorService)(nil)
)

type ChannelFlags struct {
	Title           string
	Username        string
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
	WorkerCount       int
	QueueSize         int
	CouponDedupWindow time.Duration
}

type jobKind int

const (
	jobKindProduct jobKind = iota
	jobKindCoupon
	jobKindDebugRecord
)

type monitorJob struct {
	kind        jobKind
	userID      int64
	interestID  int64
	channelID   int64
	username    string
	price       *float64
	text        string
	offerURL    string
	coupon      domain.Coupon
	debugRecord *domain.AnalyzedMessageDebugRecord
}

type MonitorService struct {
	userRepo            port.UserRepository
	interestRepo        port.InterestRepository
	channelRepo         port.ChannelRepository
	matchRepo           port.MatchHistoryRepository
	couponRepo          port.CouponRepository
	notificationGateway port.NotificationGateway
	debugRepo           port.DebugMessageRepository
	cfg                 MonitorConfig

	snapshot atomic.Pointer[MatcherSnapshot]
	jobs     chan monitorJob

	mu          sync.Mutex
	running     atomic.Bool
	cancelFn    context.CancelFunc
	wg          sync.WaitGroup
	dedupMu     sync.Mutex
	couponDedup map[string]time.Time
}

func (s *MonitorService) SetDebugRepository(repo port.DebugMessageRepository) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.debugRepo = repo
}

func (s *MonitorService) DebugRepository() port.DebugMessageRepository {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.debugRepo
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
	if cfg.CouponDedupWindow <= 0 {
		cfg.CouponDedupWindow = 4 * time.Hour
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
		couponDedup:         make(map[string]time.Time),
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
		if s.isDuplicateCoupon(ctx, job.coupon) {
			return
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
		if s.notificationGateway != nil {
			if err := s.notificationGateway.SendCouponNotification(ctx, job.coupon); err != nil {
				slog.Error("failed to send coupon notification",
					"code", job.coupon.Code,
					"store", job.coupon.StoreName,
					"error", err,
				)
			}
		}
	case jobKindDebugRecord:
		s.mu.Lock()
		debugRepo := s.debugRepo
		s.mu.Unlock()
		if debugRepo != nil && job.debugRecord != nil {
			if err := debugRepo.RecordAnalyzedMessage(ctx, job.debugRecord); err != nil {
				slog.Error("failed to record debug analyzed message",
					"channel_id", job.debugRecord.ChannelID,
					"telegram_message_id", job.debugRecord.TelegramMessageID,
					"error", err,
				)
			}
		}
	}
}

func (s *MonitorService) isDuplicateCoupon(ctx context.Context, coupon domain.Coupon) bool {
	key := strings.ToUpper(strings.TrimSpace(coupon.Code)) + "|" + strings.ToLower(strings.TrimSpace(coupon.StoreName))
	now := time.Now().UTC()
	cutoff := now.Add(-s.cfg.CouponDedupWindow)

	s.dedupMu.Lock()
	if lastSeen, found := s.couponDedup[key]; found && lastSeen.After(cutoff) {
		s.dedupMu.Unlock()
		return true
	}
	s.dedupMu.Unlock()

	if s.couponRepo != nil {
		hasRecent, err := s.couponRepo.HasRecentCoupon(ctx, coupon.Code, coupon.StoreName, cutoff)
		if err != nil {
			slog.Error("failed to check recent coupon in repository", "error", err)
		} else if hasRecent {
			s.dedupMu.Lock()
			s.couponDedup[key] = now
			s.dedupMu.Unlock()
			return true
		}
	}

	s.dedupMu.Lock()
	if lastSeen, found := s.couponDedup[key]; found && lastSeen.After(cutoff) {
		s.dedupMu.Unlock()
		return true
	}
	s.couponDedup[key] = now
	if len(s.couponDedup) > 2000 {
		for k, t := range s.couponDedup {
			if t.Before(cutoff) {
				delete(s.couponDedup, k)
			}
		}
	}
	s.dedupMu.Unlock()

	return false
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
			Title:           ch.Title,
			Username:        ch.Username,
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
	msg := domain.IncomingTelegramMessage{
		ChannelID:   channelID,
		MessageDate: time.Now().UTC(),
		Text:        messageText,
	}
	return s.ProcessIncomingTelegramMessage(ctx, msg)
}

func (s *MonitorService) ProcessIncomingTelegramMessage(ctx context.Context, msg domain.IncomingTelegramMessage) error {
	snap := s.snapshot.Load()
	if snap == nil {
		return nil
	}

	flags, channelKnown := snap.Channels[msg.ChannelID]
	isMonitored := channelKnown && (flags.MonitorProducts || flags.MonitorCoupons)

	s.mu.Lock()
	debugRepo := s.debugRepo
	s.mu.Unlock()

	if debugRepo == nil {
		if !isMonitored {
			return nil
		}
		if flags.MonitorProducts && len(snap.Interests) > 0 {
			s.matchProducts(ctx, snap.Interests, msg.ChannelID, msg.Text)
		}
		if flags.MonitorCoupons {
			s.matchCoupons(ctx, msg.ChannelID, msg.Text)
		}
		return nil
	}

	s.processAndDebugTelegramMessage(ctx, snap, flags, channelKnown, msg)
	return nil
}

func (s *MonitorService) processAndDebugTelegramMessage(
	ctx context.Context,
	snap *MatcherSnapshot,
	flags ChannelFlags,
	channelKnown bool,
	msg domain.IncomingTelegramMessage,
) {
	rawText := msg.Text
	normalizedText := domain.NormalizeText(rawText)
	tokens := domain.Tokenize(rawText)
	allPrices := domain.ExtractAllPrices(rawText)
	pricesFoundCount := len(allPrices)

	var extractedLowestPrice *float64
	if p, found := domain.ExtractLowestPrice(rawText); found {
		extractedLowestPrice = &p
	}

	plainURLs := domain.ExtractAllURLs(rawText)
	firstURL := domain.ExtractFirstURL(rawText)
	hasCouponIndicator := domain.HasCouponIndicator(rawText)
	identifiedStore, identifiedStoreURL := domain.IdentifyStoreFromText(rawText)

	hasCodeEntity := false
	for _, ent := range msg.Entities {
		if ent.Type == "code" || ent.Type == "pre" {
			hasCodeEntity = true
			break
		}
	}

	var matchedInterests []domain.DebugMatchedInterestSummary
	if channelKnown && flags.MonitorProducts && len(snap.Interests) > 0 && len(tokens) > 0 {
		tokenSet := domain.BuildTokenSet(tokens)
		var offerURL string
		var offerURLExtracted bool

		for i := range snap.Interests {
			interest := &snap.Interests[i]

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

			if interest.MaxPrice != nil {
				if extractedLowestPrice == nil || *extractedLowestPrice > *interest.MaxPrice {
					continue
				}
			}

			if !offerURLExtracted {
				offerURL = firstURL
				offerURLExtracted = true
			}

			matchedInterests = append(matchedInterests, domain.DebugMatchedInterestSummary{
				InterestID: interest.ID,
				UserID:     interest.UserID,
				Term:       interest.RawTerm,
				MaxPrice:   interest.MaxPrice,
			})

			job := monitorJob{
				kind:       jobKindProduct,
				userID:     interest.UserID,
				interestID: interest.ID,
				channelID:  msg.ChannelID,
				username:   interest.Username,
				price:      extractedLowestPrice,
				text:       rawText,
				offerURL:   offerURL,
			}
			s.enqueue(ctx, job)
		}
	}

	var extractedCoupons []domain.DebugExtractedCouponSummary
	var dedupCount int
	allCoupons := domain.ExtractCoupons(rawText)

	if len(allCoupons) > 0 {
		seenInMsg := make(map[string]struct{}, len(allCoupons))
		for _, coupon := range allCoupons {
			key := strings.ToUpper(strings.TrimSpace(coupon.Code)) + "|" + strings.ToLower(strings.TrimSpace(coupon.StoreName))
			if _, exists := seenInMsg[key]; exists {
				continue
			}
			seenInMsg[key] = struct{}{}

			isDedup := s.isDuplicateCoupon(ctx, coupon)
			if isDedup {
				dedupCount++
			}

			extractedCoupons = append(extractedCoupons, domain.DebugExtractedCouponSummary{
				Code:         coupon.Code,
				StoreName:    coupon.StoreName,
				StoreURL:     coupon.StoreURL,
				Deduplicated: isDedup,
			})

			if channelKnown && flags.MonitorCoupons && !isDedup {
				job := monitorJob{
					kind:      jobKindCoupon,
					channelID: msg.ChannelID,
					text:      rawText,
					coupon:    coupon,
				}
				s.enqueue(ctx, job)
			}
		}
	}

	var outcome domain.EvaluationOutcome
	isMonitored := channelKnown && (flags.MonitorProducts || flags.MonitorCoupons)
	trimmed := strings.TrimSpace(rawText)

	switch {
	case trimmed == "":
		outcome = domain.OutcomeEmptyText
	case !isMonitored:
		outcome = domain.OutcomeUnmonitoredChannel
	case len(matchedInterests) > 0 && len(extractedCoupons) > 0:
		outcome = domain.OutcomeMatchedProductAndCoupon
	case len(matchedInterests) > 0:
		outcome = domain.OutcomeMatchedProduct
	case len(extractedCoupons) > 0:
		outcome = domain.OutcomeExtractedCoupon
	case hasCouponIndicator || hasCodeEntity:
		outcome = domain.OutcomeCouponIndicatorNoCode
	default:
		outcome = domain.OutcomeMonitoredNoMatch
	}

	msgDate := msg.MessageDate
	if msgDate.IsZero() {
		msgDate = time.Now().UTC()
	}

	debugRecord := &domain.AnalyzedMessageDebugRecord{
		TelegramMessageID:        msg.TelegramMessageID,
		ChannelID:                msg.ChannelID,
		ChannelTitle:             flags.Title,
		ChannelUsername:          flags.Username,
		MonitorProducts:          flags.MonitorProducts,
		MonitorCoupons:           flags.MonitorCoupons,
		RawText:                  rawText,
		NormalizedText:           normalizedText,
		Tokens:                   tokens,
		HasMedia:                 msg.HasMedia,
		MediaType:                msg.MediaType,
		TGEntities:               msg.Entities,
		TGInlineButtons:          msg.InlineButtons,
		TGWebPageURL:             msg.WebPageURL,
		PlainURLs:                plainURLs,
		FirstURL:                 firstURL,
		AllPrices:                allPrices,
		PricesFoundCount:         pricesFoundCount,
		ExtractedLowestPrice:     extractedLowestPrice,
		HasCouponIndicator:       hasCouponIndicator,
		IdentifiedStore:          identifiedStore,
		IdentifiedStoreURL:       identifiedStoreURL,
		ExtractedCoupons:         extractedCoupons,
		ExtractedCouponsCount:    len(extractedCoupons),
		DeduplicatedCouponsCount: dedupCount,
		MatchedInterests:         matchedInterests,
		MatchedProductsCount:     len(matchedInterests),
		EvaluationOutcome:        outcome,
		MessageDate:              msgDate,
		AnalyzedAt:               time.Now().UTC(),
	}

	s.enqueue(ctx, monitorJob{
		kind:        jobKindDebugRecord,
		channelID:   msg.ChannelID,
		debugRecord: debugRecord,
	})
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
	if len(coupons) == 0 {
		return
	}

	seenInMsg := make(map[string]struct{}, len(coupons))
	for _, coupon := range coupons {
		key := strings.ToUpper(strings.TrimSpace(coupon.Code)) + "|" + strings.ToLower(strings.TrimSpace(coupon.StoreName))
		if _, exists := seenInMsg[key]; exists {
			continue
		}
		seenInMsg[key] = struct{}{}

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
