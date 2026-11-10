package http

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/matrocheetos/telegram-monitor/internal/adapter/primary/http/i18n"
	"github.com/matrocheetos/telegram-monitor/internal/domain"
)

type DashboardData struct {
	ActiveChannelsCount int
	UserTopic           string
	UserNormalizedTopic string
	UserPublicURL       string
	CouponsTopic        string
	CouponsPublicURL    string
	Interests           []*domain.UserInterest
}

func (s *Server) handleRootGet(w http.ResponseWriter, r *http.Request) {
	needsSetup, err := s.setupUseCase.NeedsInitialSetup(r.Context())
	if err == nil && needsSetup {
		http.Redirect(w, r, "/setup", http.StatusSeeOther)
		return
	}

	user := GetUser(r.Context())
	if user == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	if user.Role == domain.RoleAdmin {
		connected, _, err := s.telegramAuthUseCase.GetConnectionStatus(r.Context())
		if err == nil && !connected {
			http.Redirect(w, r, "/admin/telegram", http.StatusSeeOther)
			return
		}
	}

	http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
}

func (s *Server) handleDashboardGet(w http.ResponseWriter, r *http.Request) {
	user := GetUser(r.Context())
	if user == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	interests, err := s.interestUseCase.ListUserInterests(r.Context(), user.ID)
	if err != nil {
		interests = []*domain.UserInterest{}
	}

	channels, err := s.channelUseCase.ListChannels(r.Context())
	activeChannels := 0
	if err == nil {
		for _, ch := range channels {
			if ch.MonitorProducts || ch.MonitorCoupons {
				activeChannels++
			}
		}
	}

	userTopic := "promo/user/" + user.Username
	couponTopic := "promo/cupom"

	data := DashboardData{
		ActiveChannelsCount: activeChannels,
		UserTopic:           userTopic,
		UserNormalizedTopic: normalizeTopic(userTopic, s.ntfyTopicSeparator),
		UserPublicURL:       publicTopicURL(s.ntfyURL, userTopic, s.ntfyTopicSeparator),
		CouponsTopic:        couponTopic,
		CouponsPublicURL:    publicTopicURL(s.ntfyURL, couponTopic, s.ntfyTopicSeparator),
		Interests:           interests,
	}

	s.render(w, r, "dashboard.html", ViewData{
		Title:     s.translator.T(i18n.GetLanguage(r.Context()), "dash_title"),
		ActiveNav: "dashboard",
		Data:      data,
	})
}

func (s *Server) handleInterestsPost(w http.ResponseWriter, r *http.Request) {
	user := GetUser(r.Context())
	if user == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	term := strings.TrimSpace(r.PostFormValue("term"))
	priceStr := strings.TrimSpace(r.PostFormValue("max_price"))

	var maxPrice *float64
	if priceStr != "" {
		cleaned := strings.ReplaceAll(priceStr, ",", ".")
		if val, err := strconv.ParseFloat(cleaned, 64); err == nil && val > 0 {
			maxPrice = &val
		}
	}

	_, err := s.interestUseCase.AddInterest(r.Context(), user.ID, term, maxPrice)
	if err != nil {
		setFlash(w, "error", err.Error())
	} else {
		setFlash(w, "success", s.translator.T(i18n.GetLanguage(r.Context()), "msg_interest_added"))
	}

	http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
}

func (s *Server) handleInterestDeletePost(w http.ResponseWriter, r *http.Request) {
	user := GetUser(r.Context())
	if user == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	idStr := r.PathValue("id")
	interestID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
		return
	}

	err = s.interestUseCase.DeleteInterest(r.Context(), user.ID, interestID)
	if err != nil {
		setFlash(w, "error", err.Error())
	} else {
		setFlash(w, "success", s.translator.T(i18n.GetLanguage(r.Context()), "msg_interest_deleted"))
	}

	http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
}
