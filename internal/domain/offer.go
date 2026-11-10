package domain

import (
	"time"
)

type UserMatchedProduct struct {
	ID             int64     `json:"id"`
	UserID         int64     `json:"user_id"`
	InterestID     *int64    `json:"interest_id,omitempty"`
	InterestTerm   string    `json:"interest_term"`
	ChannelID      int64     `json:"channel_id"`
	ChannelTitle   string    `json:"channel_title"`
	ChannelHandle  string    `json:"channel_handle"`
	ExtractedPrice *float64  `json:"extracted_price,omitempty"`
	MessageText    string    `json:"message_text"`
	OfferURL       string    `json:"offer_url,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
}

type ExtractedCoupon struct {
	ID            int64     `json:"id"`
	Code          string    `json:"code"`
	StoreName     string    `json:"store_name"`
	StoreURL      string    `json:"store_url"`
	ChannelID     int64     `json:"channel_id"`
	ChannelTitle  string    `json:"channel_title"`
	ChannelHandle string    `json:"channel_handle"`
	MessageText   string    `json:"message_text"`
	CreatedAt     time.Time `json:"created_at"`
}
