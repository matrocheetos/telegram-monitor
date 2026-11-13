package service_test

import (
	"context"
	"errors"
	"testing"

	"github.com/matrocheetos/telegram-monitor/internal/domain"
	"github.com/matrocheetos/telegram-monitor/internal/service"
)

func TestInterestService_AddInterest(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	t.Run("valid interest with rebuild snapshot", func(t *testing.T) {
		t.Parallel()
		interestRepo := &mockInterestRepo{}
		rebuilder := &mockSnapshotRebuilder{}
		svc := service.NewInterestService(interestRepo, rebuilder)

		maxPrice := 1500.0
		interest, err := svc.AddInterest(ctx, 1, "Monitor 144Hz -curvo", &maxPrice)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if interest.UserID != 1 {
			t.Errorf("expected userID 1, got %d", interest.UserID)
		}
		if len(interest.PositiveTokens) == 0 {
			t.Errorf("expected positive tokens")
		}

		rebuilder.mu.Lock()
		calls := rebuilder.calls
		rebuilder.mu.Unlock()
		if calls != 1 {
			t.Errorf("expected 1 rebuild snapshot call, got %d", calls)
		}
	})

	t.Run("fails on empty term", func(t *testing.T) {
		t.Parallel()
		interestRepo := &mockInterestRepo{}
		svc := service.NewInterestService(interestRepo, nil)

		_, err := svc.AddInterest(ctx, 1, "   ", nil)
		if !errors.Is(err, domain.ErrEmptyInterestTerm) {
			t.Errorf("expected ErrEmptyInterestTerm, got %v", err)
		}
	})

	t.Run("fails on negative price", func(t *testing.T) {
		t.Parallel()
		interestRepo := &mockInterestRepo{}
		svc := service.NewInterestService(interestRepo, nil)

		negPrice := -50.0
		_, err := svc.AddInterest(ctx, 1, "SSD NVMe", &negPrice)
		if !errors.Is(err, domain.ErrInvalidPrice) {
			t.Errorf("expected ErrInvalidPrice, got %v", err)
		}
	})

	t.Run("fails on unauthorized user", func(t *testing.T) {
		t.Parallel()
		interestRepo := &mockInterestRepo{}
		svc := service.NewInterestService(interestRepo, nil)

		_, err := svc.AddInterest(ctx, 0, "SSD NVMe", nil)
		if !errors.Is(err, domain.ErrUnauthorized) {
			t.Errorf("expected ErrUnauthorized, got %v", err)
		}
	})
}

func TestInterestService_List_And_Delete(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	interestRepo := &mockInterestRepo{
		interests: []*domain.UserInterest{
			{ID: 10, UserID: 1, RawTerm: "RTX 4060", Active: true},
			{ID: 20, UserID: 2, RawTerm: "PS5", Active: true},
		},
	}
	rebuilder := &mockSnapshotRebuilder{}
	svc := service.NewInterestService(interestRepo, rebuilder)

	// List interests
	interests, err := svc.ListUserInterests(ctx, 1)
	if err != nil {
		t.Fatalf("list user interests failed: %v", err)
	}
	if len(interests) != 1 || interests[0].ID != 10 {
		t.Fatalf("expected interest 10, got %v", interests)
	}

	// Delete interest
	if err := svc.DeleteInterest(ctx, 1, 10); err != nil {
		t.Fatalf("delete interest failed: %v", err)
	}

	remaining, _ := svc.ListUserInterests(ctx, 1)
	if len(remaining) != 0 {
		t.Errorf("expected 0 remaining interests, got %d", len(remaining))
	}

	rebuilder.mu.Lock()
	calls := rebuilder.calls
	rebuilder.mu.Unlock()
	if calls != 1 {
		t.Errorf("expected 1 rebuild snapshot call, got %d", calls)
	}
}

func TestInterestService_CountActiveInterestsByUser(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	interestRepo := &mockInterestRepo{
		interests: []*domain.UserInterest{
			{ID: 1, UserID: 10, RawTerm: "switch", Active: true},
			{ID: 2, UserID: 10, RawTerm: "ps5", Active: true},
			{ID: 3, UserID: 10, RawTerm: "old", Active: false},
			{ID: 4, UserID: 20, RawTerm: "rtx", Active: true},
		},
	}
	svc := service.NewInterestService(interestRepo, nil)

	counts, err := svc.CountActiveInterestsByUser(ctx)
	if err != nil {
		t.Fatalf("CountActiveInterestsByUser failed: %v", err)
	}
	if counts[10] != 2 {
		t.Fatalf("expected 2 active interests for user 10, got %d", counts[10])
	}
	if counts[20] != 1 {
		t.Fatalf("expected 1 active interest for user 20, got %d", counts[20])
	}
}
