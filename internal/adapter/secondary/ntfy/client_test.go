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
	"github.com/matrocheetos/telegram-monitor/internal/domain"
)

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

func TestClient_SendProductNotification(t *testing.T) {
	t.Parallel()

	type actionButton struct {
		Action string `json:"action"`
		Label  string `json:"label"`
		Value  string `json:"value"`
	}

	type requestData struct {
		Topic    string         `json:"topic"`
		Title    string         `json:"title"`
		Message  string         `json:"message"`
		Priority int            `json:"priority"`
		Click    string         `json:"click"`
		Actions  []actionButton `json:"actions"`
	}

	var receivedData requestData
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
	ctx := context.Background()
	err := client.SendProductNotification(ctx, "mateus", "https://amzn.to/offer", &price, "Nintendo Switch OLED")
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
	if receivedData.Title != "🎯 Oferta Encontrada!" {
		t.Errorf("expected title 🎯 Oferta Encontrada!, got %s", receivedData.Title)
	}
	if receivedData.Priority != 4 {
		t.Errorf("expected priority 4, got %d", receivedData.Priority)
	}
	if receivedData.Click != "https://amzn.to/offer" {
		t.Errorf("expected click url, got %s", receivedData.Click)
	}
	if !strings.Contains(receivedData.Message, "1999.99") {
		t.Errorf("expected price in message, got %s", receivedData.Message)
	}
}

func TestClient_SendCouponNotification(t *testing.T) {
	t.Parallel()

	type actionButton struct {
		Action string `json:"action"`
		Label  string `json:"label"`
		Value  string `json:"value"`
	}

	type requestData struct {
		Topic    string         `json:"topic"`
		Title    string         `json:"title"`
		Message  string         `json:"message"`
		Priority int            `json:"priority"`
		Click    string         `json:"click"`
		Actions  []actionButton `json:"actions"`
	}

	var receivedData requestData
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

	coupon := domain.Coupon{
		Code:      "CARRINHO10",
		StoreName: "Mercado Livre",
		StoreURL:  "https://meli.la/deal",
		CreatedAt: time.Now().UTC(),
	}

	err := client.SendCouponNotification(context.Background(), coupon)
	if err != nil {
		t.Fatalf("SendCouponNotification failed: %v", err)
	}

	if receivedPath != "/" {
		t.Errorf("expected path /, got %s", receivedPath)
	}
	if receivedData.Topic != "promo-cupom" {
		t.Errorf("expected topic promo-cupom, got %s", receivedData.Topic)
	}
	if receivedData.Priority != 4 {
		t.Errorf("expected priority 4, got %d", receivedData.Priority)
	}
	if receivedData.Click != "https://meli.la/deal" {
		t.Errorf("expected click url, got %s", receivedData.Click)
	}
	if len(receivedData.Actions) != 1 {
		t.Fatalf("expected 1 action, got %d", len(receivedData.Actions))
	}
	action := receivedData.Actions[0]
	if action.Action != "copy" {
		t.Errorf("expected action copy, got %s", action.Action)
	}
	if action.Label != "Copiar Cupom" {
		t.Errorf("expected label Copiar Cupom, got %s", action.Label)
	}
	if action.Value != "CARRINHO10" {
		t.Errorf("expected value CARRINHO10, got %s", action.Value)
	}
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

		err := client.SendProductNotification(context.Background(), "user", "", nil, "text")
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

		err := client.SendProductNotification(context.Background(), "user", "", nil, "text")
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

		err := client.SendProductNotification(context.Background(), "user", "", nil, "text")
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
		err := client.SendProductNotification(context.Background(), "user", "", nil, "text")
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
		err := client.SendProductNotification(context.Background(), "user", "", nil, "text")
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

		err := client.SendProductNotification(ctx, "user", "", nil, "text")
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

		err := prefixClient.SendProductNotification(context.Background(), "mateus", "https://deal", nil, "test")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if receivedTopic != "test-promo-user-mateus" {
			t.Errorf("expected test-promo-user-mateus, got %s", receivedTopic)
		}

		err = prefixClient.SendCouponNotification(context.Background(), domain.Coupon{Code: "CUPOM", StoreName: "Loja"})
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
		if err := client.SendProductNotification(context.Background(), "user", "", nil, "text"); err != nil {
			t.Fatalf("expected bounded drain to succeed, got %v", err)
		}
	})
}
