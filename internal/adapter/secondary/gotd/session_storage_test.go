package gotd_test

import (
	"context"
	"errors"
	"testing"

	"github.com/gotd/td/session"

	"github.com/matrocheetos/telegram-monitor/internal/adapter/secondary/crypto"
	"github.com/matrocheetos/telegram-monitor/internal/adapter/secondary/gotd"
	"github.com/matrocheetos/telegram-monitor/internal/adapter/secondary/sqlite"
	"github.com/matrocheetos/telegram-monitor/internal/port"
)

func setupTestStorage(t *testing.T) (*gotd.SessionStorage, port.CredentialRepository) {
	t.Helper()
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

	storage := gotd.NewSessionStorage(repo, cryptoSvc)
	return storage, repo
}

func TestSessionStorage_Lifecycle(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	storage, repo := setupTestStorage(t)

	_, err := storage.LoadSession(ctx)
	if !errors.Is(err, session.ErrNotFound) {
		t.Fatalf("expected session.ErrNotFound on empty db, got %v", err)
	}

	testSessionData := []byte("mtproto-binary-session-data-payload-12345")
	if err := storage.StoreSession(ctx, testSessionData); err != nil {
		t.Fatalf("StoreSession failed: %v", err)
	}

	loadedData, err := storage.LoadSession(ctx)
	if err != nil {
		t.Fatalf("LoadSession failed: %v", err)
	}
	if string(loadedData) != string(testSessionData) {
		t.Fatalf("expected %q, got %q", testSessionData, loadedData)
	}

	creds, err := repo.GetTelegramCredentials(ctx)
	if err != nil {
		t.Fatalf("GetTelegramCredentials failed: %v", err)
	}
	if string(creds.EncryptedSession) == string(testSessionData) {
		t.Fatalf("session data was stored unencrypted in repository!")
	}

	creds.PhoneMasked = "+55 11 *****-1234"
	if err := repo.SaveTelegramCredentials(ctx, creds); err != nil {
		t.Fatalf("save credentials failed: %v", err)
	}

	updatedSession := []byte("new-session-data-67890")
	if err := storage.StoreSession(ctx, updatedSession); err != nil {
		t.Fatalf("StoreSession update failed: %v", err)
	}

	updatedCreds, err := repo.GetTelegramCredentials(ctx)
	if err != nil {
		t.Fatalf("GetTelegramCredentials after update failed: %v", err)
	}
	if updatedCreds.PhoneMasked != "+55 11 *****-1234" {
		t.Fatalf("StoreSession overwrote PhoneMasked, got %q", updatedCreds.PhoneMasked)
	}

	loadedUpdated, err := storage.LoadSession(ctx)
	if err != nil {
		t.Fatalf("LoadSession updated failed: %v", err)
	}
	if string(loadedUpdated) != string(updatedSession) {
		t.Fatalf("expected %q, got %q", updatedSession, loadedUpdated)
	}
}
