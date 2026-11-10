package service_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/matrocheetos/telegram-monitor/internal/domain"
	"github.com/matrocheetos/telegram-monitor/internal/service"
)

type stressMockRepo struct {
	mu             sync.Mutex
	totalRecords   int64
	oldRecords     int64
	pruneOldCalls  int
	pruneCeilCalls int
	countCalls     int
}

func (m *stressMockRepo) RecordProductMatch(ctx context.Context, userID int64, interestID *int64, channelID int64, price *float64, text string) error {
	return nil
}

func (m *stressMockRepo) ListUserMatchedProducts(ctx context.Context, userID int64, searchQuery string, limit, offset int) ([]domain.UserMatchedProduct, int, error) {
	return nil, 0, nil
}

func (m *stressMockRepo) RecordCoupon(ctx context.Context, code, storeName, storeURL string, channelID int64, text string) error {
	return nil
}

func (m *stressMockRepo) ListExtractedCoupons(ctx context.Context, searchQuery string, limit, offset int) ([]domain.ExtractedCoupon, int, error) {
	return nil, 0, nil
}

func (m *stressMockRepo) PruneOlderThan(ctx context.Context, cutoff time.Time, batchSize int) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pruneOldCalls++

	if m.oldRecords <= 0 {
		return 0, nil
	}
	deleted := int64(batchSize)
	if deleted > m.oldRecords {
		deleted = m.oldRecords
	}
	m.oldRecords -= deleted
	m.totalRecords -= deleted
	return deleted, nil
}

func (m *stressMockRepo) PruneExcessAbove(ctx context.Context, maxKeep int64, batchSize int) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pruneCeilCalls++

	if m.totalRecords <= maxKeep {
		return 0, nil
	}
	excess := m.totalRecords - maxKeep
	deleted := int64(batchSize)
	if deleted > excess {
		deleted = excess
	}
	m.totalRecords -= deleted
	return deleted, nil
}

func (m *stressMockRepo) CountTotal(ctx context.Context) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.countCalls++
	return m.totalRecords, nil
}

func TestOfferService_RunRetentionCleanup_StressBatchSizes(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	cases := []struct {
		name              string
		initialOld        int64
		initialTotal      int64
		maxKeep           int64
		batchSize         int
		wantPruneOldCalls int
		wantPruneCeilCalls int
		wantFinalTotal    int64
	}{
		{
			name:              "0_old_and_total_99999",
			initialOld:        0,
			initialTotal:      99999,
			maxKeep:           100000,
			batchSize:         1000,
			wantPruneOldCalls: 1, // 1 call returning 0 (<1000, terminates)
			wantPruneCeilCalls: 0, // total <= maxKeep (99999 <= 100000), break immediately
			wantFinalTotal:    99999,
		},
		{
			name:              "0_old_and_total_100000",
			initialOld:        0,
			initialTotal:      100000,
			maxKeep:           100000,
			batchSize:         1000,
			wantPruneOldCalls: 1, // 1 call returning 0
			wantPruneCeilCalls: 0, // total <= maxKeep (100000 <= 100000), break immediately
			wantFinalTotal:    100000,
		},
		{
			name:              "1000_old_and_1000_total",
			initialOld:        1000,
			initialTotal:      1000,
			maxKeep:           100000,
			batchSize:         1000,
			wantPruneOldCalls: 2, // call 1: deletes 1000; call 2: deletes 0 (<1000, terminates)
			wantPruneCeilCalls: 0,
			wantFinalTotal:    0,
		},
		{
			name:              "2500_old_and_2500_total",
			initialOld:        2500,
			initialTotal:      2500,
			maxKeep:           100000,
			batchSize:         1000,
			wantPruneOldCalls: 3, // call 1: 1000; call 2: 1000; call 3: 500 (<1000, terminates)
			wantPruneCeilCalls: 0,
			wantFinalTotal:    0,
		},
		{
			name:              "3000_old_and_3000_total",
			initialOld:        3000,
			initialTotal:      3000,
			maxKeep:           100000,
			batchSize:         1000,
			wantPruneOldCalls: 4, // call 1: 1000; call 2: 1000; call 3: 1000; call 4: 0 (<1000, terminates)
			wantPruneCeilCalls: 0,
			wantFinalTotal:    0,
		},
		{
			name:              "102500_total_with_0_old_enforces_ceiling",
			initialOld:        0,
			initialTotal:      102500,
			maxKeep:           100000,
			batchSize:         1000,
			wantPruneOldCalls: 1, // 1 call returns 0
			wantPruneCeilCalls: 3, // call 1: 1000; call 2: 1000; call 3: 500
			wantFinalTotal:    100000,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			matchRepo := &stressMockRepo{
				totalRecords: tc.initialTotal,
				oldRecords:   tc.initialOld,
			}
			couponRepo := &stressMockRepo{
				totalRecords: tc.initialTotal,
				oldRecords:   tc.initialOld,
			}

			cfg := service.OfferServiceConfig{
				RetentionDays: 90,
				MaxKeep:       tc.maxKeep,
				BatchSize:     tc.batchSize,
			}

			svc := service.NewOfferService(matchRepo, couponRepo, cfg)
			if err := svc.RunRetentionCleanup(ctx); err != nil {
				t.Fatalf("RunRetentionCleanup failed: %v", err)
			}

			if matchRepo.pruneOldCalls != tc.wantPruneOldCalls {
				t.Errorf("matchRepo pruneOldCalls: got %d, want %d", matchRepo.pruneOldCalls, tc.wantPruneOldCalls)
			}
			if matchRepo.pruneCeilCalls != tc.wantPruneCeilCalls {
				t.Errorf("matchRepo pruneCeilCalls: got %d, want %d", matchRepo.pruneCeilCalls, tc.wantPruneCeilCalls)
			}
			if matchRepo.totalRecords != tc.wantFinalTotal {
				t.Errorf("matchRepo final total: got %d, want %d", matchRepo.totalRecords, tc.wantFinalTotal)
			}

			if couponRepo.pruneOldCalls != tc.wantPruneOldCalls {
				t.Errorf("couponRepo pruneOldCalls: got %d, want %d", couponRepo.pruneOldCalls, tc.wantPruneOldCalls)
			}
			if couponRepo.pruneCeilCalls != tc.wantPruneCeilCalls {
				t.Errorf("couponRepo pruneCeilCalls: got %d, want %d", couponRepo.pruneCeilCalls, tc.wantPruneCeilCalls)
			}
			if couponRepo.totalRecords != tc.wantFinalTotal {
				t.Errorf("couponRepo final total: got %d, want %d", couponRepo.totalRecords, tc.wantFinalTotal)
			}
		})
	}
}
