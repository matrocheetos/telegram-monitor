package domain

import (
	"fmt"
	"strings"
	"time"
)

const telegramChannelIDPrefix int64 = 1000000000000

func BuildTelegramMessageLink(chatID int64, channelUsername string, messageID int) string {
	if messageID <= 0 {
		return ""
	}

	cleanUsername := strings.TrimPrefix(strings.TrimSpace(channelUsername), "@")
	if cleanUsername != "" {
		return fmt.Sprintf("https://t.me/%s/%d", cleanUsername, messageID)
	}

	if chatID < -telegramChannelIDPrefix {
		rawChannelID := -(chatID + telegramChannelIDPrefix)
		return fmt.Sprintf("https://t.me/c/%d/%d", rawChannelID, messageID)
	}

	return ""
}

type ChannelFlagUpdate struct {
	ChatID          int64
	MonitorProducts bool
	MonitorCoupons  bool
}

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
