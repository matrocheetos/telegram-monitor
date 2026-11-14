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

	t.Run("filters portuguese stopwords while preserving model tokens", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			name     string
			rawTerm  string
			wantPos  []string
			wantErr  error
		}{
			{
				name:    "filters prepositions",
				rawTerm: "controle para ps5",
				wantPos: []string{"controle", "ps5"},
			},
			{
				name:    "filters de and para in monitor query",
				rawTerm: "monitor de 27 para pc",
				wantPos: []string{"monitor", "27", "pc"},
			},
			{
				name:    "preserves alphanumeric model suffixes",
				rawTerm: "iphone 16e",
				wantPos: []string{"iphone", "16e"},
			},
			{
				name:    "preserves cpu and motherboard skus",
				rawTerm: "ryzen 5 5500f a520m ddr5",
				wantPos: []string{"ryzen", "5", "5500f", "a520m", "ddr5"},
			},
			{
				name:    "preserves galaxy s25 and intel hx",
				rawTerm: "galaxy s25 i5 13450hx",
				wantPos: []string{"galaxy", "s25", "i5", "13450hx"},
			},
			{
				name:    "preserves m2 hardware form factor",
				rawTerm: "ssd m.2 512gb",
				wantPos: []string{"ssd", "m2", "512", "gb"},
			},
			{
				name:    "rejects stopword only term por",
				rawTerm: "por",
				wantErr: domain.ErrEmptyInterestTerm,
			},
			{
				name:    "rejects multiple stopwords only",
				rawTerm: "para o de e com",
				wantErr: domain.ErrEmptyInterestTerm,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()
				got, err := domain.NewUserInterest(1, tt.rawTerm, nil)
				if tt.wantErr != nil {
					if !errors.Is(err, tt.wantErr) {
						t.Fatalf("NewUserInterest(%q) err = %v, want %v", tt.rawTerm, err, tt.wantErr)
					}
					return
				}
				if err != nil {
					t.Fatalf("NewUserInterest(%q) unexpected err: %v", tt.rawTerm, err)
				}
				if len(got.PositiveTokens) != len(tt.wantPos) {
					t.Fatalf("PositiveTokens = %v, want %v", got.PositiveTokens, tt.wantPos)
				}
				for i, tok := range tt.wantPos {
					if got.PositiveTokens[i] != tok {
						t.Errorf("PositiveTokens[%d] = %q, want %q", i, got.PositiveTokens[i], tok)
					}
				}
			})
		}
	})
}

