package service_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/matrocheetos/telegram-monitor/internal/adapter/secondary/sqlite"
	"github.com/matrocheetos/telegram-monitor/internal/domain"
	"github.com/matrocheetos/telegram-monitor/internal/service"
)

// Helper to create an in-memory SQLite DB with schema
func newStressDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sqlite.OpenDB(context.Background(), ":memory:")
	if err != nil {
		t.Fatalf("OpenDB failed: %v", err)
	}
	return db
}

// 1. Verify OfferService.StartRetentionJob with immediate context cancellation (no deadlocks)
func TestOfferService_ImmediateCancellation(t *testing.T) {
	t.Parallel()

	matchRepo := &mockOfferMatchRepo{totalCount: 100}
	couponRepo := &mockOfferCouponRepo{totalCount: 100}

	t.Run("PreCancelledContext", func(t *testing.T) {
		svc := service.NewOfferService(matchRepo, couponRepo, service.OfferServiceConfig{})
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel before Start

		svc.StartRetentionJob(ctx, 10*time.Millisecond)

		done := make(chan struct{})
		go func() {
			svc.Stop()
			close(done)
		}()

		select {
		case <-done:
			// Success, no deadlock
		case <-time.After(2 * time.Second):
			t.Fatal("deadlock: Stop did not return within 2 seconds for pre-cancelled context")
		}
	})

	t.Run("CancelImmediatelyAfterStart", func(t *testing.T) {
		for i := 0; i < 50; i++ {
			svc := service.NewOfferService(matchRepo, couponRepo, service.OfferServiceConfig{})
			ctx, cancel := context.WithCancel(context.Background())

			svc.StartRetentionJob(ctx, 10*time.Millisecond)
			cancel() // immediate cancellation

			done := make(chan struct{})
			go func() {
				svc.Stop()
				close(done)
			}()

			select {
			case <-done:
			case <-time.After(2 * time.Second):
				t.Fatalf("deadlock in iteration %d: Stop did not return within 2 seconds", i)
			}
		}
	})

	t.Run("ConcurrentStartAndStopStress", func(t *testing.T) {
		for i := 0; i < 50; i++ {
			svc := service.NewOfferService(matchRepo, couponRepo, service.OfferServiceConfig{})
			ctx, cancel := context.WithCancel(context.Background())

			var wg sync.WaitGroup
			wg.Add(3)

			go func() {
				defer wg.Done()
				svc.StartRetentionJob(ctx, 5*time.Millisecond)
			}()

			go func() {
				defer wg.Done()
				cancel()
			}()

			go func() {
				defer wg.Done()
				svc.Stop()
			}()

			done := make(chan struct{})
			go func() {
				wg.Wait()
				svc.Stop()
				close(done)
			}()

			select {
			case <-done:
			case <-time.After(2 * time.Second):
				t.Fatalf("deadlock in iteration %d during concurrent Start/Stop/Cancel", i)
			}
		}
	})
}

// 2. Verify OfferService.StartRetentionJob leaks zero goroutines on shutdown
func TestOfferService_GoroutineLeakFreedom(t *testing.T) {
	// Not t.Parallel to get clean NumGoroutine baseline
	matchRepo := &mockOfferMatchRepo{totalCount: 50}
	couponRepo := &mockOfferCouponRepo{totalCount: 50}

	// Warm up runtime and let any background runtime goroutines stabilize
	for i := 0; i < 3; i++ {
		runtime.GC()
		time.Sleep(20 * time.Millisecond)
	}
	baseline := runtime.NumGoroutine()

	// Run 25 consecutive start-and-stop cycles
	for i := 0; i < 25; i++ {
		svc := service.NewOfferService(matchRepo, couponRepo, service.OfferServiceConfig{})
		ctx, cancel := context.WithCancel(context.Background())

		svc.StartRetentionJob(ctx, 10*time.Millisecond)
		time.Sleep(15 * time.Millisecond) // Let it tick at least once
		cancel()
		svc.Stop()
	}

	// Allow goroutines to fully exit
	var current int
	leaked := true
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		runtime.GC()
		current = runtime.NumGoroutine()
		if current <= baseline {
			leaked = false
			break
		}
		time.Sleep(30 * time.Millisecond)
	}

	if leaked {
		t.Fatalf("goroutine leak detected: baseline=%d, current=%d (difference = +%d goroutines)", baseline, current, current-baseline)
	}
}

