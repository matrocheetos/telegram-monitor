package ntfy_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/matrocheetos/telegram-monitor/internal/adapter/secondary/ntfy"
	"github.com/matrocheetos/telegram-monitor/internal/port"
)

type capturedActionButton struct {
	Action string `json:"action"`
	Label  string `json:"label"`
	URL    string `json:"url"`
	Value  string `json:"value"`
}

type capturedRequestData struct {
	Topic    string                 `json:"topic"`
	Title    string                 `json:"title"`
	Message  string                 `json:"message"`
	Priority int                    `json:"priority"`
	Click    string                 `json:"click"`
	Actions  []capturedActionButton `json:"actions"`
}

func TestClient_TopicFormatting(t *testing.T) {
	t.Parallel()

	cfg := ntfy.Config{
		BaseURL:        "https://ntfy.example.com",
		TopicSeparator: "-",
	}
	client := ntfy.NewClient(cfg)

	t.Run("NormalizeTopic", func(t *testing.T) {
		tests := []struct {
			input    string
			expected string
		}{
			{"promo/user/mateus", "promo-user-mateus"},
			{"/promo/cupom/", "promo-cupom"},
			{"promo//user///joao", "promo-user-joao"},
			{"simple", "simple"},
		}

		for _, tc := range tests {
			actual := client.NormalizeTopic(tc.input)
			if actual != tc.expected {
				t.Errorf("NormalizeTopic(%q) = %q, expected %q", tc.input, actual, tc.expected)
			}
		}
	})

	t.Run("PublicTopicURL", func(t *testing.T) {
		pubURL := client.PublicTopicURL("promo/user/mateus")
		expected := "https://ntfy.example.com/promo-user-mateus"
		if pubURL != expected {
			t.Errorf("expected %q, got %q", expected, pubURL)
		}
	})

	t.Run("TopicPrefix", func(t *testing.T) {
		prefixClient := ntfy.NewClient(ntfy.Config{
			BaseURL:        "https://ntfy.example.com",
			TopicSeparator: "-",
			TopicPrefix:    "test-",
		})

		actual := prefixClient.NormalizeTopic("promo/user/mateus")
		expected := "test-promo-user-mateus"
		if actual != expected {
			t.Errorf("expected %q, got %q", expected, actual)
		}

		actualAlreadyPrefixed := prefixClient.NormalizeTopic("test-promo/user/mateus")
		if actualAlreadyPrefixed != expected {
			t.Errorf("expected %q, got %q", expected, actualAlreadyPrefixed)
		}

		pubURL := prefixClient.PublicTopicURL("promo/user/mateus")
		expectedURL := "https://ntfy.example.com/test-promo-user-mateus"
		if pubURL != expectedURL {
			t.Errorf("expected %q, got %q", expectedURL, pubURL)
		}
	})
}

