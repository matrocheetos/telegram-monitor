package http

import (
	"net/http"
	"strings"
	"time"

	_ "github.com/matrocheetos/telegram-monitor/docs"
	"github.com/matrocheetos/telegram-monitor/internal/port"
	httpSwagger "github.com/swaggo/http-swagger"
)

type Config struct {
	Port               int
	NtfyURL            string
	NtfyTopicSeparator string
	NtfyTopicPrefix    string
	CORSAllowedOrigins string
	AppEnv             string
	CookieSecure       bool
	RateLimitRequests  int
	RateLimitWindow    time.Duration
}

type Server struct {
	port                int
	ntfyURL             string
	ntfyTopicSeparator  string
	ntfyTopicPrefix     string
	appEnv              string
	cookieSecure        bool
	rateLimiter         *ipRateLimiter
	setupUseCase        port.SetupUseCase
	authUseCase         port.AuthUseCase
	userAdminUseCase    port.UserAdminUseCase
	telegramAuthUseCase port.TelegramAuthUseCase
	channelUseCase      port.ChannelUseCase
	interestUseCase     port.InterestUseCase
	offerHistoryUseCase port.OfferHistoryUseCase
	handler             http.Handler
}

func NewServer(
	cfg Config,
	setupUseCase port.SetupUseCase,
	authUseCase port.AuthUseCase,
	userAdminUseCase port.UserAdminUseCase,
	telegramAuthUseCase port.TelegramAuthUseCase,
	channelUseCase port.ChannelUseCase,
	interestUseCase port.InterestUseCase,
	offerHistoryUseCase port.OfferHistoryUseCase,
) (*Server, error) {
	sep := cfg.NtfyTopicSeparator
	if sep == "" {
		sep = "-"
	}

	portNum := cfg.Port
	if portNum <= 0 {
		portNum = 8080
	}

	s := &Server{
		port:                portNum,
		ntfyURL:             cfg.NtfyURL,
		ntfyTopicSeparator:  sep,
		ntfyTopicPrefix:     cfg.NtfyTopicPrefix,
		appEnv:              cfg.AppEnv,
		cookieSecure:        cfg.CookieSecure,
		rateLimiter:         newIPRateLimiter(cfg.RateLimitRequests, cfg.RateLimitWindow),
		setupUseCase:        setupUseCase,
		authUseCase:         authUseCase,
		userAdminUseCase:    userAdminUseCase,
		telegramAuthUseCase: telegramAuthUseCase,
		channelUseCase:      channelUseCase,
		interestUseCase:     interestUseCase,
		offerHistoryUseCase: offerHistoryUseCase,
	}

	allowSwagger := !isProductionEnv(cfg.AppEnv)
	mux := http.NewServeMux()
	s.registerRoutes(mux, allowSwagger)

	// Incoming request order: SecurityHeaders -> CORS -> CSRF -> Auth -> FirstBoot -> RBAC -> mux
	var h http.Handler = mux
	h = RBAC(allowSwagger)(h)
	h = FirstBoot(setupUseCase, allowSwagger)(h)
	h = Auth(authUseCase)(h)
	h = CSRF(s.isSecureRequest)(h)
	h = CORS(cfg.CORSAllowedOrigins, cfg.AppEnv)(h)
	h = SecurityHeaders(h)

	s.handler = h
	return s, nil
}

func (s *Server) registerRoutes(mux *http.ServeMux, allowSwagger bool) {
	rl := rateLimit(s.rateLimiter)

	// Health
	mux.HandleFunc("GET /api/v1/health", s.handleHealthGet)

	// Swagger documentation UI (disabled in production)
	if allowSwagger {
		mux.HandleFunc("GET /swagger", func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "/swagger/index.html", http.StatusMovedPermanently)
		})
		mux.HandleFunc("GET /swagger/", httpSwagger.WrapHandler)
	}

	// Auth & Setup
	mux.HandleFunc("GET /api/v1/auth/session", s.handleSessionGet)
	mux.HandleFunc("POST /api/v1/setup", s.handleSetupPost)
	mux.Handle("POST /api/v1/auth/login", rl(http.HandlerFunc(s.handleLoginPost)))
	mux.HandleFunc("POST /api/v1/auth/logout", s.handleLogoutPost)

	// User dashboard & interests
	mux.HandleFunc("GET /api/v1/dashboard", s.handleDashboardGet)
	mux.HandleFunc("GET /api/v1/interests", s.handleInterestsGet)
	mux.HandleFunc("POST /api/v1/interests", s.handleInterestsPost)
	mux.HandleFunc("DELETE /api/v1/interests/{id}", s.handleInterestDelete)

	// Offers history
	mux.HandleFunc("GET /api/v1/offers", s.handleOffersGet)

	// Admin Telegram MTProto
	mux.HandleFunc("GET /api/v1/admin/telegram", s.handleAdminTelegramGet)
	mux.Handle("POST /api/v1/admin/telegram/send-code", rl(http.HandlerFunc(s.handleAdminTelegramSendCodePost)))
	mux.Handle("POST /api/v1/admin/telegram/verify", rl(http.HandlerFunc(s.handleAdminTelegramVerifyPost)))
	mux.HandleFunc("POST /api/v1/admin/telegram/disconnect", s.handleAdminTelegramDisconnectPost)

	// Admin Channels
	mux.HandleFunc("GET /api/v1/admin/channels", s.handleAdminChannelsGet)
	mux.HandleFunc("POST /api/v1/admin/channels/sync", s.handleAdminChannelsSyncPost)
	mux.HandleFunc("PUT /api/v1/admin/channels", s.handleAdminChannelsUpdatePut)

	// Admin Users
	mux.HandleFunc("GET /api/v1/admin/users", s.handleAdminUsersGet)
	mux.HandleFunc("POST /api/v1/admin/users", s.handleAdminUsersCreatePost)
	mux.HandleFunc("PUT /api/v1/admin/users/{id}/password", s.handleAdminUsersPasswordPut)
	mux.HandleFunc("DELETE /api/v1/admin/users/{id}", s.handleAdminUsersDelete)
}

func (s *Server) isSecureRequest(r *http.Request) bool {
	return s.cookieSecure ||
		isProductionEnv(s.appEnv) ||
		r.TLS != nil ||
		strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.handler.ServeHTTP(w, r)
}

func (s *Server) Port() int {
	return s.port
}