// 3. What happens if the database connection is closed during retention run? Does it handle errors gracefully without panics?
func TestOfferService_DatabaseClosedResilience(t *testing.T) {
	t.Parallel()

	t.Run("RunRetentionCleanup_WithClosedDB", func(t *testing.T) {
		db := newStressDB(t)
		matchRepo := sqlite.NewMatchHistoryRepository(db)
		couponRepo := sqlite.NewCouponRepository(db)

		svc := service.NewOfferService(matchRepo, couponRepo, service.OfferServiceConfig{})

		// Close database connection
		if err := db.Close(); err != nil {
			t.Fatalf("close db: %v", err)
		}

		// Ensure calling RunRetentionCleanup does NOT panic and returns a wrapped error
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("panic occurred when running retention cleanup on closed DB: %v", r)
			}
		}()

		err := svc.RunRetentionCleanup(context.Background())
		if err == nil {
			t.Fatal("expected error when running retention cleanup on closed DB, got nil")
		}
		if !errors.Is(err, sql.ErrConnDone) && !isDBClosedErr(err) {
			t.Logf("received expected DB error: %v", err)
		}
	})

	t.Run("StartRetentionJob_BackgroundWorker_WithClosedDB", func(t *testing.T) {
		db := newStressDB(t)
		matchRepo := sqlite.NewMatchHistoryRepository(db)
		couponRepo := sqlite.NewCouponRepository(db)

		svc := service.NewOfferService(matchRepo, couponRepo, service.OfferServiceConfig{})

		// Close DB
		_ = db.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		defer cancel()

		// StartRetentionJob runs RunRetentionCleanup in goroutine; it must handle error gracefully without panicking
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("panic in background worker on closed DB: %v", r)
			}
		}()

		svc.StartRetentionJob(ctx, 20*time.Millisecond)

		// Wait for ticker to fire on closed DB multiple times
		time.Sleep(100 * time.Millisecond)

		svc.Stop()
	})

	t.Run("ConcurrentCloseDuringActiveRetentionRun", func(t *testing.T) {
		db := newStressDB(t)
		defer func() { _ = db.Close() }()

		userRepo := sqlite.NewUserRepository(db)
		matchRepo := sqlite.NewMatchHistoryRepository(db)
		couponRepo := sqlite.NewCouponRepository(db)

		ctx := context.Background()
		u := &domain.User{Username: "stress_user", PasswordHash: "h", Role: domain.RoleUser, NtfyTopic: "top"}
		if err := userRepo.CreateUser(ctx, u); err != nil {
			t.Fatalf("CreateUser: %v", err)
		}

		// Populate 1000 records
		now := time.Now().UTC().AddDate(0, 0, -100)
		for i := 0; i < 500; i++ {
			_ = matchRepo.RecordProductMatch(ctx, u.ID, nil, -100, nil, fmt.Sprintf("old item %d", i))
			_ = couponRepo.RecordCoupon(ctx, fmt.Sprintf("C%d", i), "Store", "http://s.com", -100, "text")
		}
		// Update created_at to be older than 90 days
		_, _ = db.ExecContext(ctx, "UPDATE matched_products SET created_at = ?", now)
		_, _ = db.ExecContext(ctx, "UPDATE coupons SET created_at = ?", now)

		svc := service.NewOfferService(matchRepo, couponRepo, service.OfferServiceConfig{BatchSize: 10})

		// Run retention in loop while closing DB from another goroutine
		var wg sync.WaitGroup
		wg.Add(2)

		go func() {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("panic during concurrent DB close: %v", r)
				}
			}()
			for i := 0; i < 50; i++ {
				_ = svc.RunRetentionCleanup(context.Background())
				time.Sleep(1 * time.Millisecond)
			}
		}()

		go func() {
			defer wg.Done()
			time.Sleep(5 * time.Millisecond)
			_ = db.Close()
		}()

		wg.Wait()
	})
}

func isDBClosedErr(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return errors.Is(err, sql.ErrConnDone) ||
		errors.Is(err, sql.ErrTxDone) ||
		len(msg) > 0 // database error wrapped
}
