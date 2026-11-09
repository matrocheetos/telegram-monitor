package port

import (
	"context"

	"github.com/matrocheetos/telegram-monitor/internal/domain"
)

type SetupUseCase interface {
	NeedsInitialSetup(ctx context.Context) (bool, error)
	CreateInitialAdmin(ctx context.Context, username, password string) (*domain.User, error)
}

type AuthUseCase interface {
	Login(ctx context.Context, username, password string) (*domain.User, string, error)
	Logout(ctx context.Context, token string) error
	AuthenticateSession(ctx context.Context, token string) (*domain.User, error)
}

type UserAdminUseCase interface {
	CreateUser(ctx context.Context, username, password string, role domain.Role) (*domain.User, error)
	ListUsers(ctx context.Context) ([]*domain.User, error)
	ResetUserPassword(ctx context.Context, userID int64, newPassword string) error
	DeleteUser(ctx context.Context, userID int64) error
}

type TelegramAuthUseCase interface {
	GetConnectionStatus(ctx context.Context) (connected bool, phoneMasked string, err error)
	SendLoginCode(ctx context.Context, apiID int, apiHash, phone string) (phoneCodeHash string, err error)
	VerifyLoginCode(ctx context.Context, phoneCode, password2FA string) error
	Disconnect(ctx context.Context) error
}

type ChannelUseCase interface {
	SyncChannels(ctx context.Context) error
	ListChannels(ctx context.Context) ([]*domain.TelegramChannel, error)
	UpdateChannelFlags(ctx context.Context, chatID int64, monitorProducts, monitorCoupons bool) error
}

type InterestUseCase interface {
	AddInterest(ctx context.Context, userID int64, rawTerm string, maxPrice *float64) (*domain.UserInterest, error)
	ListUserInterests(ctx context.Context, userID int64) ([]*domain.UserInterest, error)
	DeleteInterest(ctx context.Context, userID, interestID int64) error
}

type MessageProcessor interface {
	ProcessIncomingMessage(ctx context.Context, channelID int64, messageText string) error
}
