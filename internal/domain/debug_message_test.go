package domain_test

import (
	"testing"
	"time"

	"github.com/matrocheetos/telegram-monitor/internal/domain"
)

func TestExtractAllPrices(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected []float64
	}{
		{
			name:     "multiple prices - de por parcelado",
			input:    "De R$ 2.999,00 por R$ 1.899,50 no PIX ou 10x de R$ 200,00",
			expected: []float64{2999.00, 1899.50, 200.00},
		},
		{
			name:     "single price",
			input:    "Oferta monitor por R$ 999,99 imperdível",
			expected: []float64{999.99},
		},
		{
			name:     "no prices",
			input:    "Apenas texto sem nenhum valor monetario",
			expected: []float64{},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			prices := domain.ExtractAllPrices(tc.input)
			if len(prices) != len(tc.expected) {
				t.Fatalf("expected %d prices, got %d (%v)", len(tc.expected), len(prices), prices)
			}
			for i, p := range prices {
				if p != tc.expected[i] {
					t.Errorf("at index %d: expected %f, got %f", i, tc.expected[i], p)
				}
			}
		})
	}
}

func TestExtractAllURLs_And_IdentifyStore(t *testing.T) {
	t.Parallel()

	text := "Confira a oferta na Amazon https://amzn.to/deal1 e no Mercado Livre https://meli.la/deal2"
	urls := domain.ExtractAllURLs(text)
	if len(urls) != 2 {
		t.Fatalf("expected 2 urls, got %d: %v", len(urls), urls)
	}
	if urls[0] != "https://amzn.to/deal1" || urls[1] != "https://meli.la/deal2" {
		t.Errorf("unexpected extracted urls: %v", urls)
	}

	storeName, storeURL := domain.IdentifyStoreFromText(text)
	if storeName != "Amazon" {
		t.Errorf("expected Amazon store, got %s", storeName)
	}
	if storeURL != "https://amzn.to/deal1" {
		t.Errorf("expected deal1 url, got %s", storeURL)
	}

	unrecognizedText := "Promoção desconhecida em https://lojanova.com/produto"
	unrecStore, _ := domain.IdentifyStoreFromText(unrecognizedText)
	if unrecStore != "Oferta" {
		t.Errorf("expected Oferta fallback, got %s", unrecStore)
	}
}

func TestAnalyzedMessageDebugRecord(t *testing.T) {
	t.Parallel()

	rec := domain.AnalyzedMessageDebugRecord{
		TelegramMessageID: 12345,
		ChannelID:         -100123,
		ChannelTitle:      "Canal Teste",
		ChannelUsername:   "canalteste",
		MonitorProducts:   true,
		MonitorCoupons:    true,
		RawText:           "Cupom teste na Amazon R$ 50,00",
		NormalizedText:    "cupom teste na amazon r 50 00",
		Tokens:            []string{"cupom", "teste", "na", "amazon", "r", "50", "00"},
		HasMedia:          true,
		MediaType:         "photo",
		EvaluationOutcome: domain.OutcomeMatchedProductAndCoupon,
		MessageDate:       time.Now().UTC(),
		AnalyzedAt:        time.Now().UTC(),
	}

	if rec.EvaluationOutcome != domain.OutcomeMatchedProductAndCoupon {
		t.Errorf("unexpected outcome: %s", rec.EvaluationOutcome)
	}
}
