package http

import (
	"net/http"
	"strconv"

	"github.com/matrocheetos/telegram-monitor/internal/domain"
)

func toInterestItems(interests []*domain.UserInterest) []InterestItem {
	items := make([]InterestItem, 0, len(interests))
	for _, item := range interests {
		items = append(items, InterestItem{
			ID:             item.ID,
			RawTerm:        item.RawTerm,
			PositiveTokens: item.PositiveTokens,
			NegativeTokens: item.NegativeTokens,
			MaxPrice:       item.MaxPrice,
			Active:         item.Active,
			CreatedAt:      item.CreatedAt,
		})
	}
	return items
}

// handleDashboardGet godoc
// @Summary      Get dashboard summary
// @Description  Returns user ntfy topics, active channels count, and list of monitored search terms
// @Tags         Dashboard
// @Produce      json
// @Success      200  {object}  DashboardResponse
// @Failure      401  {object}  ErrorEnvelope
// @Failure      500  {object}  ErrorEnvelope
// @Security     CookieAuth
// @Router       /dashboard [get]
func (s *Server) handleDashboardGet(w http.ResponseWriter, r *http.Request) {
	user := GetUser(r.Context())
	if user == nil {
		writeErrorJSON(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}

	channels, err := s.channelUseCase.ListChannels(r.Context())
	if err != nil {
		writeDomainError(w, err)
		return
	}

	activeCount := 0
	for _, ch := range channels {
		if ch.MonitorProducts || ch.MonitorCoupons {
			activeCount++
		}
	}

	interests, err := s.interestUseCase.ListUserInterests(r.Context(), user.ID)
	if err != nil {
		writeDomainError(w, err)
		return
	}

	items := toInterestItems(interests)

	userTopic := s.ntfyTopicPrefix + user.NtfyTopic
	couponsTopic := s.ntfyTopicPrefix + "promo/cupom"

	resp := DashboardResponse{
		ActiveChannelsCount: activeCount,
		UserTopic:           userTopic,
		UserNormalizedTopic: normalizeTopic(userTopic, s.ntfyTopicSeparator),
		UserPublicURL:       buildPublicURL(s.ntfyURL, userTopic, s.ntfyTopicSeparator),
		CouponsTopic:        couponsTopic,
		CouponsPublicURL:    buildPublicURL(s.ntfyURL, couponsTopic, s.ntfyTopicSeparator),
		Interests:           items,
	}

	writeJSON(w, http.StatusOK, resp)
}

// handleInterestsGet godoc
// @Summary      List monitored interests
// @Description  Returns all product search terms configured by the authenticated user
// @Tags         Interests
// @Produce      json
// @Success      200  {array}   InterestItem
// @Failure      401  {object}  ErrorEnvelope
// @Failure      500  {object}  ErrorEnvelope
// @Security     CookieAuth
// @Router       /interests [get]
func (s *Server) handleInterestsGet(w http.ResponseWriter, r *http.Request) {
	user := GetUser(r.Context())
	if user == nil {
		writeErrorJSON(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}

	interests, err := s.interestUseCase.ListUserInterests(r.Context(), user.ID)
	if err != nil {
		writeDomainError(w, err)
		return
	}

	items := toInterestItems(interests)

	writeJSON(w, http.StatusOK, items)
}

// handleInterestsPost godoc
// @Summary      Add monitored interest
// @Description  Creates a new monitored product search term with negative keyword filtering and optional max price
// @Tags         Interests
// @Accept       json
// @Produce      json
// @Param        request  body      CreateInterestRequest  true  "Interest term and optional max price"
// @Success      201      {object}  InterestItem
// @Failure      400      {object}  ErrorEnvelope
// @Failure      401      {object}  ErrorEnvelope
// @Security     CookieAuth
// @Security     CSRFToken
// @Router       /interests [post]
func (s *Server) handleInterestsPost(w http.ResponseWriter, r *http.Request) {
	user := GetUser(r.Context())
	if user == nil {
		writeErrorJSON(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}

	var req CreateInterestRequest
	if err := readJSON(w, r, &req); err != nil {
		writeErrorJSON(w, http.StatusBadRequest, "invalid_request", "invalid json body")
		return
	}

	interest, err := s.interestUseCase.AddInterest(r.Context(), user.ID, req.Term, req.MaxPrice)
	if err != nil {
		writeDomainError(w, err)
		return
	}

	writeJSON(w, http.StatusCreated, InterestItem{
		ID:             interest.ID,
		RawTerm:        interest.RawTerm,
		PositiveTokens: interest.PositiveTokens,
		NegativeTokens: interest.NegativeTokens,
		MaxPrice:       interest.MaxPrice,
		Active:         interest.Active,
		CreatedAt:      interest.CreatedAt,
	})
}

// handleInterestDelete godoc
// @Summary      Delete monitored interest
// @Description  Deletes a monitored search term by ID
// @Tags         Interests
// @Param        id   path  int  true  "Interest ID"
// @Success      204  "No Content"
// @Failure      400  {object}  ErrorEnvelope
// @Failure      401  {object}  ErrorEnvelope
// @Security     CookieAuth
// @Security     CSRFToken
// @Router       /interests/{id} [delete]
func (s *Server) handleInterestDelete(w http.ResponseWriter, r *http.Request) {
	user := GetUser(r.Context())
	if user == nil {
		writeErrorJSON(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}

	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || id <= 0 {
		writeErrorJSON(w, http.StatusBadRequest, "invalid_id", "invalid interest id")
		return
	}

	if err := s.interestUseCase.DeleteInterest(r.Context(), user.ID, id); err != nil {
		writeDomainError(w, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
