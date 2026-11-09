package domain_test

import (
	"errors"
	"testing"

	"github.com/matrocheetos/telegram-monitor/internal/domain"
)

func TestNewUserInterest(t *testing.T) {
	t.Parallel()

	maxPrice := 2500.0
	invalidPrice := -10.0

	t.Run("valid interest with positive and negative tokens", func(t *testing.T) {
		t.Parallel()
		interest, err := domain.NewUserInterest(1, "switch 2 -capa -película", &maxPrice)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if interest.UserID != 1 {
			t.Errorf("expected UserID=1, got %d", interest.UserID)
		}
		if interest.RawTerm != "switch 2 -capa -película" {
			t.Errorf("expected RawTerm preserved, got %s", interest.RawTerm)
		}
		if len(interest.PositiveTokens) != 2 {
			t.Fatalf("expected 2 positive tokens, got %v", interest.PositiveTokens)
		}
		if interest.PositiveTokens[0] != "switch" || interest.PositiveTokens[1] != "2" {
			t.Errorf("unexpected positive tokens: %v", interest.PositiveTokens)
		}
		if len(interest.NegativeTokens) != 2 {
			t.Fatalf("expected 2 negative tokens, got %v", interest.NegativeTokens)
		}
		if interest.NegativeTokens[0] != "capa" || interest.NegativeTokens[1] != "pelicula" {
			t.Errorf("unexpected negative tokens: %v", interest.NegativeTokens)
		}
		if interest.MaxPrice == nil || *interest.MaxPrice != 2500.0 {
			t.Errorf("expected MaxPrice=2500.0, got %v", interest.MaxPrice)
		}
		if !interest.Active {
			t.Errorf("expected Active=true")
		}
	})

	t.Run("reject empty raw term", func(t *testing.T) {
		t.Parallel()
		_, err := domain.NewUserInterest(1, "", nil)
		if !errors.Is(err, domain.ErrEmptyInterestTerm) {
			t.Fatalf("expected ErrEmptyInterestTerm, got %v", err)
		}
	})

	t.Run("reject whitespace only term", func(t *testing.T) {
		t.Parallel()
		_, err := domain.NewUserInterest(1, "   \t\n  ", nil)
		if !errors.Is(err, domain.ErrEmptyInterestTerm) {
			t.Fatalf("expected ErrEmptyInterestTerm, got %v", err)
		}
	})

	t.Run("reject term with only negative tokens", func(t *testing.T) {
		t.Parallel()
		_, err := domain.NewUserInterest(1, "-capa -case -pelicula", nil)
		if !errors.Is(err, domain.ErrEmptyInterestTerm) {
			t.Fatalf("expected ErrEmptyInterestTerm for only-negative tokens, got %v", err)
		}
	})

	t.Run("reject term with only punctuation", func(t *testing.T) {
		t.Parallel()
		_, err := domain.NewUserInterest(1, "!@#$%^&*()", nil)
		if !errors.Is(err, domain.ErrEmptyInterestTerm) {
			t.Fatalf("expected ErrEmptyInterestTerm, got %v", err)
		}
	})

	t.Run("reject invalid user id", func(t *testing.T) {
		t.Parallel()
		_, err := domain.NewUserInterest(0, "playstation 5", nil)
		if !errors.Is(err, domain.ErrUnauthorized) {
			t.Fatalf("expected ErrUnauthorized for userID=0, got %v", err)
		}
	})

	t.Run("reject negative price", func(t *testing.T) {
		t.Parallel()
		_, err := domain.NewUserInterest(1, "rtx 4060", &invalidPrice)
		if !errors.Is(err, domain.ErrInvalidPrice) {
			t.Fatalf("expected ErrInvalidPrice, got %v", err)
		}
	})
}

