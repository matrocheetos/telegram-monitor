package http

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

func formatChannelDisplay(title, handle string) string {
	if handle != "" {
		cleanHandle := strings.TrimPrefix(handle, "@")
		if title != "" {
			return fmt.Sprintf("%s (@%s)", title, cleanHandle)
		}
		return "@" + cleanHandle
	}
	if title != "" {
		return title
	}
	return "Canal"
}

func formatPrice(price *float64) string {
	if price == nil {
		return ""
	}
	return fmt.Sprintf("R$ %.2f", *price)
}

func (s *Server) handleOffersGet(w http.ResponseWriter, r *http.Request) {
	user := GetUser(r.Context())
	if user == nil {
		writeErrorJSON(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}

	q := strings.TrimSpace(r.URL.Query().Get("q"))
	limit := 50
	if lStr := r.URL.Query().Get("limit"); lStr != "" {
		if lVal, err := strconv.Atoi(lStr); err == nil && lVal > 0 {
			if lVal > 100 {
				limit = 100
			} else {
				limit = lVal
			}
		}
	}

	offset := 0
	if oStr := r.URL.Query().Get("offset"); oStr != "" {
		if oVal, err := strconv.Atoi(oStr); err == nil && oVal >= 0 {
			offset = oVal
		}
	}

	products, pTotal, err := s.offerHistoryUseCase.ListUserMatchedProducts(r.Context(), user.ID, q, limit, offset)
	if err != nil {
		writeDomainError(w, err)
		return
	}

	coupons, cTotal, err := s.offerHistoryUseCase.ListExtractedCoupons(r.Context(), q, limit, offset)
	if err != nil {
		writeDomainError(w, err)
		return
	}

	productItems := make([]OfferProductItem, 0, len(products))
	for _, p := range products {
		productItems = append(productItems, OfferProductItem{
			ID:             p.ID,
			InterestTerm:   p.InterestTerm,
			ChannelDisplay: formatChannelDisplay(p.ChannelTitle, p.ChannelHandle),
			ExtractedPrice: p.ExtractedPrice,
			FormattedPrice: formatPrice(p.ExtractedPrice),
			MessageText:    p.MessageText,
			OfferURL:       p.OfferURL,
			CreatedAt:      p.CreatedAt,
		})
	}

	couponItems := make([]OfferCouponItem, 0, len(coupons))
	for _, c := range coupons {
		couponItems = append(couponItems, OfferCouponItem{
			ID:             c.ID,
			Code:           c.Code,
			StoreName:      c.StoreName,
			StoreURL:       c.StoreURL,
			MessageText:    c.MessageText,
			ChannelDisplay: formatChannelDisplay(c.ChannelTitle, c.ChannelHandle),
			CreatedAt:      c.CreatedAt,
		})
	}

	writeJSON(w, http.StatusOK, OffersResponse{
		ProductsCount: pTotal,
		CouponsCount:  cTotal,
		Products:      productItems,
		Coupons:       couponItems,
	})
}
