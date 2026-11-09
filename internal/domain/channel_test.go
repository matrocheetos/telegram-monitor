package domain_test

import (
	"errors"
	"testing"

	"github.com/matrocheetos/telegram-monitor/internal/domain"
)

func TestNewTelegramChannel(t *testing.T) {
	t.Parallel()

	t.Run("valid channel", func(t *testing.T) {
		t.Parallel()
		ch, err := domain.NewTelegramChannel(
			-1001234567890,
			987654321,
			"promosbrasil",
			"Promos Brasil",
			true,
			false,
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if ch.ChatID != -1001234567890 {
			t.Errorf("expected chatID -1001234567890, got %d", ch.ChatID)
		}
		if ch.AccessHash != 987654321 {
			t.Errorf("expected accessHash 987654321, got %d", ch.AccessHash)
		}
		if ch.Username != "promosbrasil" {
			t.Errorf("expected username promosbrasil, got %s", ch.Username)
		}
		if ch.Title != "Promos Brasil" {
			t.Errorf("expected title Promos Brasil, got %s", ch.Title)
		}
		if !ch.MonitorProducts || ch.MonitorCoupons {
			t.Errorf("expected MonitorProducts=true and MonitorCoupons=false")
		}
		if ch.UpdatedAt.IsZero() {
			t.Errorf("expected non-zero UpdatedAt")
		}
	})

	t.Run("zero chat id", func(t *testing.T) {
		t.Parallel()
		_, err := domain.NewTelegramChannel(0, 123, "user", "Title", true, true)
		if !errors.Is(err, domain.ErrInvalidChannel) {
			t.Fatalf("expected ErrInvalidChannel, got %v", err)
		}
	})

	t.Run("empty title", func(t *testing.T) {
		t.Parallel()
		_, err := domain.NewTelegramChannel(12345, 123, "user", "   ", true, true)
		if !errors.Is(err, domain.ErrInvalidChannel) {
			t.Fatalf("expected ErrInvalidChannel, got %v", err)
		}
	})
}

