package http

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/matrocheetos/telegram-monitor/internal/domain"
)

type userCtxKey struct{}
type csrfCtxKey struct{}

func WithUser(ctx context.Context, u *domain.User) context.Context {
	return context.WithValue(ctx, userCtxKey{}, u)
}

func GetUser(ctx context.Context) *domain.User {
	if u, ok := ctx.Value(userCtxKey{}).(*domain.User); ok {
		return u
	}
	return nil
}

func WithCSRFToken(ctx context.Context, token string) context.Context {
	return context.WithValue(ctx, csrfCtxKey{}, token)
}

func GetCSRFToken(ctx context.Context) string {
	if t, ok := ctx.Value(csrfCtxKey{}).(string); ok {
		return t
	}
	return ""
}

type ErrorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type ErrorEnvelope struct {
	Error ErrorDetail `json:"error"`
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	if payload == nil {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(status)
		return
	}

	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(payload); err != nil {
		slog.Error("failed to encode json response", "error", err)
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":{"code":"internal_error","message":"failed to encode response"}}` + "\n"))
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if _, err := w.Write(buf.Bytes()); err != nil {
		slog.Error("failed to write json response", "error", err)
	}
}

func writeErrorJSON(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, ErrorEnvelope{
		Error: ErrorDetail{
			Code:    code,
			Message: message,
		},
	})
}

func writeDomainError(w http.ResponseWriter, err error) {
	if err == nil {
		return
	}
	switch {
	case errors.Is(err, domain.ErrUnauthorized):
		writeErrorJSON(w, http.StatusUnauthorized, "unauthorized", err.Error())
	case errors.Is(err, domain.ErrInvalidCredentials):
		writeErrorJSON(w, http.StatusUnauthorized, "invalid_credentials", err.Error())
	case errors.Is(err, domain.ErrForbidden):
		writeErrorJSON(w, http.StatusForbidden, "forbidden", err.Error())
	case errors.Is(err, domain.ErrSetupAlreadyCompleted):
		writeErrorJSON(w, http.StatusForbidden, "setup_already_completed", err.Error())
	case errors.Is(err, domain.ErrSetupRequired):
		writeErrorJSON(w, http.StatusForbidden, "setup_required", err.Error())
	case errors.Is(err, domain.ErrNotFound):
		writeErrorJSON(w, http.StatusNotFound, "not_found", err.Error())
	case errors.Is(err, domain.ErrDuplicateEntry):
		writeErrorJSON(w, http.StatusConflict, "duplicate_entry", err.Error())
	case errors.Is(err, domain.ErrEmptyInterestTerm):
		writeErrorJSON(w, http.StatusBadRequest, "empty_interest_term", err.Error())
	case errors.Is(err, domain.ErrInvalidPrice):
		writeErrorJSON(w, http.StatusBadRequest, "invalid_price", err.Error())
	case errors.Is(err, domain.ErrTelegramNotConfigured):
		writeErrorJSON(w, http.StatusBadRequest, "telegram_not_configured", err.Error())
	case errors.Is(err, domain.ErrTelegram2FARequired):
		writeErrorJSON(w, http.StatusBadRequest, "telegram_2fa_required", err.Error())
	default:
		writeErrorJSON(w, http.StatusInternalServerError, "internal_error", "internal server error")
	}
}

func readJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	defer func() {
		_ = r.Body.Close()
	}()
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return err
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("invalid json payload: request body must contain a single JSON object")
	}
	return nil
}

func normalizeTopic(logicalTopic, separator string) string {
	if separator == "" {
		separator = "-"
	}
	trimmed := strings.Trim(logicalTopic, "/")
	parts := strings.Split(trimmed, "/")
	cleanParts := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			cleanParts = append(cleanParts, p)
		}
	}
	return strings.Join(cleanParts, separator)
}

func buildPublicURL(baseURL, logicalTopic, separator string) string {
	cleanBase := strings.TrimRight(baseURL, "/")
	normalized := normalizeTopic(logicalTopic, separator)
	if cleanBase == "" {
		return normalized
	}
	return fmt.Sprintf("%s/%s", cleanBase, url.PathEscape(normalized))
}

type UserPayload struct {
	ID        int64       `json:"id"`
	Username  string      `json:"username"`
	Role      domain.Role `json:"role"`
	NtfyTopic string      `json:"ntfyTopic"`
}

type TelegramSessionPayload struct {
	Connected   bool   `json:"connected"`
	PhoneMasked string `json:"phoneMasked"`
}

type SessionResponse struct {
	NeedsInitialSetup bool                    `json:"needsInitialSetup"`
	Authenticated     bool                    `json:"authenticated"`
	CSRFToken         string                  `json:"csrfToken"`
	User              *UserPayload            `json:"user"`
	Telegram          *TelegramSessionPayload `json:"telegram"`
}

type SetupRequest struct {
	Username        string `json:"username"`
	Password        string `json:"password"`
	PasswordConfirm string `json:"passwordConfirm"`
}

