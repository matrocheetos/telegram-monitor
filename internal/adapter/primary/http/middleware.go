package http

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"log/slog"
	"math"
	"net"
	"net/http"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/matrocheetos/telegram-monitor/internal/domain"
	"github.com/matrocheetos/telegram-monitor/internal/port"
)

func isLocalEnv(appEnv string) bool {
	switch strings.ToLower(strings.TrimSpace(appEnv)) {
	case "", "local", "development", "dev", "test":
		return true
	default:
		return false
	}
}

func isProductionEnv(appEnv string) bool {
	switch strings.ToLower(strings.TrimSpace(appEnv)) {
	case "production", "prod":
		return true
	default:
		return false
	}
}

func normalizePath(rawPath string) string {
	if rawPath == "" {
		return "/"
	}
	clean := path.Clean("/" + rawPath)
	if clean != "/" {
		clean = strings.TrimRight(clean, "/")
	}
	return clean
}

func isSwaggerPath(cleanPath string) bool {
	return cleanPath == "/swagger" || strings.HasPrefix(cleanPath, "/swagger/")
}

func generateCSRFToken() (string, error) {
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(tokenBytes), nil
}

func setCSRFCookie(w http.ResponseWriter, token string, maxAge int, secure bool) {
	// #nosec G124 -- csrf_token must be readable by frontend clients for double-submit; Secure is dynamic via isSecureRequest
	http.SetCookie(w, &http.Cookie{
		Name:     "csrf_token",
		Value:    token,
		Path:     "/",
		MaxAge:   maxAge,
		SameSite: http.SameSiteLaxMode,
		HttpOnly: false,
		Secure:   secure,
	})
}

func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "same-origin")
		next.ServeHTTP(w, r)
	})
}

