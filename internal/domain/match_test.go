package domain_test

import (
	"testing"

	"github.com/matrocheetos/telegram-monitor/internal/domain"
)

func TestNormalizeText(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "accents and uppercase",
			input:    "🔥 PROMOÇÃO EXCLUSIVA DE VERÃO!",
			expected: "promocao exclusiva de verao",
		},
		{
			name:     "alphanumeric boundary letter-digit",
			input:    "Placa de Vídeo RTX4060 Ti 8GB",
			expected: "placa de video rtx 4060 ti 8 gb",
		},
		{
			name:     "alphanumeric boundary digit-letter",
			input:    "SSD NVMe 512gb PCIe 4.0",
			expected: "ssd nvme 512 gb pcie 4 0",
		},
		{
			name:     "smartphone model boundary",
			input:    "Samsung Galaxy S25 Ultra 256GB",
			expected: "samsung galaxy s 25 ultra 256 gb",
		},
		{
			name:     "mixed symbols and emojis",
			input:    "⚡[KABUM] 🏷️ Monitor Gamer 144Hz: R$ 899,00 (À VISTA)",
			expected: "kabum monitor gamer 144 hz r 899 00 a vista",
		},
		{
			name:     "empty text",
			input:    "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := domain.NormalizeText(tt.input)
			if got != tt.expected {
				t.Errorf("NormalizeText(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestExtractLowestPrice(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		text          string
		expectedPrice float64
		expectedFound bool
	}{
		{
			name:          "standard brazilian price with comma",
			text:          "Smartphone Motorola por apenas R$ 1.299,00 no Pix",
			expectedPrice: 1299.00,
			expectedFound: true,
		},
		{
			name:          "cents only",
			text:          "Cabo USB-C por R$ 19,90",
			expectedPrice: 19.90,
			expectedFound: true,
		},
		{
			name:          "dot as decimal",
			text:          "Headset gamer R$ 149.99 na Amazon",
			expectedPrice: 149.99,
			expectedFound: true,
		},
		{
			name:          "multiple prices picks lowest",
			text:          "De R$ 4.599,00 por R$ 3.899,00 à vista ou R$ 4.199,00 a prazo",
			expectedPrice: 3899.00,
			expectedFound: true,
		},
		{
			name:          "no price in message",
			text:          "Aproveite o cupom de desconto no link da bio",
			expectedPrice: 0,
			expectedFound: false,
		},
		{
			name:          "multiple dots thousand separator",
			text:          "Imóvel ou carro por R$ 1.000.000",
			expectedPrice: 1000000,
			expectedFound: true,
		},
		{
			name:          "single dot thousand separator",
			text:          "Monitor gamer por R$ 1.500 à vista",
			expectedPrice: 1500,
			expectedFound: true,
		},
		{
			name:          "comma thousand and dot decimal",
			text:          "Importado por R$ 1,299.00",
			expectedPrice: 1299.00,
			expectedFound: true,
		},
		{
			name:          "ignores installment when cash price is present",
			text:          "R$ 1.899,00 à vista ou 12x de R$ 175,00 sem juros",
			expectedPrice: 1899.00,
			expectedFound: true,
		},
		{
			name:          "ignores leading installment when cash price follows",
			text:          "12x de R$ 175,00 sem juros ou R$ 1.899,00 à vista",
			expectedPrice: 1899.00,
			expectedFound: true,
		},
		{
			name:          "installment only returns no cash price",
			text:          "Parcelado em 12x de R$ 175,00 sem juros",
			expectedPrice: 0,
			expectedFound: false,
		},
		{
			name:          "ignores shipping and cashback prices",
			text:          "Produto R$ 299,90 + Frete R$ 25,00 com cashback de R$ 30,00",
			expectedPrice: 299.90,
			expectedFound: true,
		},
		{
			name:          "ancillary only prices return false",
			text:          "Frete R$ 19,90 | Economize R$ 50,00 | Cupom de R$ 30,00 | Cashback de R$ 20,00",
			expectedPrice: 0,
			expectedFound: false,
		},
		{
			name:          "preserves final price after coupon cue with sai por",
			text:          "Com cupom ABC10 sai por R$ 199,90",
			expectedPrice: 199.90,
			expectedFound: true,
		},
		{
			name:          "preserves price on clause after shipping exclamation",
			text:          "Frete grátis! Apenas R$ 199,90",
			expectedPrice: 199.90,
			expectedFound: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			price, found := domain.ExtractLowestPrice(tt.text)
			if found != tt.expectedFound {
				t.Fatalf("ExtractLowestPrice(%q) found = %v, want %v", tt.text, found, tt.expectedFound)
			}
			if found && price != tt.expectedPrice {
				t.Errorf("ExtractLowestPrice(%q) price = %v, want %v", tt.text, price, tt.expectedPrice)
			}
		})
	}
}

