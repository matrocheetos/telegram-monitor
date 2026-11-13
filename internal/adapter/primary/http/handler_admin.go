package http

import (
	"log/slog"
	"net/http"
	"strconv"

	"github.com/matrocheetos/telegram-monitor/internal/domain"
)

// handleAdminTelegramGet godoc
// @Summary      Get Telegram connection status
// @Description  Returns MTProto connection status, masked phone, and channels count
// @Tags         Admin Telegram
// @Produce      json
// @Success      200  {object}  TelegramStatusResponse
// @Failure      401  {object}  ErrorEnvelope
// @Failure      403  {object}  ErrorEnvelope
// @Security     CookieAuth
// @Router       /admin/telegram [get]
func (s *Server) handleAdminTelegramGet(w http.ResponseWriter, r *http.Request) {
	connected, phoneMasked, err := s.telegramAuthUseCase.GetConnectionStatus(r.Context())
	if err != nil {
		writeDomainError(w, err)
		return
	}

	channels, err := s.channelUseCase.ListChannels(r.Context())
	if err != nil {
		writeDomainError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, TelegramStatusResponse{
		Connected:     connected,
		PhoneMasked:   phoneMasked,
		ChannelsCount: len(channels),
	})
}

// handleAdminTelegramSendCodePost godoc
// @Summary      Send Telegram MTProto login code
// @Description  Sends OTP verification code to the specified phone number via Telegram MTProto
// @Tags         Admin Telegram
// @Accept       json
// @Produce      json
// @Param        request  body      TelegramSendCodeRequest  true  "Telegram credentials and phone"
// @Success      200      {object}  TelegramSendCodeResponse
// @Failure      400      {object}  ErrorEnvelope
// @Failure      401      {object}  ErrorEnvelope
// @Failure      403      {object}  ErrorEnvelope
// @Security     CookieAuth
// @Security     CSRFToken
// @Router       /admin/telegram/send-code [post]
func (s *Server) handleAdminTelegramSendCodePost(w http.ResponseWriter, r *http.Request) {
	var req TelegramSendCodeRequest
	if err := readJSON(w, r, &req); err != nil {
		writeErrorJSON(w, http.StatusBadRequest, "invalid_request", "invalid json body")
		return
	}

	phoneCodeHash, err := s.telegramAuthUseCase.SendLoginCode(r.Context(), req.APIID, req.APIHash, req.Phone)
	if err != nil {
		writeDomainError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, TelegramSendCodeResponse{
		CodeSent:      true,
		PhoneCodeHash: phoneCodeHash,
	})
}

// handleAdminTelegramVerifyPost godoc
// @Summary      Verify Telegram OTP code
// @Description  Completes MTProto authentication with OTP and optional 2FA password, and synchronizes initial channels
// @Tags         Admin Telegram
// @Accept       json
// @Produce      json
// @Param        request  body      TelegramVerifyRequest  true  "OTP code and optional 2FA password"
// @Success      200      {object}  TelegramSessionPayload
// @Failure      400      {object}  ErrorEnvelope
// @Failure      401      {object}  ErrorEnvelope
// @Failure      403      {object}  ErrorEnvelope
// @Security     CookieAuth
// @Security     CSRFToken
// @Router       /admin/telegram/verify [post]
func (s *Server) handleAdminTelegramVerifyPost(w http.ResponseWriter, r *http.Request) {
	var req TelegramVerifyRequest
	if err := readJSON(w, r, &req); err != nil {
		writeErrorJSON(w, http.StatusBadRequest, "invalid_request", "invalid json body")
		return
	}

	if err := s.telegramAuthUseCase.VerifyLoginCode(r.Context(), req.PhoneCode, req.Password2FA); err != nil {
		writeDomainError(w, err)
		return
	}

	if err := s.channelUseCase.SyncChannels(r.Context()); err != nil {
		slog.WarnContext(r.Context(), "initial channel sync after login failed", "error", err)
	}

	connected, phoneMasked, err := s.telegramAuthUseCase.GetConnectionStatus(r.Context())
	if err != nil {
		writeDomainError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, TelegramSessionPayload{
		Connected:   connected,
		PhoneMasked: phoneMasked,
	})
}

// handleAdminTelegramDisconnectPost godoc
// @Summary      Disconnect Telegram session
// @Description  Disconnects active MTProto session and stops Telegram listener
// @Tags         Admin Telegram
// @Produce      json
// @Success      200  {object}  StatusResponse
// @Failure      401  {object}  ErrorEnvelope
// @Failure      403  {object}  ErrorEnvelope
// @Security     CookieAuth
// @Security     CSRFToken
// @Router       /admin/telegram/disconnect [post]
func (s *Server) handleAdminTelegramDisconnectPost(w http.ResponseWriter, r *http.Request) {
	if err := s.telegramAuthUseCase.Disconnect(r.Context()); err != nil {
		writeDomainError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, StatusResponse{
		Status: "disconnected",
	})
}

