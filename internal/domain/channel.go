package domain

import (
	"strings"
	"time"
)

type TelegramChannel struct {
	ChatID          int64     `json:"chat_id" db:"chat_id"`
	AccessHash      int64     `json:"access_hash" db:"access_hash"`
	Username        string    `json:"username" db:"username"`
	Title           string    `json:"title" db:"title"`
	MonitorProducts bool      `json:"monitor_products" db:"monitor_products"`
	MonitorCoupons  bool      `json:"monitor_coupons" db:"monitor_coupons"`
	UpdatedAt       time.Time `json:"updated_at" db:"updated_at"`
}

func NewTelegramChannel(
	chatID int64,
	accessHash int64,
	username string,
	title string,
	monitorProducts bool,
	monitorCoupons bool,
) (*TelegramChannel, error) {
	if chatID == 0 {
		return nil, ErrInvalidChannel
	}
	cleanTitle := strings.TrimSpace(title)
	if cleanTitle == "" {
		return nil, ErrInvalidChannel
	}

	return &TelegramChannel{
		ChatID:          chatID,
		AccessHash:      accessHash,
		Username:        strings.TrimSpace(username),
		Title:           cleanTitle,
		MonitorProducts: monitorProducts,
		MonitorCoupons:  monitorCoupons,
		UpdatedAt:       time.Now().UTC(),
	}, nil
}
