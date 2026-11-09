package domain

import (
	"regexp"
	"time"
)

type Role string

const (
	RoleAdmin Role = "admin"
	RoleUser  Role = "user"

	MinPasswordLength = 8
)

var usernameRegex = regexp.MustCompile(`^[a-z0-9_.-]{3,32}$`)

type User struct {
	ID           int64     `json:"id" db:"id"`
	Username     string    `json:"username" db:"username"`
	PasswordHash string    `json:"-" db:"password_hash"`
	Role         Role      `json:"role" db:"role"`
	NtfyTopic    string    `json:"ntfy_topic" db:"ntfy_topic"`
	CreatedAt    time.Time `json:"created_at" db:"created_at"`
	UpdatedAt    time.Time `json:"updated_at" db:"updated_at"`
}

func (r Role) IsValid() bool {
	return r == RoleAdmin || r == RoleUser
}

func ValidateUsername(username string) error {
	if !usernameRegex.MatchString(username) {
		return ErrInvalidUsername
	}
	return nil
}

func ValidatePassword(password string) error {
	if len(password) < MinPasswordLength {
		return ErrInvalidPassword
	}
	return nil
}

func NtfyTopicForUser(username string) string {
	return "promo/user/" + username
}

func NewUser(username, passwordHash string, role Role) (*User, error) {
	if err := ValidateUsername(username); err != nil {
		return nil, err
	}
	if !role.IsValid() {
		return nil, ErrInvalidRole
	}
	if passwordHash == "" {
		return nil, ErrInvalidPassword
	}

	now := time.Now().UTC()
	return &User{
		Username:     username,
		PasswordHash: passwordHash,
		Role:         role,
		NtfyTopic:    NtfyTopicForUser(username),
		CreatedAt:    now,
		UpdatedAt:    now,
	}, nil
}
