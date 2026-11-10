package http

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/matrocheetos/telegram-monitor/internal/adapter/primary/http/i18n"
	"github.com/matrocheetos/telegram-monitor/internal/domain"
)

type TelegramViewData struct {
	Connected   bool
	PhoneMasked string
	APIID       string
	APIHash     string
	Phone       string
}

type ChannelsViewData struct {
	Connected             bool
	PhoneMasked           string
	Channels              []*domain.TelegramChannel
	CouponsChannelsCount  int
	ProductsChannelsCount int
}

type UsersViewData struct {
	Users []*domain.User
}

func (s *Server) handleAdminTelegramGet(w http.ResponseWriter, r *http.Request) {
	connected, phoneMasked, _ := s.telegramAuthUseCase.GetConnectionStatus(r.Context())

	s.render(w, r, "admin_telegram.html", ViewData{
		Title:     s.translator.T(i18n.GetLanguage(r.Context()), "admin_tg_title"),
		ActiveNav: "telegram",
		Data: TelegramViewData{
			Connected:   connected,
			PhoneMasked: phoneMasked,
		},
	})
}

func (s *Server) handleAdminTelegramSendCodePost(w http.ResponseWriter, r *http.Request) {
	lang := i18n.GetLanguage(r.Context())
	apiIDStr := strings.TrimSpace(r.PostFormValue("api_id"))
	apiHash := strings.TrimSpace(r.PostFormValue("api_hash"))
	phone := strings.TrimSpace(r.PostFormValue("phone"))

	apiID, err := strconv.Atoi(apiIDStr)
	if err != nil || apiID <= 0 {
		setFlash(w, "error", "Invalid API_ID")
		http.Redirect(w, r, "/admin/telegram", http.StatusSeeOther)
		return
	}

	_, err = s.telegramAuthUseCase.SendLoginCode(r.Context(), apiID, apiHash, phone)
	if err != nil {
		setFlash(w, "error", err.Error())
	} else {
		setFlash(w, "success", s.translator.T(lang, "msg_code_sent"))
	}

	http.Redirect(w, r, "/admin/telegram", http.StatusSeeOther)
}

func (s *Server) handleAdminTelegramVerifyPost(w http.ResponseWriter, r *http.Request) {
	lang := i18n.GetLanguage(r.Context())
	phoneCode := strings.TrimSpace(r.PostFormValue("phone_code"))
	password2FA := r.PostFormValue("password_2fa")

	err := s.telegramAuthUseCase.VerifyLoginCode(r.Context(), phoneCode, password2FA)
	if err != nil {
		setFlash(w, "error", err.Error())
		http.Redirect(w, r, "/admin/telegram", http.StatusSeeOther)
		return
	}

	_ = s.channelUseCase.SyncChannels(r.Context())

	setFlash(w, "success", s.translator.T(lang, "msg_telegram_connected"))
	http.Redirect(w, r, "/admin/channels", http.StatusSeeOther)
}

func (s *Server) handleAdminTelegramDisconnectPost(w http.ResponseWriter, r *http.Request) {
	lang := i18n.GetLanguage(r.Context())
	err := s.telegramAuthUseCase.Disconnect(r.Context())
	if err != nil {
		setFlash(w, "error", err.Error())
	} else {
		setFlash(w, "success", s.translator.T(lang, "msg_telegram_disconnected"))
	}

	http.Redirect(w, r, "/admin/telegram", http.StatusSeeOther)
}

func (s *Server) handleAdminChannelsGet(w http.ResponseWriter, r *http.Request) {
	connected, phoneMasked, _ := s.telegramAuthUseCase.GetConnectionStatus(r.Context())
	channels, err := s.channelUseCase.ListChannels(r.Context())
	if err != nil {
		channels = []*domain.TelegramChannel{}
	}

	couponsCount := 0
	productsCount := 0
	for _, ch := range channels {
		if ch.MonitorCoupons {
			couponsCount++
		}
		if ch.MonitorProducts {
			productsCount++
		}
	}

	s.render(w, r, "admin_channels.html", ViewData{
		Title:     s.translator.T(i18n.GetLanguage(r.Context()), "admin_ch_title"),
		ActiveNav: "channels",
		Data: ChannelsViewData{
			Connected:             connected,
			PhoneMasked:           phoneMasked,
			Channels:              channels,
			CouponsChannelsCount:  couponsCount,
			ProductsChannelsCount: productsCount,
		},
	})
}

