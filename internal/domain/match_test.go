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
			expected: "samsung galaxy s25 ultra 256 gb",
		},
		{
			name:     "preserves iphone 16e and hardware punctuation",
			input:    "Apple iPhone 16-e + SSD M.2 512GB Wi-Fi 6 USB-C",
			expected: "apple iphone 16e ssd m2 512 gb wifi 6 usbc",
		},
		{
			name:     "preserves skus and short prefixes while splitting rx gpu",
			input:    "Ryzen 5 5500F + A520M + DDR5 + i5 13450HX + RX7600",
			expected: "ryzen 5 5500f a520m ddr5 i5 13450hx rx 7600",
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

func BenchmarkParseMessage(b *testing.B) {
	message := "🔥 Console Nintendo Switch 2 OLED 256GB Wi-Fi 6 + RTX4060 S25 Ultra por apenas R$ 1.899,00 no Pix ou em até 12x de R$ 175,00"

	b.ReportAllocs()
	for b.Loop() {
		_ = domain.ParseMessage(message)
	}
}

func BenchmarkMatchParsedMessage(b *testing.B) {
	interest, err := domain.NewUserInterest(1, "switch 2 oled -capa -pelicula", nil)
	if err != nil {
		b.Fatalf("failed to create interest: %v", err)
	}
	parsed := domain.ParseMessage("🔥 Console Nintendo Switch 2 OLED Branco por apenas R$ 2.899,00 no Pix")

	b.ReportAllocs()
	for b.Loop() {
		_ = domain.MatchParsedMessage(interest, &parsed)
	}
}

func TestIsDamerauLevenshteinLE1(t *testing.T) {
	t.Parallel()

	tests := []struct {
		a    string
		b    string
		want bool
	}{
		{"nintendo", "nintendo", true},
		{"nintedo", "nintendo", true},
		{"nintnedo", "nintendo", true},
		{"nintando", "nintendo", true},
		{"playstation", "lpaystation", true},
		{"cadeira", "cadeirass", false},
		{"rtx4060", "rtx4070", true},
		{"switch", "sxitxh", false},
		{"ab", "a", true},
		{"ab", "abcd", false},
	}

	for _, tt := range tests {
		if got := domain.IsDamerauLevenshteinLE1(tt.a, tt.b); got != tt.want {
			t.Errorf("IsDamerauLevenshteinLE1(%q, %q) = %v, want %v", tt.a, tt.b, got, tt.want)
		}
	}
}

func TestMatchProduct_Fase13PrecisionAndRecall(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		rawTerm     string
		messageText string
		wantMatch   bool
	}{
		{
			name:        "iphone 16e matches exact model",
			rawTerm:     "iphone 16e",
			messageText: "Apple iPhone 16e 128GB Preto por R$ 3.799",
			wantMatch:   true,
		},
		{
			name:        "iphone 16e matches hyphenated model",
			rawTerm:     "iphone 16e",
			messageText: "Apple iPhone 16-e 128GB Branco por R$ 3.799",
			wantMatch:   true,
		},
		{
			name:        "iphone 16e rejects iphone 16 with conjunction e",
			rawTerm:     "iphone 16e",
			messageText: "Apple iPhone 16 128GB Preto e Branco por R$ 4.499",
			wantMatch:   false,
		},
		{
			name:        "iphone 16e rejects iphone 16 with compre e ganhe",
			rawTerm:     "iphone 16e",
			messageText: "Apple iPhone 16 128GB - compre e ganhe película",
			wantMatch:   false,
		},
		{
			name:        "galaxy s25 matches spaced s 25",
			rawTerm:     "galaxy s25",
			messageText: "Smartphone Samsung Galaxy S 25 Ultra 256GB por R$ 6.999",
			wantMatch:   true,
		},
		{
			name:        "ssd m.2 512gb matches M2 512 GB",
			rawTerm:     "ssd m.2 512gb",
			messageText: "SSD NVMe M2 512 GB Kingston PCIe 4.0 por R$ 259",
			wantMatch:   true,
		},
		{
			name:        "hardware skus preserved",
			rawTerm:     "ryzen 5 5500f",
			messageText: "Processador AMD Ryzen 5 5500F AM4 por R$ 549",
			wantMatch:   true,
		},
		{
			name:        "a520m motherboard preserved",
			rawTerm:     "a520m ddr5",
			messageText: "Placa Mãe A520M com suporte DDR5 em oferta",
			wantMatch:   true,
		},
		{
			name:        "i5 13450hx preserved without fuzzy on digits",
			rawTerm:     "i5 13450hx",
			messageText: "Notebook Lenovo LOQ i5 13450HX RTX 4050",
			wantMatch:   true,
		},
		{
			name:        "switch 2 matches Nintendo Switch 2",
			rawTerm:     "switch 2",
			messageText: "Console Nintendo Switch 2 Nacional por R$ 4.299",
			wantMatch:   true,
		},
		{
			name:        "switch 2 matches NS2 synonym",
			rawTerm:     "switch 2",
			messageText: "Console NS2 Bundle Mario Kart World R$ 4.499",
			wantMatch:   true,
		},
		{
			name:        "switch 2 rejects Switch OLED with 2 controles", //nolint:misspell // Portuguese fixture
			rawTerm:     "switch 2",
			messageText: "Console Nintendo Switch OLED Branco com 2 controles Joy-Con", //nolint:misspell // Portuguese fixture
			wantMatch:   false,
		},
		{
			name:        "switch 2 rejects Switch OLED without Nintendo word with 2 controles", //nolint:misspell // Portuguese fixture
			rawTerm:     "switch 2",
			messageText: "Switch OLED com 2 controles em promoção", //nolint:misspell // Portuguese fixture
			wantMatch:   false,
		},
		{
			name:        "switch 2 rejects Switch in 2x installment",
			rawTerm:     "switch 2",
			messageText: "Nintendo Switch OLED em até 2x sem juros de R$ 999",
			wantMatch:   false,
		},
		{
			name:        "ns2 synonym rejects Switch OLED with 2 controles", //nolint:misspell // Portuguese fixture
			rawTerm:     "ns2",
			messageText: "Nintendo Switch OLED Branco com 2 controles originais", //nolint:misspell // Portuguese fixture
			wantMatch:   false,
		},
		{
			name:        "iphone 16 pro rejects distant pro preposition",
			rawTerm:     "iphone 16 pro",
			messageText: "iPhone 16 128GB Preto em promoção com super desconto pro Dia dos Pais",
			wantMatch:   false,
		},
		{
			name:        "iphone 16 pro matches adjacent Pro",
			rawTerm:     "iphone 16 pro",
			messageText: "Apple iPhone 16 Pro Max 256GB Titânio Natural",
			wantMatch:   true,
		},
		{
			name:        "Damerau-Levenshtein handles adjacent transposition",
			rawTerm:     "nintnedo switch",
			messageText: "Console Nintendo Switch OLED por R$ 1.899",
			wantMatch:   true,
		},
		{
			name:        "digit guard blocks fuzzy match between GPU models",
			rawTerm:     "rtx4060",
			messageText: "Placa de Vídeo MSI RTX4070 Ventus 12GB",
			wantMatch:   false,
		},
		{
			name:        "digit guard blocks fuzzy match between Intel CPUs",
			rawTerm:     "i5 13450hx",
			messageText: "Notebook Acer Nitro i5 13400HX RTX 3050",
			wantMatch:   false,
		},
		{
			name:        "digit guard blocks fuzzy match between storage sizes",
			rawTerm:     "ssd 128gb",
			messageText: "SSD Kingston 120GB SATA III",
			wantMatch:   false,
		},
		{
			name:        "portuguese plural equivalence on controles", //nolint:misspell // Portuguese fixture
			rawTerm:     "controle ps5",
			messageText: "Kit 2 Controles Sem Fio PlayStation 5 DualSense", //nolint:misspell // Portuguese fixture
			wantMatch:   true,
		},
		{
			name:        "negative plural token excludes plural form",
			rawTerm:     "switch oled -capa",
			messageText: "Kit Capas Protetoras para Nintendo Switch OLED R$ 79",
			wantMatch:   false,
		},
		{
			name:        "stopwords in user query ignored cleanly",
			rawTerm:     "controle para ps5",
			messageText: "Controle Sony DualSense PS5 Branco R$ 349",
			wantMatch:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			interest, err := domain.NewUserInterest(1, tt.rawTerm, nil)
			if err != nil {
				t.Fatalf("NewUserInterest(%q) error: %v", tt.rawTerm, err)
			}
			_, got := domain.MatchProduct(interest, tt.messageText)
			if got != tt.wantMatch {
				t.Errorf("MatchProduct(%q, %q) = %v, want %v", tt.rawTerm, tt.messageText, got, tt.wantMatch)
			}
		})
	}
}

