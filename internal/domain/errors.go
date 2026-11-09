package domain

import "errors"

var (
	ErrUnauthorized           = errors.New("domain: unauthorized")
	ErrForbidden              = errors.New("domain: forbidden")
	ErrNotFound               = errors.New("domain: not found")
	ErrSetupAlreadyCompleted  = errors.New("domain: setup already completed")
	ErrSetupRequired          = errors.New("domain: setup required")
	ErrTelegramNotConfigured  = errors.New("domain: telegram not configured")
	ErrTelegram2FARequired    = errors.New("domain: telegram 2fa required")
	ErrEmptyInterestTerm      = errors.New("domain: empty interest term")
	ErrDuplicateEntry         = errors.New("domain: duplicate entry")
	ErrInvalidCredentials     = errors.New("domain: invalid credentials")
	ErrInvalidPrice           = errors.New("domain: invalid price")
	ErrInvalidUsername        = errors.New("domain: invalid username")
	ErrInvalidPassword        = errors.New("domain: invalid password")
	ErrInvalidRole            = errors.New("domain: invalid role")
	ErrInvalidChannel         = errors.New("domain: invalid channel")
)
