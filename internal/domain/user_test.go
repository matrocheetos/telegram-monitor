package domain_test

import (
	"errors"
	"testing"

	"github.com/matrocheetos/telegram-monitor/internal/domain"
)

func TestValidateUsername(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		username string
		wantErr  bool
	}{
		{name: "valid standard username", username: "mateus", wantErr: false},
		{name: "valid with dots and hyphens", username: "john.doe-99", wantErr: false},
		{name: "valid with underscore", username: "admin_user", wantErr: false},
		{name: "too short", username: "ab", wantErr: true},
		{name: "too long", username: "a01234567890123456789012345678901", wantErr: true},
		{name: "uppercase not allowed", username: "Mateus", wantErr: true},
		{name: "special characters not allowed", username: "user@domain", wantErr: true},
		{name: "spaces not allowed", username: "user name", wantErr: true},
		{name: "empty username", username: "", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := domain.ValidateUsername(tt.username)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ValidateUsername(%q) error = %v, wantErr %v", tt.username, err, tt.wantErr)
			}
		})
	}
}

func TestValidatePassword(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		password string
		wantErr  bool
	}{
		{name: "valid 8 chars", password: "password", wantErr: false},
		{name: "valid long password", password: "super-secret-password-1234", wantErr: false},
		{name: "too short 7 chars", password: "1234567", wantErr: true},
		{name: "empty password", password: "", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := domain.ValidatePassword(tt.password)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ValidatePassword(%q) error = %v, wantErr %v", tt.password, err, tt.wantErr)
			}
		})
	}
}

func TestNtfyTopicForUser(t *testing.T) {
	t.Parallel()

	got := domain.NtfyTopicForUser("mateus")
	expected := "promo/user/mateus"
	if got != expected {
		t.Fatalf("NtfyTopicForUser() = %q, want %q", got, expected)
	}
}

func TestNewUser(t *testing.T) {
	t.Parallel()

	t.Run("valid user creation", func(t *testing.T) {
		t.Parallel()
		user, err := domain.NewUser("admin", "hashed_secret_value", domain.RoleAdmin)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if user.Username != "admin" {
			t.Errorf("expected username admin, got %s", user.Username)
		}
		if user.Role != domain.RoleAdmin {
			t.Errorf("expected role admin, got %s", user.Role)
		}
		if user.NtfyTopic != "promo/user/admin" {
			t.Errorf("expected topic promo/user/admin, got %s", user.NtfyTopic)
		}
		if user.CreatedAt.IsZero() || user.UpdatedAt.IsZero() {
			t.Errorf("expected non-zero timestamps")
		}
	})

	t.Run("invalid username", func(t *testing.T) {
		t.Parallel()
		_, err := domain.NewUser("INVALID_USER", "validhash123", domain.RoleUser)
		if !errors.Is(err, domain.ErrInvalidUsername) {
			t.Fatalf("expected ErrInvalidUsername, got %v", err)
		}
	})

	t.Run("invalid role", func(t *testing.T) {
		t.Parallel()
		_, err := domain.NewUser("validuser", "validhash123", domain.Role("superuser"))
		if !errors.Is(err, domain.ErrInvalidRole) {
			t.Fatalf("expected ErrInvalidRole, got %v", err)
		}
	})

	t.Run("empty password hash", func(t *testing.T) {
		t.Parallel()
		_, err := domain.NewUser("validuser", "", domain.RoleUser)
		if !errors.Is(err, domain.ErrInvalidPassword) {
			t.Fatalf("expected ErrInvalidPassword, got %v", err)
		}
	})
}
