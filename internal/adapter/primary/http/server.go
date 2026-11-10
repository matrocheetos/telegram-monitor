package http

import (
	"net/http"

	"github.com/matrocheetos/telegram-monitor/internal/port"
)

type Config struct {
	Port               int
	NtfyURL            string
	NtfyTopicSeparator string
	CORSAllowedOrigins string
}

type Server struct {
	port                int
	ntfyURL             string
	ntfyTopicSeparator  string
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
		setupUseCase:        setupUseCase,
		authUseCase:         authUseCase,
		userAdminUseCase:    userAdminUseCase,
		telegramAuthUseCase: telegramAuthUseCase,
		channelUseCase:      channelUseCase,
		interestUseCase:     interestUseCase,
		offerHistoryUseCase: offerHistoryUseCase,
	}

	mux := http.NewServeMux()

	// Health
	mux.HandleFunc("GET /api/v1/health", s.handleHealthGet)

	// Auth & Setup
	mux.HandleFunc("GET /api/v1/auth/session", s.handleSessionGet)
	mux.HandleFunc("POST /api/v1/setup", s.handleSetupPost)
	mux.HandleFunc("POST /api/v1/auth/login", s.handleLoginPost)
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
	mux.HandleFunc("POST /api/v1/admin/telegram/send-code", s.handleAdminTelegramSendCodePost)
	mux.HandleFunc("POST /api/v1/admin/telegram/verify", s.handleAdminTelegramVerifyPost)
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

	// Middlewares chain:
	// Incoming request order: SecurityHeaders -> CORS -> CSRF -> Auth -> FirstBoot -> RBAC -> mux
	var h http.Handler = mux
	h = RBAC(h)
	h = FirstBoot(setupUseCase)(h)
	h = Auth(authUseCase)(h)
	h = CSRF(h)
	h = CORS(cfg.CORSAllowedOrigins)(h)
	h = SecurityHeaders(h)

	s.handler = h
	return s, nil
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.handler.ServeHTTP(w, r)
}

func (s *Server) Port() int {
	return s.port
}