func TestExtractLowestPriceWithEntities_EmpiricalCorpus(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		text      string
		entities  []domain.TelegramEntityExcerpt
		wantPrice float64
		wantFound bool
	}{
		{
			name:      "ID=4586 unit price in parentheses",
			text:      "Stick Wafer Recheado Ouro Branco 15 unidades 25g\n\nR$ 40 (R$ 2,69 cada)\nhttps://link.amazon/B08COucfM",
			wantPrice: 40.0,
			wantFound: true,
		},
		{
			name:      "ID=3725 brush pack unit price",
			text:      "3 Unidades Escovas Dentais KESS Pack Pro Extra Macias\n\nR$ 18 (R$ 6 cada)",
			wantPrice: 18.0,
			wantFound: true,
		},
		{
			name:      "ID=3868 de por with unit price cada",
			text:      "Kit com 4 Unidades Secret Desodorante\n\nDe R$ 81 por R$ 60 (R$ 15 cada)",
			wantPrice: 60.0,
			wantFound: true,
		},
		{
			name:      "ID=2965 leading unit price cada",
			text:      "R$ 66 CADA KAIAK PRA ELAS COM O CUPOM DE 15%\n\nDe R$ 315 por R$ 132\nno pix no site!",
			wantPrice: 132.0,
			wantFound: true,
		},
		{
			name:      "ID=3779 leading unit price talher",
			text:      "R$ 0,99 CADA TALHER\n\nJogo Talheres Tramontina Ipanema 25 peças\n\nR$ 24\nhttps://meli.la/2EVs1Kd",
			wantPrice: 24.0,
			wantFound: true,
		},
		{
			name:      "ID=4227 leading unit price carbo gel",
			text:      "R$ 1,62 CADA CARBO GEL (menor preço)\n\nKit com 10 Sachês Athletica Nutrition Go!\n\nR$ 16",
			wantPrice: 16.0,
			wantFound: true,
		},
		{
			name:      "ID=2012 unit price por booster",
			text:      "R$ 9,43 por booster, tá valendo demais\n\nReinado Delta Box Display 36 Booster Pokémon Copag\n\nDe R$ 459 por R$ 339",
			wantPrice: 339.0,
			wantFound: true,
		},
		{
			name: "ID=4651 strike entity and historical delta",
			text: "Mousepad Fallen Candy Speed++, Grande\n\nDe R$299 por R$183 + frete\nou em até 6x de R$30 sem juros\n- R$2 do menor preço histórico",
			entities: []domain.TelegramEntityExcerpt{
				{Type: "strike", Offset: 42, Length: 5, Text: "R$299"},
			},
			wantPrice: 183.0,
			wantFound: true,
		},
		{
			name:      "ID=1835 coupon announcement only",
			text:      "🚨 Cupom Mercado Livre\n\n🏷 R$ 300 OFF em R$ 2699: OFERTAPRATEC\n🏷 22% OFF em R$ 1, Limite de R$ 500 OFF: OFERTAS22\n🏷 25% OFF em R$ 1, Limite de R$ 500 OFF: OFERTAS25",
			wantPrice: 0,
			wantFound: false,
		},
		{
			name:      "ID=2494 shopee coupon thresholds only",
			text:      "🔥 Cupom Shopee\n\n💸 R$ 15 OFF acima de R$ 99\n💸 R$ 15 OFF acima de R$ 89\n💸 R$ 80 OFF acima de R$ 599",
			wantPrice: 0,
			wantFound: false,
		},
		{
			name:      "ID=3049 ml coupon limits only",
			text:      "🚨 Cupons Mercado Livre\n\n🏷 10% OFF acima de R$89, limite R$40: LOJASOFICIAIS1311\n🏷 25% OFF, Limite de R$ 500 OFF: OFERTAS25",
			wantPrice: 0,
			wantFound: false,
		},
		{
			name:      "ID=4468 multiline pix and installment",
			text:      "Por apenas: R$4.391,10 😍\nno pix\nou em até 12x de R$ 406,62",
			wantPrice: 4391.10,
			wantFound: true,
		},
		{
			name:      "ID=481 product price vs coupon discount OFF",
			text:      "Celular Samsung Galaxy S25 FE 5G 128GB\n\nR$ 2.549 no Pix\nAplique o Cupom de R$ 150 OFF no Anúncio",
			wantPrice: 2549.0,
			wantFound: true,
		},
		{
			name:      "strike entity with leading emoji UTF-16 offset",
			text:      "🔥 R$ 120 R$ 299",
			entities:  []domain.TelegramEntityExcerpt{{Type: "strike", Offset: 3, Length: 6}},
			wantPrice: 299.0,
			wantFound: true,
		},
		{
			name:      "unit price fallback when no total price exists",
			text:      "Café Torrado em Grãos R$ 59,90/kg no Pix",
			wantPrice: 59.90,
			wantFound: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			gotPrice, gotFound := domain.ExtractLowestPriceWithEntities(tt.text, tt.entities)
			if gotFound != tt.wantFound {
				t.Fatalf("ExtractLowestPriceWithEntities() found = %v, want %v (price=%v)", gotFound, tt.wantFound, gotPrice)
			}
			if gotFound && gotPrice != tt.wantPrice {
				t.Errorf("ExtractLowestPriceWithEntities() price = %v, want %v", gotPrice, tt.wantPrice)
			}
		})
	}
}