func TestMatchProduct(t *testing.T) {
	t.Parallel()

	maxPriceSwitch := 2200.0
	strictPriceSwitch := 1500.0

	tests := []struct {
		name        string
		rawTerm     string
		maxPrice    *float64
		messageText string
		wantMatch   bool
	}{
		{
			name:        "exact bag-of-words and match in free order",
			rawTerm:     "switch oled branco",
			maxPrice:    &maxPriceSwitch,
			messageText: "🔥 Console Nintendo Switch Modelo OLED - Cor Branco por apenas R$ 1.899,00 no Pix",
			wantMatch:   true,
		},
		{
			name:        "negative token drops match",
			rawTerm:     "switch oled -capa -pelicula",
			maxPrice:    nil,
			messageText: "🔥 Capa protetora para Nintendo Switch OLED modelo branco R$ 49,90",
			wantMatch:   false,
		},
		{
			name:        "alphanumeric boundary rtx4060 matches rtx 4060",
			rawTerm:     "rtx 4060",
			maxPrice:    nil,
			messageText: "Placa de Vídeo Galax RTX4060 1-Click OC 2X por R$ 1.999",
			wantMatch:   true,
		},
		{
			name:        "fuzzy matching on long token len >= 5 with distance 1",
			rawTerm:     "nintedo switch",
			maxPrice:    nil,
			messageText: "Novo Nintendo Switch OLED em super oferta",
			wantMatch:   true,
		},
		{
			name:        "short tokens never fuzzy match (no false positives)",
			rawTerm:     "tv oled",
			maxPrice:    nil,
			messageText: "Monitor Gamer LG 24 polegadas 144Hz",
			wantMatch:   false,
		},
		{
			name:        "price exceeds max price",
			rawTerm:     "switch oled",
			maxPrice:    &strictPriceSwitch,
			messageText: "Console Nintendo Switch OLED por R$ 1.899,00",
			wantMatch:   false,
		},
		{
			name:        "missing required price when max price is specified",
			rawTerm:     "switch oled",
			maxPrice:    &maxPriceSwitch,
			messageText: "Console Nintendo Switch OLED disponível na loja (consulte o site)",
			wantMatch:   false,
		},
		{
			name:        "installment only price does not satisfy max price",
			rawTerm:     "switch oled",
			maxPrice:    &maxPriceSwitch,
			messageText: "Console Nintendo Switch OLED em 12x de R$ 225,00 sem juros",
			wantMatch:   false,
		},
		{
			name:        "installment price does not bypass max price check when cash price exceeds limit",
			rawTerm:     "switch oled",
			maxPrice:    &maxPriceSwitch,
			messageText: "Console Nintendo Switch OLED por R$ 2.500,00 ou 12x de R$ 225,00 sem juros",
			wantMatch:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			interest, err := domain.NewUserInterest(1, tt.rawTerm, tt.maxPrice)
			if err != nil {
				t.Fatalf("unexpected error creating interest: %v", err)
			}

			match, isMatch := domain.MatchProduct(interest, tt.messageText)
			if isMatch != tt.wantMatch {
				t.Fatalf("MatchProduct() match = %v, want %v (term=%q, msg=%q)",
					isMatch, tt.wantMatch, tt.rawTerm, tt.messageText)
			}
			if isMatch && match == nil {
				t.Errorf("expected non-nil match on successful match")
			}
		})
	}

	t.Run("match extracted price uses cash price instead of installment", func(t *testing.T) {
		t.Parallel()
		interest, err := domain.NewUserInterest(1, "switch oled", nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		match, ok := domain.MatchProduct(interest, "Console Nintendo Switch OLED por R$ 1.899,00 ou 12x de R$ 175,00 sem juros")
		if !ok || match == nil || match.ExtractedPrice == nil || *match.ExtractedPrice != 1899.00 {
			t.Fatalf("expected ExtractedPrice 1899.00, got %+v", match)
		}
	})
}

func BenchmarkMatchMessage(b *testing.B) {
	interest, err := domain.NewUserInterest(1, "switch oled branco -capa -pelicula", nil)
	if err != nil {
		b.Fatalf("failed to create interest: %v", err)
	}

	message := "🔥 Console Nintendo Switch Modelo OLED - Cor Branco por apenas R$ 1.899,00 no Pix com frete grátis na Amazon Brasil!"

	b.ResetTimer()
	b.ReportAllocs()

	for b.Loop() {
		_, _ = domain.MatchProduct(interest, message)
	}
}