func CORS(allowedOriginsConfig, appEnv string) func(http.Handler) http.Handler {
	allowedSet := make(map[string]struct{})
	trimmedCfg := strings.TrimSpace(allowedOriginsConfig)
	if trimmedCfg == "" {
		if isLocalEnv(appEnv) {
			allowedSet["http://localhost:5173"] = struct{}{}
			allowedSet["http://127.0.0.1:5173"] = struct{}{}
		}
	} else {
		for _, o := range strings.Split(trimmedCfg, ",") {
			o = strings.ToLower(strings.TrimSpace(o))
			if o != "" && o != "*" {
				allowedSet[o] = struct{}{}
			}
		}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Add("Vary", "Origin")

			origin := r.Header.Get("Origin")
			if origin != "" {
				if _, allowed := allowedSet[strings.ToLower(strings.TrimSpace(origin))]; allowed {
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

func CSRF(isSecure func(*http.Request) bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var token string
			cookie, err := r.Cookie("csrf_token")
			if err == nil && cookie.Value != "" {
				token = cookie.Value
			} else {
				var genErr error
				token, genErr = generateCSRFToken()
				if genErr != nil {
					slog.ErrorContext(r.Context(), "failed to generate csrf token", "error", genErr)
					writeErrorJSON(w, http.StatusInternalServerError, "internal_error", "failed to generate csrf token")
					return
				}
				secure := false
				if isSecure != nil {
					secure = isSecure(r)
				}
				setCSRFCookie(w, token, 0, secure)
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

func FirstBoot(setupUseCase port.SetupUseCase, allowSwagger bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			needsSetup, err := setupUseCase.NeedsInitialSetup(r.Context())
			if err != nil {
				writeErrorJSON(w, http.StatusInternalServerError, "internal_error", "failed to check initial setup state")
				return
			}

			cleanPath := normalizePath(r.URL.Path)

			if needsSetup {
				if cleanPath == "/api/v1/health" ||
					cleanPath == "/api/v1/auth/session" ||
					cleanPath == "/api/v1/setup" ||
					(allowSwagger && isSwaggerPath(cleanPath)) {
					next.ServeHTTP(w, r)
					return
				}
				writeErrorJSON(w, http.StatusForbidden, "setup_required", "initial admin setup required")
				return
			}

			if cleanPath == "/api/v1/setup" {
				writeErrorJSON(w, http.StatusForbidden, "setup_already_completed", "initial setup already completed")
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func RBAC(allowSwagger bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			cleanPath := normalizePath(r.URL.Path)

			if cleanPath == "/api/v1/health" ||
				cleanPath == "/api/v1/auth/session" ||
				cleanPath == "/api/v1/setup" ||
				cleanPath == "/api/v1/auth/login" ||
				(allowSwagger && isSwaggerPath(cleanPath)) {
				next.ServeHTTP(w, r)
				return
			}

			user := GetUser(r.Context())
			if user == nil {
				writeErrorJSON(w, http.StatusUnauthorized, "unauthorized", "authentication required")
				return
			}

			if strings.HasPrefix(cleanPath, "/api/v1/admin/") || cleanPath == "/api/v1/admin" {
				if user.Role != domain.RoleAdmin {
					writeErrorJSON(w, http.StatusForbidden, "forbidden", "admin access required")
					return
				}
			}

			next.ServeHTTP(w, r)
		})
	}
}

type rateLimitBucket struct {
	count   int
	resetAt time.Time
}

type ipRateLimiter struct {
	mu      sync.Mutex
	buckets map[string]*rateLimitBucket
	limit   int
	window  time.Duration
	nowFn   func() time.Time
	ops     int
}

func newIPRateLimiter(limit int, window time.Duration) *ipRateLimiter {
	if limit <= 0 {
		limit = 15
	}
	if window <= 0 {
		window = time.Minute
	}
	return &ipRateLimiter{
		buckets: make(map[string]*rateLimitBucket),
		limit:   limit,
		window:  window,
		nowFn:   time.Now,
	}
}

func (l *ipRateLimiter) allow(ip string) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.nowFn()
	l.ops++
	if l.ops%64 == 0 || len(l.buckets) > 1000 {
		for k, b := range l.buckets {
			if !now.Before(b.resetAt) {
				delete(l.buckets, k)
			}
		}
	}

	b, exists := l.buckets[ip]
	if !exists || !now.Before(b.resetAt) {
		l.buckets[ip] = &rateLimitBucket{
			count:   1,
			resetAt: now.Add(l.window),
		}
		return true, 0
	}

	if b.count >= l.limit {
		remaining := b.resetAt.Sub(now)
		if remaining < time.Second {
			remaining = time.Second
		}
		return false, remaining
	}

	b.count++
	return true, 0
}

func extractClientIP(r *http.Request) string {
	if cfIP := strings.TrimSpace(r.Header.Get("CF-Connecting-IP")); cfIP != "" {
		if ip := net.ParseIP(cfIP); ip != nil {
			return ip.String()
		}
	}
	if xff := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); xff != "" {
		if first, _, ok := strings.Cut(xff, ","); ok {
			xff = strings.TrimSpace(first)
		}
		if ip := net.ParseIP(xff); ip != nil {
			return ip.String()
		}
	}
	if xri := strings.TrimSpace(r.Header.Get("X-Real-IP")); xri != "" {
		if ip := net.ParseIP(xri); ip != nil {
			return ip.String()
		}
	}
	host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
	if err == nil && host != "" {
		return host
	}
	if r.RemoteAddr != "" {
		return r.RemoteAddr
	}
	return "unknown"
}

func rateLimit(limiter *ipRateLimiter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			allowed, retryAfter := limiter.allow(extractClientIP(r))
			if !allowed {
				secs := int(math.Ceil(retryAfter.Seconds()))
				if secs < 1 {
					secs = 1
				}
				w.Header().Set("Retry-After", strconv.Itoa(secs))
				writeErrorJSON(w, http.StatusTooManyRequests, "rate_limit_exceeded", "too many requests, please try again later")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
