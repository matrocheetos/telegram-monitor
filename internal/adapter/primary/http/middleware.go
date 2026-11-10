package http

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"net/http"
	"strings"

	"github.com/matrocheetos/telegram-monitor/internal/adapter/primary/http/i18n"
	"github.com/matrocheetos/telegram-monitor/internal/domain"
	"github.com/matrocheetos/telegram-monitor/internal/port"
)

func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "same-origin")
		next.ServeHTTP(w, r)
	})
}

func I18n(tr *i18n.Translator) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var langVal string
			if qLang := r.URL.Query().Get("lang"); qLang != "" {
				langVal = qLang
			} else if cookie, err := r.Cookie("lang"); err == nil && cookie.Value != "" {
				langVal = cookie.Value
			} else {
				langVal = r.Header.Get("Accept-Language")
			}

			normalized := tr.NormalizeLang(langVal)
			ctx := i18n.WithLanguage(r.Context(), normalized)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func CSRF(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var token string
		cookie, err := r.Cookie("csrf_token")
		if err == nil && cookie.Value != "" {
			token = cookie.Value
		} else {
			tokenBytes := make([]byte, 32)
			_, _ = rand.Read(tokenBytes)
			token = hex.EncodeToString(tokenBytes)
			//nolint:gosec // csrf_token must be readable by client-side scripts
			http.SetCookie(w, &http.Cookie{
				Name:     "csrf_token",
				Value:    token,
				Path:     "/",
				SameSite: http.SameSiteLaxMode,
				HttpOnly: false,
			})
		}

		ctx := WithCSRFToken(r.Context(), token)
		r = r.WithContext(ctx)

		method := r.Method
		if method == http.MethodPost || method == http.MethodPut || method == http.MethodDelete || method == http.MethodPatch {
			// Skip CSRF check for /lang if needed or validate everywhere
			formToken := r.PostFormValue("csrf_token")
			if formToken == "" {
				formToken = r.Header.Get("X-CSRF-Token")
			}

			if subtle.ConstantTimeCompare([]byte(token), []byte(formToken)) != 1 {
				http.Error(w, "Forbidden: CSRF token mismatch", http.StatusForbidden)
				return
			}
		}

		next.ServeHTTP(w, r)
	})
}

func Auth(authUseCase port.AuthUseCase) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			cookie, err := r.Cookie("session_id")
			if err == nil && cookie.Value != "" {
				user, err := authUseCase.AuthenticateSession(r.Context(), cookie.Value)
				if err == nil && user != nil {
					r = r.WithContext(WithUser(r.Context(), user))
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

func FirstBoot(setupUseCase port.SetupUseCase) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			needsSetup, err := setupUseCase.NeedsInitialSetup(r.Context())
			if err != nil {
				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
				return
			}

			path := r.URL.Path

			if needsSetup {
				if path == "/setup" || path == "/lang" || strings.HasPrefix(path, "/static/") {
					next.ServeHTTP(w, r)
					return
				}
				http.Redirect(w, r, "/setup", http.StatusSeeOther)
				return
			}

			if path == "/setup" {
				if r.Method == http.MethodPost {
					http.Error(w, "Forbidden: setup already completed", http.StatusForbidden)
					return
				}
				http.Redirect(w, r, "/login", http.StatusSeeOther)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func RBAC(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path

		if path == "/" || path == "/login" || path == "/setup" || path == "/lang" || strings.HasPrefix(path, "/static/") {
			next.ServeHTTP(w, r)
			return
		}

		user := GetUser(r.Context())
		if user == nil {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}

		if strings.HasPrefix(path, "/admin/") {
			if user.Role != domain.RoleAdmin {
				http.Error(w, "Forbidden: admin access required", http.StatusForbidden)
				return
			}
		}

		next.ServeHTTP(w, r)
	})
}

func TelegramSetupRedirect(telegramAuthUseCase port.TelegramAuthUseCase) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			user := GetUser(r.Context())
			if user != nil && user.Role == domain.RoleAdmin && r.URL.Path == "/dashboard" {
				connected, _, err := telegramAuthUseCase.GetConnectionStatus(r.Context())
				if err == nil && !connected {
					http.Redirect(w, r, "/admin/telegram", http.StatusSeeOther)
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}
