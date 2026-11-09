package apprise_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/matrocheetos/telegram-monitor/internal/adapter/secondary/apprise"
	"github.com/matrocheetos/telegram-monitor/internal/domain"
)

func TestClient_URLFormatting(t *testing.T) {
	t.Parallel()

	cfg := apprise.Config{
		AppriseURL:         "http://localhost:8000",
		NtfyURL:            "https://ntfy.example.com",
		NtfyTopicSeparator: "-",
	}
	client := apprise.NewClient(cfg)

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

	t.Run("BuildAppriseURL_HTTPS", func(t *testing.T) {
		appriseTarget := client.BuildAppriseURL("promo/user/mateus", "https://amazon.com.br/deal", "")
		if !strings.HasPrefix(appriseTarget, "ntfys://ntfy.example.com/promo-user-mateus?") {
			t.Errorf("unexpected prefix: %s", appriseTarget)
		}

		parsed, err := url.Parse(appriseTarget)
		if err != nil {
			t.Fatalf("failed to parse url: %v", err)
		}
		q := parsed.Query()
		if q.Get("priority") != "high" {
			t.Errorf("expected priority=high, got %s", q.Get("priority"))
		}
		if q.Get("click") != "https://amazon.com.br/deal" {
			t.Errorf("expected click param, got %s", q.Get("click"))
		}
	})

	t.Run("BuildAppriseURL_HTTP", func(t *testing.T) {
		httpCfg := apprise.Config{
			NtfyURL:            "http://ntfy.local:8080",
			NtfyTopicSeparator: "_",
		}
		httpClient := apprise.NewClient(httpCfg)
		target := httpClient.BuildAppriseURL("promo/cupom", "https://shopee.com", "PROMO10")

		if !strings.HasPrefix(target, "ntfy://ntfy.local:8080/promo_cupom?") {
			t.Errorf("unexpected url: %s", target)
		}

		parsed, err := url.Parse(target)
		if err != nil {
			t.Fatalf("failed to parse url: %v", err)
		}
		q := parsed.Query()
		if q.Get("actions") != "copy,Copiar Cupom,PROMO10" {
			t.Errorf("expected actions param, got %s", q.Get("actions"))
		}
	})
}

func TestClient_SendProductNotification(t *testing.T) {
	t.Parallel()

	type requestData struct {
		URLs  string `json:"urls"`
		Title string `json:"title"`
		Body  string `json:"body"`
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
		_, _ = w.Write([]byte(`{"success": true}`))
	}))
	t.Cleanup(server.Close)

	client := apprise.NewClient(apprise.Config{
		AppriseURL:         server.URL,
		AppriseKey:         "mykey",
		NtfyURL:            "https://ntfy.example.com",
		NtfyTopicSeparator: "-",
	})

	price := 1999.99
	ctx := context.Background()
	err := client.SendProductNotification(ctx, "mateus", "https://amzn.to/offer", &price, "Nintendo Switch OLED")
	if err != nil {
		t.Fatalf("SendProductNotification failed: %v", err)
	}

	if receivedPath != "/notify/mykey" {
		t.Errorf("expected /notify/mykey, got %s", receivedPath)
	}
	if !strings.Contains(receivedData.URLs, "promo-user-mateus") {
		t.Errorf("expected topic in URL, got %s", receivedData.URLs)
	}
	if !strings.Contains(receivedData.URLs, "click=") {
		t.Errorf("expected click param in URL, got %s", receivedData.URLs)
	}
	if !strings.Contains(receivedData.Body, "1999.99") {
		t.Errorf("expected price in body, got %s", receivedData.Body)
	}
}

func TestClient_SendCouponNotification(t *testing.T) {
	t.Parallel()

	type requestData struct {
		URLs  string `json:"urls"`
		Title string `json:"title"`
		Body  string `json:"body"`
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

	client := apprise.NewClient(apprise.Config{
		AppriseURL: server.URL,
		NtfyURL:    "https://ntfy.example.com",
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

	if receivedPath != "/notify" {
		t.Errorf("expected /notify, got %s", receivedPath)
	}
	if !strings.Contains(receivedData.URLs, "actions=") {
		t.Errorf("expected actions in URLs, got %s", receivedData.URLs)
	}
	if !strings.Contains(receivedData.URLs, "CARRINHO10") {
		t.Errorf("expected coupon code in URLs, got %s", receivedData.URLs)
	}
	if !strings.Contains(receivedData.Body, "CARRINHO10") {
		t.Errorf("expected coupon code in body, got %s", receivedData.Body)
	}
}

func TestClient_ErrorHandling(t *testing.T) {
	t.Parallel()

	t.Run("EmptyAppriseURL", func(t *testing.T) {
		client := apprise.NewClient(apprise.Config{})
		err := client.SendProductNotification(context.Background(), "user", "", nil, "text")
		if !errors.Is(err, apprise.ErrEmptyAppriseURL) {
			t.Errorf("expected ErrEmptyAppriseURL, got %v", err)
		}
	})

	t.Run("ServerError", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "internal server error", http.StatusInternalServerError)
		}))
		t.Cleanup(server.Close)

		client := apprise.NewClient(apprise.Config{AppriseURL: server.URL})
		err := client.SendProductNotification(context.Background(), "user", "", nil, "text")
		if !errors.Is(err, apprise.ErrAppriseRequestFailed) {
			t.Errorf("expected ErrAppriseRequestFailed, got %v", err)
		}
	})

	t.Run("ContextCancelled", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(100 * time.Millisecond)
			w.WriteHeader(http.StatusOK)
		}))
		t.Cleanup(server.Close)

		client := apprise.NewClient(apprise.Config{AppriseURL: server.URL})
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		err := client.SendProductNotification(ctx, "user", "", nil, "text")
		if err == nil {
			t.Errorf("expected error with cancelled context, got nil")
		}
	})
}