type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type LoginResponse struct {
	Authenticated bool                    `json:"authenticated"`
	CSRFToken     string                  `json:"csrfToken,omitempty"`
	User          UserPayload             `json:"user"`
	Telegram      *TelegramSessionPayload `json:"telegram"`
}

type InterestItem struct {
	ID             int64     `json:"id"`
	RawTerm        string    `json:"rawTerm"`
	PositiveTokens []string  `json:"positiveTokens"`
	NegativeTokens []string  `json:"negativeTokens"`
	MaxPrice       *float64  `json:"maxPrice"`
	Active         bool      `json:"active"`
	CreatedAt      time.Time `json:"createdAt"`
}

type CreateInterestRequest struct {
	Term     string   `json:"term"`
	MaxPrice *float64 `json:"maxPrice"`
}

type DashboardResponse struct {
	ActiveChannelsCount int            `json:"activeChannelsCount"`
	UserTopic           string         `json:"userTopic"`
	UserNormalizedTopic string         `json:"userNormalizedTopic"`
	UserPublicURL       string         `json:"userPublicUrl"`
	CouponsTopic        string         `json:"couponsTopic"`
	CouponsPublicURL    string         `json:"couponsPublicUrl"`
	Interests           []InterestItem `json:"interests"`
}

type OfferProductItem struct {
	ID             int64     `json:"id"`
	InterestTerm   string    `json:"interestTerm"`
	ChannelDisplay string    `json:"channelDisplay"`
	ExtractedPrice *float64  `json:"extractedPrice"`
	FormattedPrice string    `json:"formattedPrice"`
	MessageText    string    `json:"messageText"`
	OfferURL       string    `json:"offerUrl"`
	CreatedAt      time.Time `json:"createdAt"`
}

type OfferCouponItem struct {
	ID             int64     `json:"id"`
	Code           string    `json:"code"`
	StoreName      string    `json:"storeName"`
	StoreURL       string    `json:"storeUrl"`
	MessageText    string    `json:"messageText"`
	ChannelDisplay string    `json:"channelDisplay"`
	CreatedAt      time.Time `json:"createdAt"`
}

type OffersResponse struct {
	ProductsCount int                `json:"productsCount"`
	CouponsCount  int                `json:"couponsCount"`
	Products      []OfferProductItem `json:"products"`
	Coupons       []OfferCouponItem  `json:"coupons"`
}

type TelegramStatusResponse struct {
	Connected     bool   `json:"connected"`
	PhoneMasked   string `json:"phoneMasked"`
	ChannelsCount int    `json:"channelsCount"`
}

type TelegramSendCodeRequest struct {
	APIID   int    `json:"apiId"`
	APIHash string `json:"apiHash"`
	Phone   string `json:"phone"`
}

type TelegramSendCodeResponse struct {
	CodeSent      bool   `json:"codeSent"`
	PhoneCodeHash string `json:"phoneCodeHash"`
}

type TelegramVerifyRequest struct {
	PhoneCode   string `json:"phoneCode"`
	Password2FA string `json:"password2FA"`
}

type ChannelItem struct {
	ChatID          int64     `json:"chatId"`
	Username        string    `json:"username"`
	Title           string    `json:"title"`
	MonitorProducts bool      `json:"monitorProducts"`
	MonitorCoupons  bool      `json:"monitorCoupons"`
	UpdatedAt       time.Time `json:"updatedAt"`
}

type AdminChannelsResponse struct {
	Connected             bool          `json:"connected"`
	PhoneMasked           string        `json:"phoneMasked"`
	TotalChannelsCount    int           `json:"totalChannelsCount"`
	CouponsChannelsCount  int           `json:"couponsChannelsCount"`
	ProductsChannelsCount int           `json:"productsChannelsCount"`
	AdminProductTopic     string        `json:"adminProductTopic"`
	CouponsTopic          string        `json:"couponsTopic"`
	Channels              []ChannelItem `json:"channels"`
}

type UpdateChannelItem struct {
	ChatID          int64 `json:"chatId"`
	MonitorProducts bool  `json:"monitorProducts"`
	MonitorCoupons  bool  `json:"monitorCoupons"`
}

type UpdateChannelsRequest struct {
	Channels []UpdateChannelItem `json:"channels"`
}

type AdminUserItem struct {
	ID                   int64       `json:"id"`
	Username             string      `json:"username"`
	Role                 domain.Role `json:"role"`
	NtfyTopic            string      `json:"ntfyTopic"`
	NormalizedNtfyTopic  string      `json:"normalizedNtfyTopic"`
	ActiveInterestsCount int         `json:"activeInterestsCount"`
	CreatedAt            time.Time   `json:"createdAt"`
}

type CreateUserRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Role     string `json:"role"`
}

type ResetPasswordRequest struct {
	Password string `json:"password"`
}

type HealthResponse struct {
	Status string `json:"status" example:"ok"`
}

type StatusResponse struct {
	Status string `json:"status" example:"disconnected"`
}

type UpdatedResponse struct {
	Updated bool `json:"updated" example:"true"`
}

type AdminChannelsListResponse struct {
	Channels []ChannelItem `json:"channels"`
}
