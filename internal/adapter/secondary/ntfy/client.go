package ntfy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/matrocheetos/telegram-monitor/internal/port"
)

var (
	ErrEmptyNtfyURL      = errors.New("ntfy: base url is empty")
	ErrNtfyRequestFailed = errors.New("ntfy: request failed")
)

type Config struct {
	BaseURL        string
	Token          string
	Username       string
	Password       string
	TopicSeparator string
	TopicPrefix    string
	HTTPClient     *http.Client
}

type Client struct {
	baseURL        string
	token          string
	username       string
	password       string
	topicSeparator string
	topicPrefix    string
	httpClient     *http.Client
}

var _ port.NotificationGateway = (*Client)(nil)

func NewClient(cfg Config) *Client {
	sep := cfg.TopicSeparator
	if sep == "" {
		sep = "-"
	}

	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{
			Timeout: 10 * time.Second,
		}
	}

	return &Client{
		baseURL:        strings.TrimRight(cfg.BaseURL, "/"),
		token:          strings.TrimSpace(cfg.Token),
		username:       strings.TrimSpace(cfg.Username),
		password:       cfg.Password,
		topicSeparator: sep,
		topicPrefix:    cfg.TopicPrefix,
		httpClient:     httpClient,
	}
}

func (c *Client) NormalizeTopic(logicalTopic string) string {
	raw := logicalTopic
	if c.topicPrefix != "" && strings.HasPrefix(raw, c.topicPrefix) {
		raw = strings.TrimPrefix(raw, c.topicPrefix)
	}

	trimmed := strings.Trim(raw, "/")
	parts := strings.Split(trimmed, "/")
	cleanParts := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			cleanParts = append(cleanParts, p)
		}
	}
	norm := strings.Join(cleanParts, c.topicSeparator)
	if c.topicPrefix != "" {
		return c.topicPrefix + norm
	}
	return norm
}

func (c *Client) PublicTopicURL(logicalTopic string) string {
	topic := c.NormalizeTopic(logicalTopic)
	if c.baseURL == "" {
		return topic
	}
	return fmt.Sprintf("%s/%s", c.baseURL, topic)
}

type actionButton struct {
	Action string `json:"action"`
	Label  string `json:"label"`
	URL    string `json:"url,omitempty"`
	Value  string `json:"value,omitempty"`
}

type publishPayload struct {
	Topic    string         `json:"topic"`
	Title    string         `json:"title,omitempty"`
	Message  string         `json:"message,omitempty"`
	Priority int            `json:"priority,omitempty"`
	Click    string         `json:"click,omitempty"`
	Actions  []actionButton `json:"actions,omitempty"`
}

func (c *Client) resolveClickURL(telegramMessageURL, logicalTopic string) string {
	if clean := strings.TrimSpace(telegramMessageURL); clean != "" {
		return clean
	}
	return c.PublicTopicURL(logicalTopic)
}

func (c *Client) SendProductNotification(ctx context.Context, notif port.ProductNotification) error {
	logicalTopic := "promo/user/" + notif.Username
	topic := c.NormalizeTopic(logicalTopic)

	matchedTerm := strings.TrimSpace(notif.MatchedTerm)
	title := "🎯 Oferta Encontrada!"
	if matchedTerm != "" {
		title = fmt.Sprintf("🎯 Oferta: %s", matchedTerm)
	}

	var bodyBuilder strings.Builder
	bodyBuilder.Grow(len(notif.Text) + 96)
	hasHeader := false
	if matchedTerm != "" {
		fmt.Fprintf(&bodyBuilder, "🔎 Termo: %s\n", matchedTerm)
		hasHeader = true
	}
	if notif.Price != nil {
		fmt.Fprintf(&bodyBuilder, "💰 Preço: R$ %.2f\n", *notif.Price)
		hasHeader = true
	}
	if hasHeader && notif.Text != "" {
		bodyBuilder.WriteByte('\n')
	}
	bodyBuilder.WriteString(notif.Text)

	var actions []actionButton
	if offerURL := strings.TrimSpace(notif.OfferURL); offerURL != "" {
		actions = append(actions, actionButton{
			Action: "view",
			Label:  "Abrir Link",
			URL:    offerURL,
		})
	}
	if couponCode := strings.TrimSpace(notif.CouponCode); couponCode != "" {
		actions = append(actions, actionButton{
			Action: "copy",
			Label:  "Copiar Cupom",
			Value:  couponCode,
		})
	}

	payload := publishPayload{
		Topic:    topic,
		Title:    title,
		Message:  bodyBuilder.String(),
		Priority: 4,
		Click:    c.resolveClickURL(notif.TelegramMessageURL, logicalTopic),
		Actions:  actions,
	}

	return c.publish(ctx, payload)
}

func (c *Client) SendCouponNotification(ctx context.Context, notif port.CouponNotification) error {
	logicalTopic := "promo/cupom"
	topic := c.NormalizeTopic(logicalTopic)

	code := strings.TrimSpace(notif.Code)
	storeName := strings.TrimSpace(notif.StoreName)
	title := fmt.Sprintf("🎟️ %s - %s", code, storeName)
	if storeName == "" {
		title = fmt.Sprintf("🎟️ %s", code)
	}

	var actions []actionButton
	if storeURL := strings.TrimSpace(notif.StoreURL); storeURL != "" {
		actions = append(actions, actionButton{
			Action: "view",
			Label:  "Abrir Link",
			URL:    storeURL,
		})
	}
	if code != "" {
		actions = append(actions, actionButton{
			Action: "copy",
			Label:  "Copiar Cupom",
			Value:  code,
		})
	}

	payload := publishPayload{
		Topic:    topic,
		Title:    title,
		Message:  notif.Text,
		Priority: 4,
		Click:    c.resolveClickURL(notif.TelegramMessageURL, logicalTopic),
		Actions:  actions,
	}

	return c.publish(ctx, payload)
}

func (c *Client) publish(ctx context.Context, payload publishPayload) error {
	if c.baseURL == "" {
		return ErrEmptyNtfyURL
	}

	endpoint := c.baseURL + "/"

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("ntfy: marshal payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payloadBytes))
	if err != nil {
		return fmt.Errorf("ntfy: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	} else if c.username != "" && c.password != "" {
		req.SetBasicAuth(c.username, c.password)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("ntfy: execute request: %w", err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		_ = resp.Body.Close()
	}()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("%w: status %d: %s", ErrNtfyRequestFailed, resp.StatusCode, string(bodyBytes))
	}

	return nil
}
