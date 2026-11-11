package port

import (
	"context"
	"time"

	"github.com/matrocheetos/telegram-monitor/internal/domain"
)

type TelegramCredentials struct {
	ID               int64     `db:"id"`
	EncryptedAPIID   []byte    `db:"encrypted_api_id"`
	EncryptedAPIHash []byte    `db:"encrypted_api_hash"`
	EncryptedSession []byte    `db:"encrypted_session"`
	PhoneMasked      string    `db:"phone_masked"`
	UpdatedAt        time.Time `db:"updated_at"`
}

type UserRepository interface {
	CountUsers(ctx context.Context) (int, error)
	CreateUser(ctx context.Context, user *domain.User) error
	GetUserByID(ctx context.Context, id int64) (*domain.User, error)
	GetUserByUsername(ctx context.Context, username string) (*domain.User, error)
	ListUsers(ctx context.Context) ([]*domain.User, error)
	UpdatePassword(ctx context.Context, id int64, passwordHash string) error
	DeleteUser(ctx context.Context, id int64) error
}

type SessionRepository interface {
	CreateSession(ctx context.Context, tokenHash string, userID int64, expiresAt time.Time) error
	GetSessionUser(ctx context.Context, tokenHash string) (*domain.User, error)
	DeleteSession(ctx context.Context, tokenHash string) error
	DeleteExpiredSessions(ctx context.Context) error
}

type CredentialRepository interface {
	SaveTelegramCredentials(ctx context.Context, creds *TelegramCredentials) error
	GetTelegramCredentials(ctx context.Context) (*TelegramCredentials, error)
	DeleteTelegramCredentials(ctx context.Context) error
}

type ChannelRepository interface {
	UpsertChannel(ctx context.Context, ch *domain.TelegramChannel) error
	GetChannel(ctx context.Context, chatID int64) (*domain.TelegramChannel, error)
	ListChannels(ctx context.Context) ([]*domain.TelegramChannel, error)
	UpdateChannelFlags(ctx context.Context, chatID int64, monitorProducts, monitorCoupons bool) error
}

type InterestRepository interface {
	CreateInterest(ctx context.Context, interest *domain.UserInterest) error
	ListInterestsByUserID(ctx context.Context, userID int64) ([]*domain.UserInterest, error)
	ListAllActiveInterests(ctx context.Context) ([]*domain.UserInterest, error)
	DeleteInterest(ctx context.Context, userID, id int64) error
}

type MatchHistoryRepository interface {
	RecordProductMatch(ctx context.Context, userID int64, interestID *int64, channelID int64, price *float64, text string) error
	ListUserMatchedProducts(ctx context.Context, userID int64, searchQuery string, limit, offset int) ([]domain.UserMatchedProduct, int, error)
	PruneOlderThan(ctx context.Context, cutoff time.Time, batchSize int) (int64, error)
	PruneExcessAbove(ctx context.Context, maxKeep int64, batchSize int) (int64, error)
	CountTotal(ctx context.Context) (int64, error)
}

type CouponRepository interface {
	RecordCoupon(ctx context.Context, code, storeName, storeURL string, channelID int64, text string) error
	HasRecentCoupon(ctx context.Context, code, storeName string, since time.Time) (bool, error)
	ListExtractedCoupons(ctx context.Context, searchQuery string, limit, offset int) ([]domain.ExtractedCoupon, int, error)
	PruneOlderThan(ctx context.Context, cutoff time.Time, batchSize int) (int64, error)
	PruneExcessAbove(ctx context.Context, maxKeep int64, batchSize int) (int64, error)
	CountTotal(ctx context.Context) (int64, error)
}

type CryptoService interface {
	Encrypt(plaintext []byte) ([]byte, error)
	Decrypt(ciphertext []byte) ([]byte, error)
}

type PasswordHasher interface {
	HashPassword(password string) (string, error)
	VerifyPassword(password, hash string) (bool, error)
}

type NotificationGateway interface {
	SendProductNotification(ctx context.Context, username, offerURL string, price *float64, text string) error
	SendCouponNotification(ctx context.Context, coupon domain.Coupon) error
}

type TelegramClientPort interface {
	SendCode(ctx context.Context, apiID int, apiHash, phone string) (phoneCodeHash string, err error)
	SignIn(ctx context.Context, code, password2FA string) ([]byte, error)
	IsConnected() bool
	SyncDialogs(ctx context.Context) ([]*domain.TelegramChannel, error)
	StartListener(ctx context.Context) error
	StopListener(ctx context.Context) error
}
