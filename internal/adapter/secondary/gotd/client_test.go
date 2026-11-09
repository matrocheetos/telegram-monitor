package gotd_test

import (
	"context"
	"errors"
	"testing"

	"github.com/matrocheetos/telegram-monitor/internal/adapter/secondary/crypto"
	"github.com/matrocheetos/telegram-monitor/internal/adapter/secondary/gotd"
	"github.com/matrocheetos/telegram-monitor/internal/adapter/secondary/sqlite"
)

func TestMaskPhone(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input    string
		expected string
	}{
		{"+5511999991234", "+5511 *****-1234"},
		{"+12345678901", "+1234 *****-8901"},
		{"12345", "***"},
		{"", "***"},
	}

	for _, tc := range tests {
		actual := gotd.MaskPhone(tc.input)
		if actual != tc.expected {
			t.Errorf("MaskPhone(%q) = %q, expected %q", tc.input, actual, tc.expected)
		}
	}
}

func TestClientManager_LifecycleAndValidation(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := sqlite.OpenDB(ctx, ":memory:")
	if err != nil {
		t.Fatalf("OpenDB failed: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	repo := sqlite.NewCredentialRepository(db)
	cryptoSvc, err := crypto.NewAESGCMCrypto("secret-encryption-key-for-tests-12345")
	if err != nil {
		t.Fatalf("NewAESGCMCrypto failed: %v", err)
	}

	mgr := gotd.NewClientManager(repo, cryptoSvc, nil)

	t.Run("IsConnected initially false", func(t *testing.T) {
		if mgr.IsConnected() {
			t.Errorf("expected IsConnected to be false initially")
		}
	})

	t.Run("StartListener without credentials", func(t *testing.T) {
		err := mgr.StartListener(ctx)
		if !errors.Is(err, gotd.ErrCredentialsNotConfig) {
			t.Errorf("expected ErrCredentialsNotConfig, got %v", err)
		}
	})

	t.Run("StopListener when not running", func(t *testing.T) {
		err := mgr.StopListener(ctx)
		if err != nil {
			t.Errorf("expected nil error stopping inactive listener, got %v", err)
		}
	})

	t.Run("SignIn without pending login", func(t *testing.T) {
		_, err := mgr.SignIn(ctx, "12345", "")
		if !errors.Is(err, gotd.ErrNoPendingLogin) {
			t.Errorf("expected ErrNoPendingLogin, got %v", err)
		}
	})

	t.Run("SendCode validation", func(t *testing.T) {
		_, err := mgr.SendCode(ctx, 0, "hash", "+5511999991234")
		if err == nil {
			t.Errorf("expected error for non-positive apiID")
		}

		_, err = mgr.SendCode(ctx, 12345, "", "+5511999991234")
		if err == nil {
			t.Errorf("expected error for empty apiHash")
		}

		_, err = mgr.SendCode(ctx, 12345, "hash", "")
		if err == nil {
			t.Errorf("expected error for empty phone")
		}
	})

	t.Run("SyncDialogs when not connected", func(t *testing.T) {
		_, err := mgr.SyncDialogs(ctx)
		if !errors.Is(err, gotd.ErrNotConnected) {
			t.Errorf("expected ErrNotConnected, got %v", err)
		}
	})
}