func (s *Server) handleAdminChannelsSyncPost(w http.ResponseWriter, r *http.Request) {
	lang := i18n.GetLanguage(r.Context())
	err := s.channelUseCase.SyncChannels(r.Context())
	if err != nil {
		setFlash(w, "error", err.Error())
	} else {
		setFlash(w, "success", s.translator.T(lang, "msg_channels_synced"))
	}

	http.Redirect(w, r, "/admin/channels", http.StatusSeeOther)
}

func (s *Server) handleAdminChannelsUpdatePost(w http.ResponseWriter, r *http.Request) {
	lang := i18n.GetLanguage(r.Context())
	_ = r.ParseForm()

	channelIDs := r.PostForm["channel_ids"]
	for _, idStr := range channelIDs {
		chatID, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			continue
		}
		monitorProducts := r.PostFormValue("monitor_products_"+idStr) == "true"
		monitorCoupons := r.PostFormValue("monitor_coupons_"+idStr) == "true"

		_ = s.channelUseCase.UpdateChannelFlags(r.Context(), chatID, monitorProducts, monitorCoupons)
	}

	setFlash(w, "success", s.translator.T(lang, "msg_channels_updated"))
	http.Redirect(w, r, "/admin/channels", http.StatusSeeOther)
}

func (s *Server) handleAdminUsersGet(w http.ResponseWriter, r *http.Request) {
	users, err := s.userAdminUseCase.ListUsers(r.Context())
	if err != nil {
		users = []*domain.User{}
	}

	s.render(w, r, "admin_users.html", ViewData{
		Title:     s.translator.T(i18n.GetLanguage(r.Context()), "admin_u_title"),
		ActiveNav: "users",
		Data: UsersViewData{
			Users: users,
		},
	})
}

func (s *Server) handleAdminUsersCreatePost(w http.ResponseWriter, r *http.Request) {
	lang := i18n.GetLanguage(r.Context())
	username := strings.TrimSpace(r.PostFormValue("username"))
	password := r.PostFormValue("password")
	roleStr := r.PostFormValue("role")

	role := domain.RoleUser
	if roleStr == string(domain.RoleAdmin) {
		role = domain.RoleAdmin
	}

	user, err := s.userAdminUseCase.CreateUser(r.Context(), username, password, role)
	if err != nil {
		setFlash(w, "error", err.Error())
	} else {
		setFlash(w, "success", s.translator.T(lang, "msg_user_created", user.Username))
	}

	http.Redirect(w, r, "/admin/users", http.StatusSeeOther)
}

func (s *Server) handleAdminUsersPasswordPost(w http.ResponseWriter, r *http.Request) {
	lang := i18n.GetLanguage(r.Context())
	idStr := r.PathValue("id")
	userID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Redirect(w, r, "/admin/users", http.StatusSeeOther)
		return
	}

	password := r.PostFormValue("password")
	err = s.userAdminUseCase.ResetUserPassword(r.Context(), userID, password)
	if err != nil {
		setFlash(w, "error", err.Error())
	} else {
		setFlash(w, "success", s.translator.T(lang, "msg_password_reset"))
	}

	http.Redirect(w, r, "/admin/users", http.StatusSeeOther)
}

func (s *Server) handleAdminUsersDeletePost(w http.ResponseWriter, r *http.Request) {
	lang := i18n.GetLanguage(r.Context())
	idStr := r.PathValue("id")
	userID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Redirect(w, r, "/admin/users", http.StatusSeeOther)
		return
	}

	currentUser := GetUser(r.Context())
	if currentUser != nil && currentUser.ID == userID {
		setFlash(w, "error", "Cannot delete your own administrator account")
		http.Redirect(w, r, "/admin/users", http.StatusSeeOther)
		return
	}

	err = s.userAdminUseCase.DeleteUser(r.Context(), userID)
	if err != nil {
		setFlash(w, "error", err.Error())
	} else {
		setFlash(w, "success", s.translator.T(lang, "msg_user_deleted"))
	}

	http.Redirect(w, r, "/admin/users", http.StatusSeeOther)
}
