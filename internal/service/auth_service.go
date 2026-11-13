package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"github.com/matrocheetos/telegram-monitor/internal/domain"
	"github.com/matrocheetos/telegram-monitor/internal/port"
)

var (
	_ port.SetupUseCase     = (*AuthService)(nil)
	_ port.AuthUseCase      = (*AuthService)(nil)
	_ port.UserAdminUseCase = (*AuthService)(nil)
)

type AuthService struct {
	userRepo          port.UserRepository
	sessionRepo       port.SessionRepository
	hasher            port.PasswordHasher
	snapshotRebuilder port.SnapshotRebuilder
	setupMu           sync.Mutex
}

func NewAuthService(
	userRepo port.UserRepository,
	sessionRepo port.SessionRepository,
	hasher port.PasswordHasher,
	snapshotRebuilder port.SnapshotRebuilder,
) *AuthService {
	return &AuthService{
		userRepo:          userRepo,
		sessionRepo:       sessionRepo,
		hasher:            hasher,
		snapshotRebuilder: snapshotRebuilder,
	}
}

func (s *AuthService) NeedsInitialSetup(ctx context.Context) (bool, error) {
	count, err := s.userRepo.CountUsers(ctx)
	if err != nil {
		return false, fmt.Errorf("count users: %w", err)
	}
	return count == 0, nil
}

func (s *AuthService) CreateInitialAdmin(ctx context.Context, username, password string) (*domain.User, error) {
	s.setupMu.Lock()
	defer s.setupMu.Unlock()

	count, err := s.userRepo.CountUsers(ctx)
	if err != nil {
		return nil, fmt.Errorf("count users: %w", err)
	}
	if count > 0 {
		return nil, domain.ErrSetupAlreadyCompleted
	}

	if err := domain.ValidatePassword(password); err != nil {
		return nil, err
	}

	hash, err := s.hasher.HashPassword(password)
	if err != nil {
		return nil, fmt.Errorf("hash password: %w", err)
	}

	user, err := domain.NewUser(username, hash, domain.RoleAdmin)
	if err != nil {
		return nil, err
	}

	if err := s.userRepo.CreateInitialAdmin(ctx, user); err != nil {
		return nil, fmt.Errorf("create initial admin: %w", err)
	}

	return user, nil
}

func (s *AuthService) Login(ctx context.Context, username, password string) (*domain.User, string, error) {
	user, err := s.userRepo.GetUserByUsername(ctx, username)
	if err != nil {
		return nil, "", domain.ErrInvalidCredentials
	}

	valid, err := s.hasher.VerifyPassword(password, user.PasswordHash)
	if err != nil || !valid {
		return nil, "", domain.ErrInvalidCredentials
	}

	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return nil, "", fmt.Errorf("generate session token: %w", err)
	}

	token := hex.EncodeToString(tokenBytes)
	tokenHash := HashSessionToken(token)
	expiresAt := time.Now().UTC().Add(domain.SessionDuration)

	if err := s.sessionRepo.CreateSession(ctx, tokenHash, user.ID, expiresAt); err != nil {
		return nil, "", fmt.Errorf("create session: %w", err)
	}

	return user, token, nil
}

func (s *AuthService) Logout(ctx context.Context, token string) error {
	tokenHash := HashSessionToken(token)
	return s.sessionRepo.DeleteSession(ctx, tokenHash)
}

func (s *AuthService) AuthenticateSession(ctx context.Context, token string) (*domain.User, error) {
	if token == "" {
		return nil, domain.ErrUnauthorized
	}

	tokenHash := HashSessionToken(token)
	user, err := s.sessionRepo.GetSessionUser(ctx, tokenHash)
	if err != nil {
		return nil, domain.ErrUnauthorized
	}

	return user, nil
}

func (s *AuthService) CreateUser(ctx context.Context, username, password string, role domain.Role) (*domain.User, error) {
	if err := domain.ValidatePassword(password); err != nil {
		return nil, err
	}

	hash, err := s.hasher.HashPassword(password)
	if err != nil {
		return nil, fmt.Errorf("hash password: %w", err)
	}

	user, err := domain.NewUser(username, hash, role)
	if err != nil {
		return nil, err
	}

	if err := s.userRepo.CreateUser(ctx, user); err != nil {
		return nil, fmt.Errorf("create user: %w", err)
	}

	return user, nil
}

func (s *AuthService) ListUsers(ctx context.Context) ([]*domain.User, error) {
	return s.userRepo.ListUsers(ctx)
}

func (s *AuthService) ResetUserPassword(ctx context.Context, userID int64, newPassword string) error {
	if err := domain.ValidatePassword(newPassword); err != nil {
		return err
	}

	hash, err := s.hasher.HashPassword(newPassword)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}

	if err := s.userRepo.UpdatePassword(ctx, userID, hash); err != nil {
		return fmt.Errorf("update user password: %w", err)
	}

	if s.sessionRepo != nil {
		if err := s.sessionRepo.DeleteUserSessions(ctx, userID); err != nil {
			return fmt.Errorf("delete user sessions: %w", err)
		}
	}

	return nil
}

func (s *AuthService) DeleteUser(ctx context.Context, userID int64) error {
	if err := s.userRepo.DeleteUser(ctx, userID); err != nil {
		return fmt.Errorf("delete user: %w", err)
	}

	if s.snapshotRebuilder != nil {
		if err := s.snapshotRebuilder.RebuildSnapshot(ctx); err != nil {
			return fmt.Errorf("rebuild snapshot: %w", err)
		}
	}

	return nil
}

func HashSessionToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