// handleAdminChannelsGet godoc
// @Summary      List Telegram channels
// @Description  Returns list of channels and monitoring KPIs
// @Tags         Admin Channels
// @Produce      json
// @Success      200  {object}  AdminChannelsResponse
// @Failure      401  {object}  ErrorEnvelope
// @Failure      403  {object}  ErrorEnvelope
// @Security     CookieAuth
// @Router       /admin/channels [get]
func (s *Server) handleAdminChannelsGet(w http.ResponseWriter, r *http.Request) {
	connected, phoneMasked, err := s.telegramAuthUseCase.GetConnectionStatus(r.Context())
	if err != nil {
		writeDomainError(w, err)
		return
	}

	channels, err := s.channelUseCase.ListChannels(r.Context())
	if err != nil {
		writeDomainError(w, err)
		return
	}

	couponsCount := 0
	productsCount := 0
	items := make([]ChannelItem, 0, len(channels))
	for _, ch := range channels {
		if ch.MonitorCoupons {
			couponsCount++
		}
		if ch.MonitorProducts {
			productsCount++
		}
		items = append(items, ChannelItem{
			ChatID:          ch.ChatID,
			Username:        ch.Username,
			Title:           ch.Title,
			MonitorProducts: ch.MonitorProducts,
			MonitorCoupons:  ch.MonitorCoupons,
			UpdatedAt:       ch.UpdatedAt,
		})
	}

	user := GetUser(r.Context())
	adminTopic := ""
	if user != nil {
		adminTopic = s.ntfyTopicPrefix + user.NtfyTopic
	}

	writeJSON(w, http.StatusOK, AdminChannelsResponse{
		Connected:             connected,
		PhoneMasked:           phoneMasked,
		TotalChannelsCount:    len(channels),
		CouponsChannelsCount:  couponsCount,
		ProductsChannelsCount: productsCount,
		AdminProductTopic:     adminTopic,
		CouponsTopic:          s.ntfyTopicPrefix + "promo/cupom",
		Channels:              items,
	})
}

// handleAdminChannelsSyncPost godoc
// @Summary      Sync Telegram dialogs
// @Description  Fetches user's dialogs from Telegram MTProto and saves new channels
// @Tags         Admin Channels
// @Produce      json
// @Success      200  {object}  AdminChannelsListResponse
// @Failure      401  {object}  ErrorEnvelope
// @Failure      403  {object}  ErrorEnvelope
// @Security     CookieAuth
// @Security     CSRFToken
// @Router       /admin/channels/sync [post]
func (s *Server) handleAdminChannelsSyncPost(w http.ResponseWriter, r *http.Request) {
	if err := s.channelUseCase.SyncChannels(r.Context()); err != nil {
		writeDomainError(w, err)
		return
	}

	channels, err := s.channelUseCase.ListChannels(r.Context())
	if err != nil {
		writeDomainError(w, err)
		return
	}

	items := make([]ChannelItem, 0, len(channels))
	for _, ch := range channels {
		items = append(items, ChannelItem{
			ChatID:          ch.ChatID,
			Username:        ch.Username,
			Title:           ch.Title,
			MonitorProducts: ch.MonitorProducts,
			MonitorCoupons:  ch.MonitorCoupons,
			UpdatedAt:       ch.UpdatedAt,
		})
	}

	writeJSON(w, http.StatusOK, AdminChannelsListResponse{
		Channels: items,
	})
}

// handleAdminChannelsUpdatePut godoc
// @Summary      Update channel monitoring flags
// @Description  Updates monitorProducts and monitorCoupons flags for specified channels
// @Tags         Admin Channels
// @Accept       json
// @Produce      json
// @Param        request  body      UpdateChannelsRequest  true  "Channel monitoring flags"
// @Success      200      {object}  UpdatedResponse
// @Failure      400      {object}  ErrorEnvelope
// @Failure      401      {object}  ErrorEnvelope
// @Failure      403      {object}  ErrorEnvelope
// @Security     CookieAuth
// @Security     CSRFToken
// @Router       /admin/channels [put]
func (s *Server) handleAdminChannelsUpdatePut(w http.ResponseWriter, r *http.Request) {
	var req UpdateChannelsRequest
	if err := readJSON(w, r, &req); err != nil {
		writeErrorJSON(w, http.StatusBadRequest, "invalid_request", "invalid json body")
		return
	}

	updates := make([]domain.ChannelFlagUpdate, 0, len(req.Channels))
	for _, item := range req.Channels {
		updates = append(updates, domain.ChannelFlagUpdate{
			ChatID:          item.ChatID,
			MonitorProducts: item.MonitorProducts,
			MonitorCoupons:  item.MonitorCoupons,
		})
	}
	if err := s.channelUseCase.UpdateChannelsFlagsBatch(r.Context(), updates); err != nil {
		writeDomainError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, UpdatedResponse{
		Updated: true,
	})
}

