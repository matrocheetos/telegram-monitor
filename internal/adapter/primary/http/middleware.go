package http

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"net/http"
	"strings"

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

func CORS(allowedOriginsConfig string) func(http.Handler) http.Handler {
	originsList := make([]string, 0)
	for _, o := range strings.Split(allowedOriginsConfig, ",") {
		o = strings.TrimSpace(o)
		if o != "" {
			originsList = append(originsList, o)
		}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if origin != "" {
				allowed := false
				if len(originsList) == 0 {
					allowed = true
				} else {
					for _, o := range originsList {
						if o == "*" || strings.EqualFold(o, origin) {
							allowed = true
							break
						}
					}
				}

				if allowed {
					w.Header().Set("Access-Control-Allow-Origin", origin)
					w.Header().Set("Access-Control-Allow-Credentials", "true")
					w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
					w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-CSRF-Token, Accept-Language")
				}
			}

			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}

			next.ServeHTTP(w, r)
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
			//nolint:gosec // csrf_token must be readable by frontend clients for double-submit
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
			headerToken := r.Header.Get("X-CSRF-Token")
			if headerToken == "" || subtle.ConstantTimeCompare([]byte(token), []byte(headerToken)) != 1 {
				writeErrorJSON(w, http.StatusForbidden, "invalid_csrf_token", "invalid or missing csrf token")
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
				writeErrorJSON(w, http.StatusInternalServerError, "internal_error", "failed to check initial setup state")
				return
			}

			path := strings.TrimRight(r.URL.Path, "/")
			if path == "" {
				path = "/"
			}

			if needsSetup {
				if path == "/api/v1/health" || path == "/api/v1/auth/session" || path == "/api/v1/setup" {
					next.ServeHTTP(w, r)
					return
				}
				writeErrorJSON(w, http.StatusForbidden, "setup_required", "initial admin setup required")
				return
			}

			if path == "/api/v1/setup" {
				writeErrorJSON(w, http.StatusForbidden, "setup_already_completed", "initial setup already completed")
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func RBAC(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimRight(r.URL.Path, "/")
		if path == "" {
			path = "/"
		}

		if path == "/api/v1/health" || path == "/api/v1/auth/session" || path == "/api/v1/setup" || path == "/api/v1/auth/login" {
			next.ServeHTTP(w, r)
			return
		}

		user := GetUser(r.Context())
		if user == nil {
			writeErrorJSON(w, http.StatusUnauthorized, "unauthorized", "authentication required")
			return
		}

		if strings.HasPrefix(path, "/api/v1/admin/") || path == "/api/v1/admin" {
			if user.Role != domain.RoleAdmin {
				writeErrorJSON(w, http.StatusForbidden, "forbidden", "admin access required")
				return
			}
		}

		next.ServeHTTP(w, r)
	})
}