func TestClient_SendProductNotification_WithoutCoupon(t *testing.T) {
	t.Parallel()

	var receivedData capturedRequestData
	var receivedMethod string
	var receivedPath string
	var receivedContentType string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedMethod = r.Method
		receivedPath = r.URL.Path
		receivedContentType = r.Header.Get("Content-Type")

		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if err := json.Unmarshal(body, &receivedData); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"success": true}`))
	}))
	t.Cleanup(server.Close)

	client := ntfy.NewClient(ntfy.Config{
		BaseURL:        server.URL,
		TopicSeparator: "-",
	})

	price := 1999.99
	originalText := "Console Nintendo Switch OLED Branco lacrado! https://amzn.to/offer"
	err := client.SendProductNotification(context.Background(), port.ProductNotification{
		Username:           "mateus",
		MatchedTerm:        "nintendo switch oled",
		Price:              &price,
		Text:               originalText,
		OfferURL:           "https://amzn.to/offer",
		TelegramMessageURL: "https://t.me/achadosmeli/4651",
	})
	if err != nil {
		t.Fatalf("SendProductNotification failed: %v", err)
	}

	if receivedMethod != http.MethodPost {
		t.Errorf("expected POST, got %s", receivedMethod)
	}
	if receivedPath != "/" {
		t.Errorf("expected path /, got %s", receivedPath)
	}
	if receivedContentType != "application/json" {
		t.Errorf("expected application/json, got %s", receivedContentType)
	}
	if receivedData.Topic != "promo-user-mateus" {
		t.Errorf("expected topic promo-user-mateus, got %s", receivedData.Topic)
	}
	if receivedData.Title != "🎯 Oferta: nintendo switch oled" {
		t.Errorf("expected title '🎯 Oferta: nintendo switch oled', got %q", receivedData.Title)
	}
	if receivedData.Priority != 4 {
		t.Errorf("expected priority 4, got %d", receivedData.Priority)
	}
	if receivedData.Click != "https://t.me/achadosmeli/4651" {
		t.Errorf("expected Telegram click url, got %q", receivedData.Click)
	}
	if !strings.Contains(receivedData.Message, "🔎 Termo: nintendo switch oled") {
		t.Errorf("expected matched term in message, got %q", receivedData.Message)
	}
	if !strings.Contains(receivedData.Message, "1999.99") {
		t.Errorf("expected price in message, got %q", receivedData.Message)
	}
	if !strings.Contains(receivedData.Message, originalText) {
		t.Errorf("expected original text in message, got %q", receivedData.Message)
	}
	if len(receivedData.Actions) != 1 {
		t.Fatalf("expected 1 action button (Abrir Link only), got %d: %+v", len(receivedData.Actions), receivedData.Actions)
	}
	viewBtn := receivedData.Actions[0]
	if viewBtn.Action != "view" || viewBtn.Label != "Abrir Link" || viewBtn.URL != "https://amzn.to/offer" {
		t.Errorf("unexpected view action: %+v", viewBtn)
	}
}

func TestClient_SendProductNotification_WithCoupon(t *testing.T) {
	t.Parallel()

	var receivedData capturedRequestData

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if err := json.Unmarshal(body, &receivedData); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	client := ntfy.NewClient(ntfy.Config{
		BaseURL:        server.URL,
		TopicSeparator: "-",
	})

	price := 1850.00
	originalText := "Console Nintendo Switch OLED\n🎟️ Cupom: SWITCH150\nhttps://meli.la/switch"
	err := client.SendProductNotification(context.Background(), port.ProductNotification{
		Username:           "mateus",
		MatchedTerm:        "switch oled",
		Price:              &price,
		Text:               originalText,
		OfferURL:           "https://meli.la/switch",
		CouponCode:         "SWITCH150",
		TelegramMessageURL: "https://t.me/c/1483635032/99",
	})
	if err != nil {
		t.Fatalf("SendProductNotification failed: %v", err)
	}

	if receivedData.Click != "https://t.me/c/1483635032/99" {
		t.Errorf("expected private Telegram click url, got %q", receivedData.Click)
	}
	if !strings.Contains(receivedData.Message, originalText) {
		t.Errorf("expected original text in message, got %q", receivedData.Message)
	}
	if len(receivedData.Actions) != 2 {
		t.Fatalf("expected 2 action buttons, got %d: %+v", len(receivedData.Actions), receivedData.Actions)
	}
	if receivedData.Actions[0] != (capturedActionButton{
		Action: "view",
		Label:  "Abrir Link",
		URL:    "https://meli.la/switch",
	}) {
		t.Errorf("unexpected first action: %+v", receivedData.Actions[0])
	}
	if receivedData.Actions[1] != (capturedActionButton{
		Action: "copy",
		Label:  "Copiar Cupom",
		Value:  "SWITCH150",
	}) {
		t.Errorf("unexpected second action: %+v", receivedData.Actions[1])
	}
}

func TestClient_SendCouponNotification(t *testing.T) {
	t.Parallel()

	var receivedData capturedRequestData
	var receivedPath string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedPath = r.URL.Path
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if err := json.Unmarshal(body, &receivedData); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	client := ntfy.NewClient(ntfy.Config{
		BaseURL: server.URL,
	})

	originalMsg := "🚨 Cupom Mercado Livre!\n🏷️ 10% OFF acima de R$ 99: PROMO10\nResgate: https://meli.la/deal"
	err := client.SendCouponNotification(context.Background(), port.CouponNotification{
		Code:               "PROMO10",
		StoreName:          "Mercado Livre",
		StoreURL:           "https://meli.la/deal",
		Text:               originalMsg,
		TelegramMessageURL: "https://t.me/achadosmeli/4651",
	})
	if err != nil {
		t.Fatalf("SendCouponNotification failed: %v", err)
	}

	if receivedPath != "/" {
		t.Errorf("expected path /, got %s", receivedPath)
	}
	if receivedData.Topic != "promo-cupom" {
		t.Errorf("expected topic promo-cupom, got %s", receivedData.Topic)
	}
	if receivedData.Title != "🎟️ PROMO10 - Mercado Livre" {
		t.Errorf("expected title '🎟️ PROMO10 - Mercado Livre', got %q", receivedData.Title)
	}
	if receivedData.Message != originalMsg {
		t.Errorf("expected original message body %q, got %q", originalMsg, receivedData.Message)
	}
	if receivedData.Priority != 4 {
		t.Errorf("expected priority 4, got %d", receivedData.Priority)
	}
	if receivedData.Click != "https://t.me/achadosmeli/4651" {
		t.Errorf("expected click url https://t.me/achadosmeli/4651, got %q", receivedData.Click)
	}
	if len(receivedData.Actions) != 2 {
		t.Fatalf("expected 2 actions, got %d: %+v", len(receivedData.Actions), receivedData.Actions)
	}
	if receivedData.Actions[0] != (capturedActionButton{
		Action: "view",
		Label:  "Abrir Link",
		URL:    "https://meli.la/deal",
	}) {
		t.Errorf("unexpected first action: %+v", receivedData.Actions[0])
	}
	if receivedData.Actions[1] != (capturedActionButton{
		Action: "copy",
		Label:  "Copiar Cupom",
		Value:  "PROMO10",
	}) {
		t.Errorf("unexpected second action: %+v", receivedData.Actions[1])
	}
}

func TestClient_ClickFallbackToPublicTopicURL(t *testing.T) {
	t.Parallel()

	var receivedData capturedRequestData
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if err := json.Unmarshal(body, &receivedData); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	client := ntfy.NewClient(ntfy.Config{
		BaseURL:        server.URL,
		TopicSeparator: "-",
		TopicPrefix:    "test-",
	})

	t.Run("product click falls back to user ntfy topic URL", func(t *testing.T) {
		err := client.SendProductNotification(context.Background(), port.ProductNotification{
			Username:    "mateus",
			MatchedTerm: "rtx 4060",
			Text:        "Placa RTX 4060 em oferta",
			OfferURL:    "https://kabum.com.br/rtx",
		})
		if err != nil {
			t.Fatalf("SendProductNotification failed: %v", err)
		}
		wantClick := server.URL + "/test-promo-user-mateus"
		if receivedData.Click != wantClick {
			t.Errorf("expected fallback click %q, got %q", wantClick, receivedData.Click)
		}
	})

	t.Run("coupon click falls back to coupon ntfy topic URL", func(t *testing.T) {
		err := client.SendCouponNotification(context.Background(), port.CouponNotification{
			Code:      "KABUM10",
			StoreName: "Kabum",
			StoreURL:  "https://kabum.com.br",
			Text:      "Cupom Kabum: KABUM10",
		})
		if err != nil {
			t.Fatalf("SendCouponNotification failed: %v", err)
		}
		wantClick := server.URL + "/test-promo-cupom"
		if receivedData.Click != wantClick {
			t.Errorf("expected fallback click %q, got %q", wantClick, receivedData.Click)
		}
	})
}

func TestClient_Authentication(t *testing.T) {
	t.Parallel()

	t.Run("BearerToken", func(t *testing.T) {
		var authHeader string
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authHeader = r.Header.Get("Authorization")
			w.WriteHeader(http.StatusOK)
		}))
		t.Cleanup(server.Close)

		client := ntfy.NewClient(ntfy.Config{
			BaseURL: server.URL,
			Token:   "tk_test_token_123", // #nosec G101 -- test dummy token
		})

		err := client.SendProductNotification(context.Background(), port.ProductNotification{
			Username: "user",
			Text:     "text",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if authHeader != "Bearer tk_test_token_123" {
			t.Errorf("expected Bearer token, got %q", authHeader)
		}
	})

	t.Run("BasicAuth", func(t *testing.T) {
		var authHeader string
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authHeader = r.Header.Get("Authorization")
			w.WriteHeader(http.StatusOK)
		}))
		t.Cleanup(server.Close)

		client := ntfy.NewClient(ntfy.Config{
			BaseURL:  server.URL,
			Username: "admin",
			Password: "password123", // #nosec G101 -- test dummy credentials
		})

		err := client.SendProductNotification(context.Background(), port.ProductNotification{
			Username: "user",
			Text:     "text",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		expected := "Basic " + base64.StdEncoding.EncodeToString([]byte("admin:password123"))
		if authHeader != expected {
			t.Errorf("expected %q, got %q", expected, authHeader)
		}
	})

	t.Run("NoAuth", func(t *testing.T) {
		var authHeader string
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authHeader = r.Header.Get("Authorization")
			w.WriteHeader(http.StatusOK)
		}))
		t.Cleanup(server.Close)

		client := ntfy.NewClient(ntfy.Config{
			BaseURL: server.URL,
		})

		err := client.SendProductNotification(context.Background(), port.ProductNotification{
			Username: "user",
			Text:     "text",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if authHeader != "" {
			t.Errorf("expected empty auth header, got %q", authHeader)
		}
	})
}

func TestClient_ErrorHandling(t *testing.T) {
	t.Parallel()

	t.Run("EmptyNtfyURL", func(t *testing.T) {
		client := ntfy.NewClient(ntfy.Config{})
		err := client.SendProductNotification(context.Background(), port.ProductNotification{
			Username: "user",
			Text:     "text",
		})
		if !errors.Is(err, ntfy.ErrEmptyNtfyURL) {
			t.Errorf("expected ErrEmptyNtfyURL, got %v", err)
		}
	})

	t.Run("ServerError", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "internal server error", http.StatusInternalServerError)
		}))
		t.Cleanup(server.Close)

		client := ntfy.NewClient(ntfy.Config{BaseURL: server.URL})
		err := client.SendProductNotification(context.Background(), port.ProductNotification{
			Username: "user",
			Text:     "text",
		})
		if !errors.Is(err, ntfy.ErrNtfyRequestFailed) {
			t.Errorf("expected ErrNtfyRequestFailed, got %v", err)
		}
	})

	t.Run("ContextCancelled", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(100 * time.Millisecond)
			w.WriteHeader(http.StatusOK)
		}))
		t.Cleanup(server.Close)

		client := ntfy.NewClient(ntfy.Config{BaseURL: server.URL})
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		err := client.SendProductNotification(ctx, port.ProductNotification{
			Username: "user",
			Text:     "text",
		})
		if err == nil {
			t.Errorf("expected error with cancelled context, got nil")
		}
	})

	t.Run("Prefix_Applied_In_Notifications", func(t *testing.T) {
		var receivedTopic string
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var payload struct {
				Topic string `json:"topic"`
			}
			_ = json.NewDecoder(r.Body).Decode(&payload)
			receivedTopic = payload.Topic
			w.WriteHeader(http.StatusOK)
		}))
		t.Cleanup(ts.Close)

		prefixClient := ntfy.NewClient(ntfy.Config{
			BaseURL:        ts.URL,
			TopicSeparator: "-",
			TopicPrefix:    "test-",
		})

		err := prefixClient.SendProductNotification(context.Background(), port.ProductNotification{
			Username: "mateus",
			OfferURL: "https://deal",
			Text:     "test",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if receivedTopic != "test-promo-user-mateus" {
			t.Errorf("expected test-promo-user-mateus, got %s", receivedTopic)
		}

		err = prefixClient.SendCouponNotification(context.Background(), port.CouponNotification{
			Code:      "CUPOM",
			StoreName: "Loja",
			Text:      "Cupom: CUPOM",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if receivedTopic != "test-promo-cupom" {
			t.Errorf("expected test-promo-cupom, got %s", receivedTopic)
		}
	})

	t.Run("LargeSuccessResponseBoundedDrain", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(strings.Repeat("x", 16384)))
		}))
		t.Cleanup(server.Close)

		client := ntfy.NewClient(ntfy.Config{BaseURL: server.URL})
		if err := client.SendProductNotification(context.Background(), port.ProductNotification{
			Username: "user",
			Text:     "text",
		}); err != nil {
			t.Fatalf("expected bounded drain to succeed, got %v", err)
		}
	})
}
