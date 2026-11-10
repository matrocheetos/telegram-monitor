package http

import (
	"net/http"
	"strconv"
)

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

	resp := DashboardResponse{
		ActiveChannelsCount: activeCount,
		UserTopic:           user.NtfyTopic,
		UserNormalizedTopic: normalizeTopic(user.NtfyTopic, s.ntfyTopicSeparator),
		UserPublicURL:       buildPublicURL(s.ntfyURL, user.NtfyTopic, s.ntfyTopicSeparator),
		CouponsTopic:        "promo/cupom",
		CouponsPublicURL:    buildPublicURL(s.ntfyURL, "promo/cupom", s.ntfyTopicSeparator),
		Interests:           items,
	}

	writeJSON(w, http.StatusOK, resp)
}

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

	writeJSON(w, http.StatusOK, items)
}

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