// handleAdminUsersGet godoc
// @Summary      List registered users
// @Description  Returns all user accounts with roles and active interests counts (excluding passwords)
// @Tags         Admin Users
// @Produce      json
// @Success      200  {array}   AdminUserItem
// @Failure      401  {object}  ErrorEnvelope
// @Failure      403  {object}  ErrorEnvelope
// @Security     CookieAuth
// @Router       /admin/users [get]
func (s *Server) handleAdminUsersGet(w http.ResponseWriter, r *http.Request) {
	users, err := s.userAdminUseCase.ListUsers(r.Context())
	if err != nil {
		writeDomainError(w, err)
		return
	}

	counts, err := s.interestUseCase.CountActiveInterestsByUser(r.Context())
	if err != nil {
		writeDomainError(w, err)
		return
	}

	items := make([]AdminUserItem, 0, len(users))
	for _, u := range users {
		topic := s.ntfyTopicPrefix + u.NtfyTopic
		items = append(items, AdminUserItem{
			ID:                   u.ID,
			Username:             u.Username,
			Role:                 u.Role,
			NtfyTopic:            topic,
			NormalizedNtfyTopic:  normalizeTopic(topic, s.ntfyTopicSeparator),
			ActiveInterestsCount: counts[u.ID],
			CreatedAt:            u.CreatedAt,
		})
	}

	writeJSON(w, http.StatusOK, items)
}

// handleAdminUsersCreatePost godoc
// @Summary      Create user account
// @Description  Creates a new user account with specified username, password, and role
// @Tags         Admin Users
// @Accept       json
// @Produce      json
// @Param        request  body      CreateUserRequest  true  "New user details"
// @Success      201      {object}  AdminUserItem
// @Failure      400      {object}  ErrorEnvelope
// @Failure      401      {object}  ErrorEnvelope
// @Failure      403      {object}  ErrorEnvelope
// @Security     CookieAuth
// @Security     CSRFToken
// @Router       /admin/users [post]
func (s *Server) handleAdminUsersCreatePost(w http.ResponseWriter, r *http.Request) {
	var req CreateUserRequest
	if err := readJSON(w, r, &req); err != nil {
		writeErrorJSON(w, http.StatusBadRequest, "invalid_request", "invalid json body")
		return
	}

	role := domain.Role(req.Role)
	if role == "" {
		role = domain.RoleUser
	}

	user, err := s.userAdminUseCase.CreateUser(r.Context(), req.Username, req.Password, role)
	if err != nil {
		writeDomainError(w, err)
		return
	}

	topic := s.ntfyTopicPrefix + user.NtfyTopic
	writeJSON(w, http.StatusCreated, AdminUserItem{
		ID:                  user.ID,
		Username:            user.Username,
		Role:                user.Role,
		NtfyTopic:           topic,
		NormalizedNtfyTopic: normalizeTopic(topic, s.ntfyTopicSeparator),
		CreatedAt:           user.CreatedAt,
	})
}

// handleAdminUsersPasswordPut godoc
// @Summary      Reset user password
// @Description  Resets password for the specified user ID
// @Tags         Admin Users
// @Accept       json
// @Produce      json
// @Param        id       path  int                   true  "User ID"
// @Param        request  body  ResetPasswordRequest  true  "New password"
// @Success      204      "No Content"
// @Failure      400      {object}  ErrorEnvelope
// @Failure      401      {object}  ErrorEnvelope
// @Failure      403      {object}  ErrorEnvelope
// @Security     CookieAuth
// @Security     CSRFToken
// @Router       /admin/users/{id}/password [put]
func (s *Server) handleAdminUsersPasswordPut(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || id <= 0 {
		writeErrorJSON(w, http.StatusBadRequest, "invalid_id", "invalid user id")
		return
	}

	var req ResetPasswordRequest
	if err := readJSON(w, r, &req); err != nil {
		writeErrorJSON(w, http.StatusBadRequest, "invalid_request", "invalid json body")
		return
	}

	if req.Password == "" {
		writeErrorJSON(w, http.StatusBadRequest, "invalid_request", "password is required")
		return
	}

	if err := s.userAdminUseCase.ResetUserPassword(r.Context(), id, req.Password); err != nil {
		writeDomainError(w, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// handleAdminUsersDelete godoc
// @Summary      Delete user account
// @Description  Deletes user by ID (prevents self-deletion)
// @Tags         Admin Users
// @Param        id   path  int  true  "User ID"
// @Success      204  "No Content"
// @Failure      400  {object}  ErrorEnvelope
// @Failure      401  {object}  ErrorEnvelope
// @Failure      403  {object}  ErrorEnvelope
// @Security     CookieAuth
// @Security     CSRFToken
// @Router       /admin/users/{id} [delete]
func (s *Server) handleAdminUsersDelete(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || id <= 0 {
		writeErrorJSON(w, http.StatusBadRequest, "invalid_id", "invalid user id")
		return
	}

	currUser := GetUser(r.Context())
	if currUser != nil && currUser.ID == id {
		writeErrorJSON(w, http.StatusBadRequest, "cannot_delete_self", "cannot delete currently logged in account")
		return
	}

	if err := s.userAdminUseCase.DeleteUser(r.Context(), id); err != nil {
		writeDomainError(w, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
