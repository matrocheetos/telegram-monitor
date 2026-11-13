package domain

import (
	"time"
)

type TelegramEntityExcerpt struct {
	Type   string `json:"type"`
	Offset int    `json:"offset"`
	Length int    `json:"length"`
	Text   string `json:"text"`
	URL    string `json:"url,omitempty"`
}

type TelegramInlineButton struct {
	Text string `json:"text"`
	URL  string `json:"url"`
}

type IncomingTelegramMessage struct {
	ChannelID         int64                   `json:"channel_id"`
	TelegramMessageID int                     `json:"telegram_message_id"`
	MessageDate       time.Time               `json:"message_date"`
	Text              string                  `json:"text"`
	HasMedia          bool                    `json:"has_media"`
	MediaType         string                  `json:"media_type"`
	WebPageURL        string                  `json:"webpage_url,omitempty"`
	Entities          []TelegramEntityExcerpt `json:"entities,omitempty"`
	InlineButtons     []TelegramInlineButton  `json:"inline_buttons,omitempty"`
}

type EvaluationOutcome string

const (
	OutcomeMatchedProduct          EvaluationOutcome = "matched_product"
	OutcomeExtractedCoupon         EvaluationOutcome = "extracted_coupon"
	OutcomeMatchedProductAndCoupon EvaluationOutcome = "matched_product_and_coupon"
	OutcomeCouponIndicatorNoCode   EvaluationOutcome = "coupon_indicator_no_code"
	OutcomeMonitoredNoMatch        EvaluationOutcome = "monitored_no_match"
	OutcomeUnmonitoredChannel      EvaluationOutcome = "unmonitored_channel"
	OutcomeEmptyText               EvaluationOutcome = "empty_text"
)

type DebugMatchedInterestSummary struct {
	InterestID int64    `json:"interest_id"`
	UserID     int64    `json:"user_id"`
	Term       string   `json:"term"`
	MaxPrice   *float64 `json:"max_price,omitempty"`
}

type DebugExtractedCouponSummary struct {
	Code         string `json:"code"`
	StoreName    string `json:"store_name"`
	StoreURL     string `json:"store_url"`
	Deduplicated bool   `json:"deduplicated"`
}

type AnalyzedMessageDebugRecord struct {
	ID                       int64                         `json:"id"`
	TelegramMessageID        int                           `json:"telegram_message_id"`
	ChannelID                int64                         `json:"channel_id"`
	ChannelTitle             string                        `json:"channel_title"`
	ChannelUsername          string                        `json:"channel_username"`
	MonitorProducts          bool                          `json:"monitor_products"`
	MonitorCoupons           bool                          `json:"monitor_coupons"`
	RawText                  string                        `json:"raw_text"`
	NormalizedText           string                        `json:"normalized_text"`
	Tokens                   []string                      `json:"tokens"`
	HasMedia                 bool                          `json:"has_media"`
	MediaType                string                        `json:"media_type"`
	TGEntities               []TelegramEntityExcerpt       `json:"tg_entities"`
	TGInlineButtons          []TelegramInlineButton        `json:"tg_inline_buttons"`
	TGWebPageURL             string                        `json:"tg_webpage_url"`
	PlainURLs                []string                      `json:"plain_urls"`
	FirstURL                 string                        `json:"first_url"`
	AllPrices                []float64                     `json:"all_prices"`
	PricesFoundCount         int                           `json:"prices_found_count"`
	ExtractedLowestPrice     *float64                      `json:"extracted_lowest_price,omitempty"`
	HasCouponIndicator       bool                          `json:"has_coupon_indicator"`
	IdentifiedStore          string                        `json:"identified_store"`
	IdentifiedStoreURL       string                        `json:"identified_store_url"`
	ExtractedCoupons         []DebugExtractedCouponSummary `json:"extracted_coupons"`
	ExtractedCouponsCount    int                           `json:"extracted_coupons_count"`
	DeduplicatedCouponsCount int                           `json:"deduplicated_coupons_count"`
	MatchedInterests         []DebugMatchedInterestSummary `json:"matched_interests"`
	MatchedProductsCount     int                           `json:"matched_products_count"`
	EvaluationOutcome        EvaluationOutcome             `json:"evaluation_outcome"`
	MessageDate              time.Time                     `json:"message_date"`
	AnalyzedAt               time.Time                     `json:"analyzed_at"`
}
