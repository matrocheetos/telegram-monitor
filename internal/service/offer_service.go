package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/matrocheetos/telegram-monitor/internal/domain"
	"github.com/matrocheetos/telegram-monitor/internal/port"
)

var _ port.OfferHistoryUseCase = (*OfferService)(nil)

type OfferServiceConfig struct {
	RetentionDays int
	MaxKeep       int64
	BatchSize     int
}

type OfferService struct {
	matchRepo  port.MatchHistoryRepository
	couponRepo port.CouponRepository
	cfg        OfferServiceConfig

	mu       sync.Mutex
	running  atomic.Bool
	cancelFn context.CancelFunc
	wg       sync.WaitGroup
}

func NewOfferService(
	matchRepo port.MatchHistoryRepository,
	couponRepo port.CouponRepository,
	cfg OfferServiceConfig,
) *OfferService {
	if cfg.RetentionDays <= 0 {
		cfg.RetentionDays = 90
	}
	if cfg.MaxKeep <= 0 {
		cfg.MaxKeep = 100000
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 1000
	}

	return &OfferService{
		matchRepo:  matchRepo,
		couponRepo: couponRepo,
		cfg:        cfg,
	}
}

func (s *OfferService) ListUserMatchedProducts(ctx context.Context, userID int64, searchQuery string, limit, offset int) ([]domain.UserMatchedProduct, int, error) {
	return s.matchRepo.ListUserMatchedProducts(ctx, userID, searchQuery, limit, offset)
}

func (s *OfferService) ListExtractedCoupons(ctx context.Context, searchQuery string, limit, offset int) ([]domain.ExtractedCoupon, int, error) {
	return s.couponRepo.ListExtractedCoupons(ctx, searchQuery, limit, offset)
}

func (s *OfferService) RunRetentionCleanup(ctx context.Context) error {
	slog.InfoContext(ctx, "starting periodic retention cleanup", "retention_days", s.cfg.RetentionDays, "max_keep", s.cfg.MaxKeep, "batch_size", s.cfg.BatchSize)

	cutoff := time.Now().UTC().AddDate(0, 0, -s.cfg.RetentionDays)

	// Step 1: Prune older than retention cutoff in batches of batchSize
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		deleted, err := s.matchRepo.PruneOlderThan(ctx, cutoff, s.cfg.BatchSize)
		if err != nil {
			return fmt.Errorf("prune old matched products: %w", err)
		}
		if deleted > 0 {
			slog.DebugContext(ctx, "pruned batch of old matched products", "deleted", deleted)
		}
		if deleted < int64(s.cfg.BatchSize) {
			break
		}
	}

	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		deleted, err := s.couponRepo.PruneOlderThan(ctx, cutoff, s.cfg.BatchSize)
		if err != nil {
			return fmt.Errorf("prune old coupons: %w", err)
		}
		if deleted > 0 {
			slog.DebugContext(ctx, "pruned batch of old coupons", "deleted", deleted)
		}
		if deleted < int64(s.cfg.BatchSize) {
			break
		}
	}

	// Step 2: Enforce maxKeep ceiling in batches of batchSize
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		count, err := s.matchRepo.CountTotal(ctx)
		if err != nil {
			return fmt.Errorf("count matched products: %w", err)
		}
		if count <= s.cfg.MaxKeep {
			break
		}
		deleted, err := s.matchRepo.PruneExcessAbove(ctx, s.cfg.MaxKeep, s.cfg.BatchSize)
		if err != nil {
			return fmt.Errorf("prune excess matched products: %w", err)
		}
		if deleted == 0 {
			break
		}
		slog.DebugContext(ctx, "pruned batch of excess matched products", "deleted", deleted)
	}

	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		count, err := s.couponRepo.CountTotal(ctx)
		if err != nil {
			return fmt.Errorf("count coupons: %w", err)
		}
		if count <= s.cfg.MaxKeep {
			break
		}
		deleted, err := s.couponRepo.PruneExcessAbove(ctx, s.cfg.MaxKeep, s.cfg.BatchSize)
		if err != nil {
			return fmt.Errorf("prune excess coupons: %w", err)
		}
		if deleted == 0 {
			break
		}
		slog.DebugContext(ctx, "pruned batch of excess coupons", "deleted", deleted)
	}

	slog.InfoContext(ctx, "retention cleanup completed successfully")
	return nil
}

func (s *OfferService) StartRetentionJob(ctx context.Context, interval time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.running.Swap(true) {
		return
	}

	jobCtx, cancel := context.WithCancel(ctx)
	s.cancelFn = cancel

	if interval <= 0 {
		interval = time.Hour
	}

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer s.running.Store(false)

		if err := s.RunRetentionCleanup(jobCtx); err != nil && !errors.Is(err, context.Canceled) {
			slog.ErrorContext(jobCtx, "startup retention cleanup error", "error", err)
		}

		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-jobCtx.Done():
				return
			case <-ticker.C:
				if err := s.RunRetentionCleanup(jobCtx); err != nil && !errors.Is(err, context.Canceled) {
					slog.ErrorContext(jobCtx, "periodic retention cleanup error", "error", err)
				}
			}
		}
	}()
}

func (s *OfferService) Stop() {
	s.mu.Lock()
	if !s.running.Load() {
		s.mu.Unlock()
		return
	}
	if s.cancelFn != nil {
		s.cancelFn()
	}
	s.mu.Unlock()

	s.wg.Wait()
}
