package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/matrocheetos/telegram-monitor/internal/domain"
	"github.com/matrocheetos/telegram-monitor/internal/port"
)

var _ port.TelegramAuthUseCase = (*TelegramAuthService)(nil)

type TelegramAuthService struct {
	telegramClient port.TelegramClientPort
	credRepo       port.CredentialRepository
}

func NewTelegramAuthService(
	telegramClient port.TelegramClientPort,
	credRepo port.CredentialRepository,
) *TelegramAuthService {
	return &TelegramAuthService{
		telegramClient: telegramClient,
		credRepo:       credRepo,
	}
}

func (s *TelegramAuthService) GetConnectionStatus(ctx context.Context) (bool, string, error) {
	creds, err := s.credRepo.GetTelegramCredentials(ctx)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return false, "", nil
		}
		return false, "", fmt.Errorf("get telegram credentials: %w", err)
	}

	connected := s.telegramClient.IsConnected()
	return connected, creds.PhoneMasked, nil
}

func (s *TelegramAuthService) SendLoginCode(ctx context.Context, apiID int, apiHash, phone string) (string, error) {
	cleanHash := strings.TrimSpace(apiHash)
	cleanPhone := strings.TrimSpace(phone)

	if apiID <= 0 || cleanHash == "" || cleanPhone == "" {
		return "", domain.ErrInvalidCredentials
	}

	hash, err := s.telegramClient.SendCode(ctx, apiID, cleanHash, cleanPhone)
	if err != nil {
		return "", fmt.Errorf("send login code: %w", err)
	}

	return hash, nil
}

func (s *TelegramAuthService) VerifyLoginCode(ctx context.Context, phoneCode, password2FA string) error {
	cleanCode := strings.TrimSpace(phoneCode)
	if cleanCode == "" {
		return domain.ErrInvalidCredentials
	}

	_, err := s.telegramClient.SignIn(ctx, cleanCode, strings.TrimSpace(password2FA))
	if err != nil {
		return fmt.Errorf("verify login code: %w", err)
	}

	return nil
}

func (s *TelegramAuthService) Disconnect(ctx context.Context) error {
	_ = s.telegramClient.StopListener(ctx)

	if err := s.credRepo.DeleteTelegramCredentials(ctx); err != nil {
		return fmt.Errorf("delete telegram credentials: %w", err)
	}

	return nil
}
