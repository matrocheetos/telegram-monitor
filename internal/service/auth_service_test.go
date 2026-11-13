package service_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/matrocheetos/telegram-monitor/internal/domain"
	"github.com/matrocheetos/telegram-monitor/internal/service"
)

type mockPasswordHasher struct{}

func (m *mockPasswordHasher) HashPassword(password string) (string, error) {
	return "hashed:" + password, nil
}

func (m *mockPasswordHasher) VerifyPassword(password, hash string) (bool, error) {
	return hash == "hashed:"+password, nil
}

type mockSessionRepo struct {
	mu       sync.Mutex
	sessions map[string]*domain.User
}

func newMockSessionRepo() *mockSessionRepo {
	return &mockSessionRepo{sessions: make(map[string]*domain.User)}
}

func (m *mockSessionRepo) CreateSession(ctx context.Context, tokenHash string, userID int64, expiresAt time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessions[tokenHash] = &domain.User{ID: userID, Username: "testuser"}
	return nil
}

func (m *mockSessionRepo) GetSessionUser(ctx context.Context, tokenHash string) (*domain.User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	u, ok := m.sessions[tokenHash]
	if !ok {
		return nil, domain.ErrUnauthorized
	}
	return u, nil
}

func (m *mockSessionRepo) DeleteSession(ctx context.Context, tokenHash string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.sessions, tokenHash)
	return nil
}

func (m *mockSessionRepo) DeleteUserSessions(ctx context.Context, userID int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for k, u := range m.sessions {
		if u.ID == userID {
			delete(m.sessions, k)
		}
	}
	return nil
}

func (m *mockSessionRepo) DeleteExpiredSessions(ctx context.Context) error {
	return nil
}

type mockSnapshotRebuilder struct {
	mu    sync.Mutex
	calls int
}

func (m *mockSnapshotRebuilder) RebuildSnapshot(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls++
	return nil
}

