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
