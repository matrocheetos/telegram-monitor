package service_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/matrocheetos/telegram-monitor/internal/domain"
	"github.com/matrocheetos/telegram-monitor/internal/port"
	"github.com/matrocheetos/telegram-monitor/internal/service"
)

type mockTelegramClient struct {
	mu          sync.Mutex
	connected   bool
	codeSent    bool
	signedIn    bool
	stopped     bool
	dialogs     []*domain.TelegramChannel
	sendCodeErr error
	signInErr   error
}

func (m *mockTelegramClient) SendCode(ctx context.Context, apiID int, apiHash, phone string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sendCodeErr != nil {
		return "", m.sendCodeErr
	}
	m.codeSent = true
	return "hash123", nil
}

func (m *mockTelegramClient) SignIn(ctx context.Context, code, password2FA string) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.signInErr != nil {
		return nil, m.signInErr
	}
	m.signedIn = true
	m.connected = true
	return []byte("session-blob"), nil
}

func (m *mockTelegramClient) IsConnected() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.connected
}

func (m *mockTelegramClient) SyncDialogs(ctx context.Context) ([]*domain.TelegramChannel, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.dialogs, nil
}

func (m *mockTelegramClient) StartListener(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.connected = true
	return nil
}

func (m *mockTelegramClient) StopListener(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stopped = true
	m.connected = false
	return nil
}

type mockCredRepo struct {
	mu    sync.Mutex
	creds *port.TelegramCredentials
}

func (m *mockCredRepo) SaveTelegramCredentials(ctx context.Context, creds *port.TelegramCredentials) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.creds = creds
	return nil
}

func (m *mockCredRepo) GetTelegramCredentials(ctx context.Context) (*port.TelegramCredentials, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.creds == nil {
		return nil, domain.ErrNotFound
	}
	return m.creds, nil
}

func (m *mockCredRepo) DeleteTelegramCredentials(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.creds = nil
	return nil
}

func TestTelegramAuthService_GetConnectionStatus(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	t.Run("no credentials returns disconnected", func(t *testing.T) {
		t.Parallel()
		client := &mockTelegramClient{}
		credRepo := &mockCredRepo{}
		svc := service.NewTelegramAuthService(client, credRepo)

		connected, phone, err := svc.GetConnectionStatus(ctx)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if connected || phone != "" {
			t.Errorf("expected false and empty phone, got %v, %q", connected, phone)
		}
	})

	t.Run("credentials exist and connected", func(t *testing.T) {
		t.Parallel()
		client := &mockTelegramClient{connected: true}
		credRepo := &mockCredRepo{
			creds: &port.TelegramCredentials{PhoneMasked: "+55 (11) 9****-9999"},
		}
		svc := service.NewTelegramAuthService(client, credRepo)

		connected, phone, err := svc.GetConnectionStatus(ctx)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !connected {
			t.Errorf("expected connected true, got false")
		}
		if phone != "+55 (11) 9****-9999" {
			t.Errorf("expected phone mask, got %s", phone)
		}
	})
}

func TestTelegramAuthService_SendLoginCode_And_Verify(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	client := &mockTelegramClient{}
	credRepo := &mockCredRepo{}
	svc := service.NewTelegramAuthService(client, credRepo)

	// Validation errors on SendLoginCode
	_, err := svc.SendLoginCode(ctx, 0, "hash", "+5511999999999")
	if !errors.Is(err, domain.ErrInvalidCredentials) {
		t.Errorf("expected ErrInvalidCredentials on zero apiID, got %v", err)
	}

	_, err = svc.SendLoginCode(ctx, 12345, "", "+5511999999999")
	if !errors.Is(err, domain.ErrInvalidCredentials) {
		t.Errorf("expected ErrInvalidCredentials on empty apiHash, got %v", err)
	}

	// Successful SendLoginCode
	codeHash, err := svc.SendLoginCode(ctx, 12345, "valid_hash", "+5511999999999")
	if err != nil {
		t.Fatalf("send code failed: %v", err)
	}
	if codeHash != "hash123" {
		t.Errorf("expected hash123, got %s", codeHash)
	}

	// Validation error on VerifyLoginCode
	err = svc.VerifyLoginCode(ctx, "", "")
	if !errors.Is(err, domain.ErrInvalidCredentials) {
		t.Errorf("expected ErrInvalidCredentials on empty code, got %v", err)
	}

	// Successful VerifyLoginCode
	if err := svc.VerifyLoginCode(ctx, "54321", "2fa_password"); err != nil {
		t.Fatalf("verify code failed: %v", err)
	}
}

func TestTelegramAuthService_Disconnect(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	client := &mockTelegramClient{connected: true}
	credRepo := &mockCredRepo{
		creds: &port.TelegramCredentials{PhoneMasked: "+55 (11) 9****-9999"},
	}
	svc := service.NewTelegramAuthService(client, credRepo)

	if err := svc.Disconnect(ctx); err != nil {
		t.Fatalf("disconnect failed: %v", err)
	}

	if credRepo.creds != nil {
		t.Errorf("expected credentials to be deleted")
	}
	if client.connected {
		t.Errorf("expected client to be disconnected")
	}
}
