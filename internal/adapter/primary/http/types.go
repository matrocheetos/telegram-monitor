package http

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/matrocheetos/telegram-monitor/internal/adapter/primary/http/i18n"
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

type FlashMessage struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

type ViewData struct {
	Title       string
	CurrentLang string
	User        *domain.User
	CSRFToken   string
	Flash       *FlashMessage
	ActiveNav   string
	Data        any
	translator  *i18n.Translator
}

func (v ViewData) T(key string, args ...any) string {
	if v.translator == nil {
		return key
	}
	return v.translator.T(v.CurrentLang, key, args...)
}

func setFlash(w http.ResponseWriter, flashType, message string) {
	flash := FlashMessage{Type: flashType, Message: message}
	data, err := json.Marshal(flash)
	if err != nil {
		return
	}
	//nolint:gosec // flash cookie security attributes managed for reverse-proxy deployment
	http.SetCookie(w, &http.Cookie{
		Name:     "flash",
		Value:    url.QueryEscape(string(data)),
		Path:     "/",
		MaxAge:   10,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}

func getAndClearFlash(w http.ResponseWriter, r *http.Request) *FlashMessage {
	cookie, err := r.Cookie("flash")
	if err != nil || cookie.Value == "" {
		return nil
	}

	//nolint:gosec // clearing flash cookie
	http.SetCookie(w, &http.Cookie{
		Name:     "flash",
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})

	unescaped, err := url.QueryUnescape(cookie.Value)
	if err != nil {
		return nil
	}

	var flash FlashMessage
	if err := json.Unmarshal([]byte(unescaped), &flash); err != nil {
		return nil
	}
	return &flash
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

func publicTopicURL(baseURL, logicalTopic, separator string) string {
	topic := normalizeTopic(logicalTopic, separator)
	cleanBase := strings.TrimRight(baseURL, "/")
	if cleanBase == "" {
		return topic
	}
	return fmt.Sprintf("%s/%s", cleanBase, topic)
}
