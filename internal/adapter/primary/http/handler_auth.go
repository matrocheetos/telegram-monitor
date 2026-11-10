package http

import (
	"net/http"

	"github.com/matrocheetos/telegram-monitor/internal/domain"
)

// handleHealthGet godoc
// @Summary      Healthcheck
// @Description  Returns service health status
// @Tags         Health
// @Produce      json
// @Success      200  {object}  HealthResponse
// @Router       /health [get]
func (s *Server) handleHealthGet(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, HealthResponse{
		Status: "ok",
	})
}

// handleSessionGet godoc
// @Summary      Get current session
// @Description  Returns initial setup state, authentication state, CSRF token, and user/telegram details
// @Tags         Auth
// @Produce      json
// @Success      200  {object}  SessionResponse
// @Failure      500  {object}  ErrorEnvelope
// @Router       /auth/session [get]
func (s *Server) handleSessionGet(w http.ResponseWriter, r *http.Request) {
	needsSetup, err := s.setupUseCase.NeedsInitialSetup(r.Context())
	if err != nil {
		writeDomainError(w, err)
		return
	}

	user := GetUser(r.Context())
	csrfToken := GetCSRFToken(r.Context())

	resp := SessionResponse{
		NeedsInitialSetup: needsSetup,
		Authenticated:     user != nil,
		CSRFToken:         csrfToken,
	}

	if user != nil {
		resp.User = &UserPayload{
			ID:        user.ID,
			Username:  user.Username,
			Role:      user.Role,
			NtfyTopic: user.NtfyTopic,
		}

		if user.Role == domain.RoleAdmin {
			connected, phoneMasked, err := s.telegramAuthUseCase.GetConnectionStatus(r.Context())
			if err == nil {
				resp.Telegram = &TelegramSessionPayload{
					Connected:   connected,
					PhoneMasked: phoneMasked,
				}
			} else {
				resp.Telegram = &TelegramSessionPayload{
					Connected:   false,
					PhoneMasked: "",
				}
			}
		}
	}

	writeJSON(w, http.StatusOK, resp)
}

// handleSetupPost godoc
// @Summary      Initial Admin Setup
// @Description  Creates initial admin account on first boot, issues session cookie, and returns session details
// @Tags         Auth
// @Accept       json
// @Produce      json
// @Param        request  body      SetupRequest  true  "Admin initial credentials"
// @Success      201      {object}  SessionResponse
// @Failure      400      {object}  ErrorEnvelope
// @Failure      403      {object}  ErrorEnvelope
// @Router       /setup [post]
func (s *Server) handleSetupPost(w http.ResponseWriter, r *http.Request) {
	var req SetupRequest
	if err := readJSON(w, r, &req); err != nil {
		writeErrorJSON(w, http.StatusBadRequest, "invalid_request", "invalid json body")
		return
	}

	if req.Username == "" || req.Password == "" {
		writeErrorJSON(w, http.StatusBadRequest, "invalid_request", "username and password are required")
		return
	}

	if req.Password != req.PasswordConfirm {
		writeErrorJSON(w, http.StatusBadRequest, "password_mismatch", "passwords do not match")
		return
	}

	if _, err := s.setupUseCase.CreateInitialAdmin(r.Context(), req.Username, req.Password); err != nil {
		writeDomainError(w, err)
		return
	}

	user, token, err := s.authUseCase.Login(r.Context(), req.Username, req.Password)
	if err != nil {
		writeDomainError(w, err)
		return
	}

	//nolint:gosec // session cookie for api
	http.SetCookie(w, &http.Cookie{
		Name:     "session_id",
		Value:    token,
		Path:     "/",
		MaxAge:   30 * 24 * 3600,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})

	writeJSON(w, http.StatusCreated, SessionResponse{
		NeedsInitialSetup: false,
		Authenticated:     true,
		CSRFToken:         GetCSRFToken(r.Context()),
		User: &UserPayload{
			ID:        user.ID,
			Username:  user.Username,
			Role:      user.Role,
			NtfyTopic: user.NtfyTopic,
		},
		Telegram: &TelegramSessionPayload{
			Connected:   false,
			PhoneMasked: "",
		},
	})
}

// handleLoginPost godoc
// @Summary      User Login
// @Description  Authenticates user credentials, sets session_id cookie, and returns user details
// @Tags         Auth
// @Accept       json
// @Produce      json
// @Param        request  body      LoginRequest  true  "User login credentials"
// @Success      200      {object}  LoginResponse
// @Failure      400      {object}  ErrorEnvelope
// @Failure      401      {object}  ErrorEnvelope
// @Router       /auth/login [post]
func (s *Server) handleLoginPost(w http.ResponseWriter, r *http.Request) {
	var req LoginRequest
	if err := readJSON(w, r, &req); err != nil {
		writeErrorJSON(w, http.StatusBadRequest, "invalid_request", "invalid json body")
		return
	}

	user, token, err := s.authUseCase.Login(r.Context(), req.Username, req.Password)
	if err != nil {
		writeDomainError(w, err)
		return
	}

	//nolint:gosec // session cookie for api
	http.SetCookie(w, &http.Cookie{
		Name:     "session_id",
		Value:    token,
		Path:     "/",
		MaxAge:   30 * 24 * 3600,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})

	var tgPayload *TelegramSessionPayload
	if user.Role == domain.RoleAdmin {
		connected, phoneMasked, err := s.telegramAuthUseCase.GetConnectionStatus(r.Context())
		if err == nil {
			tgPayload = &TelegramSessionPayload{
				Connected:   connected,
				PhoneMasked: phoneMasked,
			}
		} else {
			tgPayload = &TelegramSessionPayload{
				Connected:   false,
				PhoneMasked: "",
			}
		}
	}

	writeJSON(w, http.StatusOK, LoginResponse{
		Authenticated: true,
		User: UserPayload{
			ID:        user.ID,
			Username:  user.Username,
			Role:      user.Role,
			NtfyTopic: user.NtfyTopic,
		},
		Telegram: tgPayload,
	})
}

// handleLogoutPost godoc
// @Summary      User Logout
// @Description  Revokes session and clears session_id cookie
// @Tags         Auth
// @Produce      json
// @Success      204      "No Content"
// @Security     CookieAuth
// @Security     CSRFToken
// @Router       /auth/logout [post]
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

	w.WriteHeader(http.StatusNoContent)
}
