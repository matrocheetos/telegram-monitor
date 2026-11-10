package apprise

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/matrocheetos/telegram-monitor/internal/domain"
	"github.com/matrocheetos/telegram-monitor/internal/port"
)

var (
	ErrEmptyAppriseURL      = errors.New("apprise: apprise url is empty")
	ErrAppriseRequestFailed = errors.New("apprise: request failed")
)

type Config struct {
	AppriseURL         string
	AppriseKey         string
	NtfyURL            string
	NtfyTopicSeparator string
	NtfyTopicPrefix    string
	HTTPClient         *http.Client
}

type Client struct {
	appriseURL         string
	appriseKey         string
	ntfyBaseURL        string
	ntfyScheme         string
	ntfyHost           string
	ntfyTopicSeparator string
	ntfyTopicPrefix    string
	httpClient         *http.Client
}

var _ port.NotificationGateway = (*Client)(nil)

func NewClient(cfg Config) *Client {
	sep := cfg.NtfyTopicSeparator
	if sep == "" {
		sep = "-"
	}

	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{
			Timeout: 10 * time.Second,
		}
	}

	scheme := "ntfys"
	host := ""
	cleanNtfyURL := strings.TrimRight(cfg.NtfyURL, "/")

	if cleanNtfyURL != "" {
		parsed, err := url.Parse(cleanNtfyURL)
		if err == nil {
			if strings.EqualFold(parsed.Scheme, "http") {
				scheme = "ntfy"
			} else {
				scheme = "ntfys"
			}
			host = parsed.Host
		}
		if host == "" {
			host = cleanNtfyURL
		}
	}

	return &Client{
		appriseURL:         strings.TrimRight(cfg.AppriseURL, "/"),
		appriseKey:         strings.TrimSpace(cfg.AppriseKey),
		ntfyBaseURL:        cleanNtfyURL,
		ntfyScheme:         scheme,
		ntfyHost:           host,
		ntfyTopicSeparator: sep,
		ntfyTopicPrefix:    cfg.NtfyTopicPrefix,
		httpClient:         httpClient,
	}
}

func (c *Client) NormalizeTopic(logicalTopic string) string {
	trimmed := strings.Trim(logicalTopic, "/")
	parts := strings.Split(trimmed, "/")
	cleanParts := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			cleanParts = append(cleanParts, p)
		}
	}
	return strings.Join(cleanParts, c.ntfyTopicSeparator)
}

func (c *Client) PublicTopicURL(logicalTopic string) string {
	topic := c.NormalizeTopic(logicalTopic)
	if c.ntfyBaseURL == "" {
		return topic
	}
	return fmt.Sprintf("%s/%s", c.ntfyBaseURL, topic)
}

func (c *Client) BuildAppriseURL(logicalTopic, clickURL, couponCode string) string {
	topic := c.NormalizeTopic(logicalTopic)
	host := c.ntfyHost
	if host == "" {
		host = "ntfy.sh"
	}

	base := fmt.Sprintf("%s://%s/%s", c.ntfyScheme, host, topic)

	query := make(url.Values)
	query.Set("priority", "high")

	if clickURL != "" {
		query.Set("click", clickURL)
	}

	if couponCode != "" {
		query.Set("actions", fmt.Sprintf("copy,Copiar Cupom,%s", couponCode))
	}

	return fmt.Sprintf("%s?%s", base, query.Encode())
}

type notifyPayload struct {
	URLs  string `json:"urls"`
	Title string `json:"title"`
	Body  string `json:"body"`
}

func (c *Client) SendProductNotification(ctx context.Context, username, offerURL string, price *float64, text string) error {
	logicalTopic := c.ntfyTopicPrefix + "promo/user/" + username
	appriseTarget := c.BuildAppriseURL(logicalTopic, offerURL, "")

	title := "🎯 Oferta Encontrada!"
	var bodyBuilder strings.Builder
	if price != nil {
		fmt.Fprintf(&bodyBuilder, "💰 Preço: R$ %.2f\n\n", *price)
	}
	bodyBuilder.WriteString(text)

	return c.send(ctx, appriseTarget, title, bodyBuilder.String())
}

func (c *Client) SendCouponNotification(ctx context.Context, coupon domain.Coupon) error {
	logicalTopic := c.ntfyTopicPrefix + "promo/cupom"
	appriseTarget := c.BuildAppriseURL(logicalTopic, coupon.StoreURL, coupon.Code)

	title := fmt.Sprintf("🎟️ Cupom: %s (%s)", coupon.Code, coupon.StoreName)
	var bodyBuilder strings.Builder
	fmt.Fprintf(&bodyBuilder, "Loja: %s\n", coupon.StoreName)
	fmt.Fprintf(&bodyBuilder, "Cupom: %s\n", coupon.Code)
	if coupon.StoreURL != "" {
		fmt.Fprintf(&bodyBuilder, "Link: %s\n", coupon.StoreURL)
	}

	return c.send(ctx, appriseTarget, title, bodyBuilder.String())
}

func (c *Client) send(ctx context.Context, targetURL, title, body string) error {
	if c.appriseURL == "" {
		return ErrEmptyAppriseURL
	}

	endpoint := c.appriseURL + "/notify"
	if c.appriseKey != "" {
		endpoint = fmt.Sprintf("%s/notify/%s", c.appriseURL, c.appriseKey)
	}

	payload := notifyPayload{
		URLs:  targetURL,
		Title: title,
		Body:  body,
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("apprise: marshal payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payloadBytes))
	if err != nil {
		return fmt.Errorf("apprise: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("apprise: execute request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("%w: status %d: %s", ErrAppriseRequestFailed, resp.StatusCode, string(bodyBytes))
	}

	return nil
}
