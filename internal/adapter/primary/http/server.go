package http

import (
	"embed"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"

	"github.com/matrocheetos/telegram-monitor/internal/adapter/primary/http/i18n"
	"github.com/matrocheetos/telegram-monitor/internal/port"
)

//go:embed templates/*.html static/*
var contentFS embed.FS

type Config struct {
	Port               int
	NtfyURL            string
	NtfyTopicSeparator string
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
	translator          *i18n.Translator
	templates           map[string]*template.Template
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
	translator *i18n.Translator,
) (*Server, error) {
	templates, err := parseTemplates(contentFS)
	if err != nil {
		return nil, fmt.Errorf("parse templates: %w", err)
	}

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
		translator:          translator,
		templates:           templates,
	}

	mux := http.NewServeMux()

	staticFS, err := fs.Sub(contentFS, "static")
	if err != nil {
		return nil, fmt.Errorf("sub static fs: %w", err)
	}
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticFS))))

	// Language switcher
	mux.HandleFunc("POST /lang", s.handleLangPost)

	// Auth routes
	mux.HandleFunc("GET /setup", s.handleSetupGet)
	mux.HandleFunc("POST /setup", s.handleSetupPost)
	mux.HandleFunc("GET /login", s.handleLoginGet)
	mux.HandleFunc("POST /login", s.handleLoginPost)
	mux.HandleFunc("POST /logout", s.handleLogoutPost)

	// User dashboard routes
	mux.HandleFunc("GET /{$}", s.handleRootGet)
	mux.HandleFunc("GET /dashboard", s.handleDashboardGet)
	mux.HandleFunc("POST /interests", s.handleInterestsPost)
	mux.HandleFunc("POST /interests/{id}/delete", s.handleInterestDeletePost)

	// Admin Telegram connection routes
	mux.HandleFunc("GET /admin/telegram", s.handleAdminTelegramGet)
	mux.HandleFunc("POST /admin/telegram/send-code", s.handleAdminTelegramSendCodePost)
	mux.HandleFunc("POST /admin/telegram/verify", s.handleAdminTelegramVerifyPost)
	mux.HandleFunc("POST /admin/telegram/disconnect", s.handleAdminTelegramDisconnectPost)

	// Admin Channels routes
	mux.HandleFunc("GET /admin/channels", s.handleAdminChannelsGet)
	mux.HandleFunc("POST /admin/channels/sync", s.handleAdminChannelsSyncPost)
	mux.HandleFunc("POST /admin/channels/update", s.handleAdminChannelsUpdatePost)

	// Admin Users routes
	mux.HandleFunc("GET /admin/users", s.handleAdminUsersGet)
	mux.HandleFunc("POST /admin/users", s.handleAdminUsersCreatePost)
	mux.HandleFunc("POST /admin/users/{id}/password", s.handleAdminUsersPasswordPost)
	mux.HandleFunc("POST /admin/users/{id}/delete", s.handleAdminUsersDeletePost)

	// Chain middlewares in execution order:
	// 1. SecurityHeaders
	// 2. I18n
	// 3. CSRF
	// 4. Auth
	// 5. FirstBoot
	// 6. RBAC
	// 7. TelegramSetupRedirect
	var h http.Handler = mux
	h = TelegramSetupRedirect(telegramAuthUseCase)(h)
	h = RBAC(h)
	h = FirstBoot(setupUseCase)(h)
	h = Auth(authUseCase)(h)
	h = CSRF(h)
	h = I18n(translator)(h)
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

func (s *Server) render(w http.ResponseWriter, r *http.Request, templateName string, data ViewData) {
	tmpl, ok := s.templates[templateName]
	if !ok {
		http.Error(w, fmt.Sprintf("Template %s not found", templateName), http.StatusInternalServerError)
		return
	}

	data.CurrentLang = i18n.GetLanguage(r.Context())
	data.CSRFToken = GetCSRFToken(r.Context())
	data.User = GetUser(r.Context())
	data.Flash = getAndClearFlash(w, r)
	data.translator = s.translator

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.Execute(w, &data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func parseTemplates(embeddedFS embed.FS) (map[string]*template.Template, error) {
	tmplMap := make(map[string]*template.Template)
	pages := []string{
		"setup.html",
		"login.html",
		"dashboard.html",
		"admin_telegram.html",
		"admin_channels.html",
		"admin_users.html",
	}

	for _, page := range pages {
		t, err := template.New("layout.html").ParseFS(embeddedFS, "templates/layout.html", "templates/"+page)
		if err != nil {
			return nil, fmt.Errorf("parse template %s: %w", page, err)
		}
		tmplMap[page] = t
	}

	return tmplMap, nil
}
