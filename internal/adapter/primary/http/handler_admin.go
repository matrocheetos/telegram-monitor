package http

import (
	"net/http"
	"strconv"

	"github.com/matrocheetos/telegram-monitor/internal/domain"
)

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

	_ = s.channelUseCase.SyncChannels(r.Context())

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

func (s *Server) handleAdminTelegramDisconnectPost(w http.ResponseWriter, r *http.Request) {
	if err := s.telegramAuthUseCase.Disconnect(r.Context()); err != nil {
		writeDomainError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"status": "disconnected",
	})
}

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
		adminTopic = user.NtfyTopic
	}

	writeJSON(w, http.StatusOK, AdminChannelsResponse{
		Connected:             connected,
		PhoneMasked:           phoneMasked,
		TotalChannelsCount:    len(channels),
		CouponsChannelsCount:  couponsCount,
		ProductsChannelsCount: productsCount,
		AdminProductTopic:     adminTopic,
		CouponsTopic:          "promo/cupom",
		Channels:              items,
	})
}

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

	writeJSON(w, http.StatusOK, map[string]any{
		"channels": items,
	})
}

func (s *Server) handleAdminChannelsUpdatePut(w http.ResponseWriter, r *http.Request) {
	var req UpdateChannelsRequest
	if err := readJSON(w, r, &req); err != nil {
		writeErrorJSON(w, http.StatusBadRequest, "invalid_request", "invalid json body")
		return
	}

	for _, item := range req.Channels {
		if err := s.channelUseCase.UpdateChannelFlags(r.Context(), item.ChatID, item.MonitorProducts, item.MonitorCoupons); err != nil {
			writeDomainError(w, err)
			return
		}
	}

	writeJSON(w, http.StatusOK, map[string]bool{
		"updated": true,
	})
}

func (s *Server) handleAdminUsersGet(w http.ResponseWriter, r *http.Request) {
	users, err := s.userAdminUseCase.ListUsers(r.Context())
	if err != nil {
		writeDomainError(w, err)
		return
	}

	items := make([]AdminUserItem, 0, len(users))
	for _, u := range users {
		interests, err := s.interestUseCase.ListUserInterests(r.Context(), u.ID)
		activeCount := 0
		if err == nil {
			for _, in := range interests {
				if in.Active {
					activeCount++
				}
			}
		}

		items = append(items, AdminUserItem{
			ID:                   u.ID,
			Username:             u.Username,
			Role:                 u.Role,
			NtfyTopic:            u.NtfyTopic,
			NormalizedNtfyTopic:  normalizeTopic(u.NtfyTopic, s.ntfyTopicSeparator),
			ActiveInterestsCount: activeCount,
			CreatedAt:            u.CreatedAt,
		})
	}

	writeJSON(w, http.StatusOK, items)
}

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

	writeJSON(w, http.StatusCreated, AdminUserItem{
		ID:                  user.ID,
		Username:            user.Username,
		Role:                user.Role,
		NtfyTopic:           user.NtfyTopic,
		NormalizedNtfyTopic: normalizeTopic(user.NtfyTopic, s.ntfyTopicSeparator),
		CreatedAt:           user.CreatedAt,
	})
}

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