func TestAuthService_NeedsInitialSetup(t *testing.T) {
	t.Parallel()

	userRepo := &mockUserRepo{}
	sessionRepo := newMockSessionRepo()
	hasher := &mockPasswordHasher{}
	svc := service.NewAuthService(userRepo, sessionRepo, hasher, nil)

	ctx := context.Background()
	needs, err := svc.NeedsInitialSetup(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !needs {
		t.Errorf("expected needs setup true, got false")
	}

	_ = userRepo.CreateUser(ctx, &domain.User{Username: "admin"})
	needsAfter, err := svc.NeedsInitialSetup(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if needsAfter {
		t.Errorf("expected needs setup false, got true")
	}
}

func TestAuthService_CreateInitialAdmin(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	t.Run("success when zero users", func(t *testing.T) {
		t.Parallel()
		userRepo := &mockUserRepo{}
		sessionRepo := newMockSessionRepo()
		hasher := &mockPasswordHasher{}
		svc := service.NewAuthService(userRepo, sessionRepo, hasher, nil)

		user, err := svc.CreateInitialAdmin(ctx, "admin", "password123")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if user.Role != domain.RoleAdmin {
			t.Errorf("expected role admin, got %s", user.Role)
		}
		if user.Username != "admin" {
			t.Errorf("expected username admin, got %s", user.Username)
		}
	})

	t.Run("blocked when users already exist", func(t *testing.T) {
		t.Parallel()
		userRepo := &mockUserRepo{
			users: []*domain.User{{ID: 1, Username: "existing"}},
		}
		sessionRepo := newMockSessionRepo()
		hasher := &mockPasswordHasher{}
		svc := service.NewAuthService(userRepo, sessionRepo, hasher, nil)

		_, err := svc.CreateInitialAdmin(ctx, "admin2", "password123")
		if !errors.Is(err, domain.ErrSetupAlreadyCompleted) {
			t.Errorf("expected ErrSetupAlreadyCompleted, got %v", err)
		}
	})

	t.Run("fails on short password", func(t *testing.T) {
		t.Parallel()
		userRepo := &mockUserRepo{}
		sessionRepo := newMockSessionRepo()
		hasher := &mockPasswordHasher{}
		svc := service.NewAuthService(userRepo, sessionRepo, hasher, nil)

		_, err := svc.CreateInitialAdmin(ctx, "admin", "short")
		if !errors.Is(err, domain.ErrInvalidPassword) {
			t.Errorf("expected ErrInvalidPassword, got %v", err)
		}
	})
}

func TestAuthService_Login_And_Session(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	userRepo := &mockUserRepo{}
	sessionRepo := newMockSessionRepo()
	hasher := &mockPasswordHasher{}
	rebuilder := &mockSnapshotRebuilder{}
	svc := service.NewAuthService(userRepo, sessionRepo, hasher, rebuilder)

	_, err := svc.CreateInitialAdmin(ctx, "john_doe", "supersecret123")
	if err != nil {
		t.Fatalf("setup admin failed: %v", err)
	}

	// Successful login
	user, token, err := svc.Login(ctx, "john_doe", "supersecret123")
	if err != nil {
		t.Fatalf("login failed: %v", err)
	}
	if user.Username != "john_doe" {
		t.Errorf("expected username john_doe, got %s", user.Username)
	}
	if token == "" {
		t.Errorf("expected non-empty token")
	}

	// Authenticate valid token
	sessionUser, err := svc.AuthenticateSession(ctx, token)
	if err != nil {
		t.Fatalf("authenticate session failed: %v", err)
	}
	if sessionUser.ID != user.ID {
		t.Errorf("expected user id %d, got %d", user.ID, sessionUser.ID)
	}

	// Wrong password
	_, _, err = svc.Login(ctx, "john_doe", "wrongpassword")
	if !errors.Is(err, domain.ErrInvalidCredentials) {
		t.Errorf("expected ErrInvalidCredentials, got %v", err)
	}

	// Unknown user
	_, _, err = svc.Login(ctx, "nonexistent", "supersecret123")
	if !errors.Is(err, domain.ErrInvalidCredentials) {
		t.Errorf("expected ErrInvalidCredentials, got %v", err)
	}

	// Logout
	if err := svc.Logout(ctx, token); err != nil {
		t.Fatalf("logout failed: %v", err)
	}

	// Authenticate after logout
	_, err = svc.AuthenticateSession(ctx, token)
	if !errors.Is(err, domain.ErrUnauthorized) {
		t.Errorf("expected ErrUnauthorized, got %v", err)
	}
}

func TestAuthService_UserAdminCRUD(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	userRepo := &mockUserRepo{}
	sessionRepo := newMockSessionRepo()
	hasher := &mockPasswordHasher{}
	rebuilder := &mockSnapshotRebuilder{}
	svc := service.NewAuthService(userRepo, sessionRepo, hasher, rebuilder)

	// Create user
	created, err := svc.CreateUser(ctx, "alice", "securepass1", domain.RoleUser)
	if err != nil {
		t.Fatalf("create user failed: %v", err)
	}
	if created.Role != domain.RoleUser {
		t.Errorf("expected role user, got %s", created.Role)
	}

	// List users
	users, err := svc.ListUsers(ctx)
	if err != nil {
		t.Fatalf("list users failed: %v", err)
	}
	if len(users) != 1 {
		t.Fatalf("expected 1 user, got %d", len(users))
	}

	// Reset password
	if err := svc.ResetUserPassword(ctx, created.ID, "newpass123"); err != nil {
		t.Fatalf("reset password failed: %v", err)
	}

	// Delete user
	if err := svc.DeleteUser(ctx, created.ID); err != nil {
		t.Fatalf("delete user failed: %v", err)
	}

	rebuilder.mu.Lock()
	calls := rebuilder.calls
	rebuilder.mu.Unlock()
	if calls != 1 {
		t.Errorf("expected 1 rebuild snapshot call, got %d", calls)
	}
}

func TestAuthService_CreateInitialAdmin_Concurrency(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	userRepo := &mockUserRepo{}
	sessionRepo := newMockSessionRepo()
	hasher := &mockPasswordHasher{}
	svc := service.NewAuthService(userRepo, sessionRepo, hasher, nil)

	const goroutines = 10
	var wg sync.WaitGroup
	errs := make(chan error, goroutines)

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := svc.CreateInitialAdmin(ctx, "admin", "password123")
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)

	successCount := 0
	alreadyCompletedCount := 0
	for err := range errs {
		switch {
		case err == nil:
			successCount++
		case errors.Is(err, domain.ErrSetupAlreadyCompleted):
			alreadyCompletedCount++
		default:
			t.Fatalf("unexpected error during concurrent setup: %v", err)
		}
	}

	if successCount != 1 {
		t.Fatalf("expected exactly 1 setup success, got %d", successCount)
	}
	if alreadyCompletedCount != goroutines-1 {
		t.Fatalf("expected %d ErrSetupAlreadyCompleted errors, got %d", goroutines-1, alreadyCompletedCount)
	}
}

func TestAuthService_ResetUserPassword_RevokesSessions(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	userRepo := &mockUserRepo{}
	sessionRepo := newMockSessionRepo()
	hasher := &mockPasswordHasher{}
	svc := service.NewAuthService(userRepo, sessionRepo, hasher, nil)

	u, err := svc.CreateUser(ctx, "bob", "initialpass123", domain.RoleUser)
	if err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}

	_, token1, err := svc.Login(ctx, "bob", "initialpass123")
	if err != nil {
		t.Fatalf("Login 1 failed: %v", err)
	}
	_, token2, err := svc.Login(ctx, "bob", "initialpass123")
	if err != nil {
		t.Fatalf("Login 2 failed: %v", err)
	}

	if _, err := svc.AuthenticateSession(ctx, token1); err != nil {
		t.Fatalf("expected token1 valid before reset, got %v", err)
	}

	if err := svc.ResetUserPassword(ctx, u.ID, "newpass456"); err != nil {
		t.Fatalf("ResetUserPassword failed: %v", err)
	}

	if _, err := svc.AuthenticateSession(ctx, token1); !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatalf("expected token1 revoked after password reset, got %v", err)
	}
	if _, err := svc.AuthenticateSession(ctx, token2); !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatalf("expected token2 revoked after password reset, got %v", err)
	}

	if _, _, err := svc.Login(ctx, "bob", "newpass456"); err != nil {
		t.Fatalf("expected login with new password to succeed, got %v", err)
	}
}
