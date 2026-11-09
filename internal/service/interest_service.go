package service

import (
	"context"
	"fmt"

	"github.com/matrocheetos/telegram-monitor/internal/domain"
	"github.com/matrocheetos/telegram-monitor/internal/port"
)

var _ port.InterestUseCase = (*InterestService)(nil)

type InterestService struct {
	interestRepo      port.InterestRepository
	snapshotRebuilder port.SnapshotRebuilder
}

func NewInterestService(
	interestRepo port.InterestRepository,
	snapshotRebuilder port.SnapshotRebuilder,
) *InterestService {
	return &InterestService{
		interestRepo:      interestRepo,
		snapshotRebuilder: snapshotRebuilder,
	}
}

func (s *InterestService) AddInterest(ctx context.Context, userID int64, rawTerm string, maxPrice *float64) (*domain.UserInterest, error) {
	interest, err := domain.NewUserInterest(userID, rawTerm, maxPrice)
	if err != nil {
		return nil, err
	}

	if err := s.interestRepo.CreateInterest(ctx, interest); err != nil {
		return nil, fmt.Errorf("create interest: %w", err)
	}

	if s.snapshotRebuilder != nil {
		if err := s.snapshotRebuilder.RebuildSnapshot(ctx); err != nil {
			return nil, fmt.Errorf("rebuild snapshot: %w", err)
		}
	}

	return interest, nil
}

func (s *InterestService) ListUserInterests(ctx context.Context, userID int64) ([]*domain.UserInterest, error) {
	if userID <= 0 {
		return nil, domain.ErrUnauthorized
	}

	return s.interestRepo.ListInterestsByUserID(ctx, userID)
}

func (s *InterestService) DeleteInterest(ctx context.Context, userID, interestID int64) error {
	if userID <= 0 {
		return domain.ErrUnauthorized
	}

	if err := s.interestRepo.DeleteInterest(ctx, userID, interestID); err != nil {
		return fmt.Errorf("delete interest: %w", err)
	}

	if s.snapshotRebuilder != nil {
		if err := s.snapshotRebuilder.RebuildSnapshot(ctx); err != nil {
			return fmt.Errorf("rebuild snapshot: %w", err)
		}
	}

	return nil
}
