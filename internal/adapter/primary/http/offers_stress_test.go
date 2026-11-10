package http_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	adapterHTTP "github.com/matrocheetos/telegram-monitor/internal/adapter/primary/http"
	"github.com/matrocheetos/telegram-monitor/internal/domain"
)

type failingOfferUseCase struct {
	fail bool
}

func (f *failingOfferUseCase) ListUserMatchedProducts(ctx context.Context, userID int64, searchQuery string, limit, offset int) ([]domain.UserMatchedProduct, int, error) {
	if f.fail {
		return nil, 0, errors.New("simulated database failure")
	}
	return []domain.UserMatchedProduct{}, 0, nil
}

func (f *failingOfferUseCase) ListExtractedCoupons(ctx context.Context, searchQuery string, limit, offset int) ([]domain.ExtractedCoupon, int, error) {
	if f.fail {
		return nil, 0, errors.New("simulated coupon query failure")
	}
	return []domain.ExtractedCoupon{}, 0, nil
}

func (f *failingOfferUseCase) RunRetentionCleanup(ctx context.Context) error {
	return nil
}

func TestOffers_ConcurrentLoad(t *testing.T) {
	ctx := context.Background()
	fix := setupFixture(t, false, true)
	userCookie := newSessionCookie(fix.regularToken)

	var wg sync.WaitGroup
	workers := 20
	requestsPerWorker := 25

	errChan := make(chan error, workers*requestsPerWorker)

	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for r := 0; r < requestsPerWorker; r++ {
				url := "/api/v1/offers?tab=products&limit=10"
				if r%2 == 0 {
					url = "/api/v1/offers?q=switch"
				}
				req := newJSONRequest(ctx, http.MethodGet, url, nil, userCookie, "")
				rec := httptest.NewRecorder()
				fix.server.ServeHTTP(rec, req)

				if rec.Code != http.StatusOK {
					errChan <- fmt.Errorf("worker %d req %d got status %d", workerID, r, rec.Code)
					return
				}

				var resp map[string]any
				if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
					errChan <- fmt.Errorf("worker %d req %d decode error: %w", workerID, r, err)
					return
				}
			}
		}(w)
	}

	wg.Wait()
	close(errChan)

	for err := range errChan {
		t.Fatalf("concurrent test error: %v", err)
	}
}

func TestOffers_ResilienceOnFailure(t *testing.T) {
	ctx := context.Background()
	adminUser := &domain.User{
		ID:        1,
		Username:  "admin",
		Role:      domain.RoleAdmin,
		NtfyTopic: "promo/user/admin",
	}

	setupUC := &mockSetupUseCase{needsSetup: false}
	authUC := &mockAuthUseCase{
		sessions: map[string]*domain.User{
			"admin-token": adminUser,
		},
	}
	userAdminUC := &mockUserAdminUseCase{}
	tgAuthUC := &mockTelegramAuthUseCase{}
	channelUC := &mockChannelUseCase{}
	interestUC := &mockInterestUseCase{}
	failingOfferUC := &failingOfferUseCase{fail: true}

	server, err := adapterHTTP.NewServer(
		adapterHTTP.Config{
			Port: 8080,
		},
		setupUC,
		authUC,
		userAdminUC,
		tgAuthUC,
		channelUC,
		interestUC,
		failingOfferUC,
	)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	adminCookie := newSessionCookie("admin-token")
	req := newJSONRequest(ctx, http.MethodGet, "/api/v1/offers", nil, adminCookie, "")
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 on database failure, got %d", rec.Code)
	}

	var errResp adapterHTTP.ErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &errResp); err != nil {
		t.Fatalf("failed to parse error envelope: %v", err)
	}
	if errResp.Error.Code != "internal_error" {
		t.Fatalf("expected error code internal_error, got %s", errResp.Error.Code)
	}
}
