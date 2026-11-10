package http

import (
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/matrocheetos/telegram-monitor/internal/adapter/primary/http/i18n"
	"github.com/matrocheetos/telegram-monitor/internal/domain"
)

func (s *Server) handleSetupGet(w http.ResponseWriter, r *http.Request) {
	s.render(w, r, "setup.html", ViewData{
		Title: s.translator.T(i18n.GetLanguage(r.Context()), "setup_title"),
	})
}

func (s *Server) handleSetupPost(w http.ResponseWriter, r *http.Request) {
	lang := i18n.GetLanguage(r.Context())
	username := strings.TrimSpace(r.PostFormValue("username"))
	password := r.PostFormValue("password")
	passwordConfirm := r.PostFormValue("password_confirm")

	if password != passwordConfirm {
		setFlash(w, "error", s.translator.T(lang, "msg_password_mismatch"))
		http.Redirect(w, r, "/setup", http.StatusSeeOther)
		return
	}

	_, err := s.setupUseCase.CreateInitialAdmin(r.Context(), username, password)
	if err != nil {
		setFlash(w, "error", err.Error())
		http.Redirect(w, r, "/setup", http.StatusSeeOther)
		return
	}

	_, token, err := s.authUseCase.Login(r.Context(), username, password)
	if err == nil {
		//nolint:gosec // session cookie security attributes managed for reverse-proxy deployment
		http.SetCookie(w, &http.Cookie{
			Name:     "session_id",
			Value:    token,
			Path:     "/",
			MaxAge:   7 * 24 * 3600,
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
		})
	}

	setFlash(w, "success", s.translator.T(lang, "msg_setup_success"))
	http.Redirect(w, r, "/admin/telegram", http.StatusSeeOther)
}

func (s *Server) handleLoginGet(w http.ResponseWriter, r *http.Request) {
	if user := GetUser(r.Context()); user != nil {
		if user.Role == domain.RoleAdmin {
			connected, _, err := s.telegramAuthUseCase.GetConnectionStatus(r.Context())
			if err == nil && !connected {
				http.Redirect(w, r, "/admin/telegram", http.StatusSeeOther)
				return
			}
		}
		http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
		return
	}

	s.render(w, r, "login.html", ViewData{
		Title: s.translator.T(i18n.GetLanguage(r.Context()), "login_title"),
	})
}

func (s *Server) handleLoginPost(w http.ResponseWriter, r *http.Request) {
	lang := i18n.GetLanguage(r.Context())
	username := strings.TrimSpace(r.PostFormValue("username"))
	password := r.PostFormValue("password")

	user, token, err := s.authUseCase.Login(r.Context(), username, password)
	if err != nil {
		msg := s.translator.T(lang, "msg_login_failed")
		if !errors.Is(err, domain.ErrInvalidCredentials) {
			msg = err.Error()
		}
		setFlash(w, "error", msg)
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	//nolint:gosec // session cookie security attributes managed for reverse-proxy deployment
	http.SetCookie(w, &http.Cookie{
		Name:     "session_id",
		Value:    token,
		Path:     "/",
		MaxAge:   7 * 24 * 3600,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})

	if user.Role == domain.RoleAdmin {
		connected, _, err := s.telegramAuthUseCase.GetConnectionStatus(r.Context())
		if err == nil && !connected {
			http.Redirect(w, r, "/admin/telegram", http.StatusSeeOther)
			return
		}
	}

	http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
}

func (s *Server) handleLogoutPost(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie("session_id")
	if err == nil && cookie.Value != "" {
		_ = s.authUseCase.Logout(r.Context(), cookie.Value)
	}

	//nolint:gosec // clearing session cookie
	http.SetCookie(w, &http.Cookie{
		Name:     "session_id",
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})

	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func (s *Server) handleLangPost(w http.ResponseWriter, r *http.Request) {
	lang := r.PostFormValue("lang")
	normalized := s.translator.NormalizeLang(lang)

	//nolint:gosec // language preference cookie managed for reverse-proxy deployment
	http.SetCookie(w, &http.Cookie{
		Name:     "lang",
		Value:    normalized,
		Path:     "/",
		MaxAge:   365 * 24 * 3600,
		SameSite: http.SameSiteLaxMode,
	})

	cleanRedirect := "/"
	if ref := r.Header.Get("Referer"); ref != "" {
		if parsed, err := url.Parse(ref); err == nil && parsed.Path != "" && strings.HasPrefix(parsed.Path, "/") {
			cleanRedirect = parsed.Path
			if parsed.RawQuery != "" {
				cleanRedirect += "?" + parsed.RawQuery
			}
		}
	}
	//nolint:gosec // sanitized relative redirect target
	http.Redirect(w, r, cleanRedirect, http.StatusSeeOther)
}
