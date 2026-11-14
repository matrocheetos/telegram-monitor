package domain_test

import (
	"testing"

	"github.com/matrocheetos/telegram-monitor/internal/domain"
)

func TestHasCouponIndicator(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		text     string
		expected bool
	}{
		{name: "with cupom", text: "Use o cupom VALE10", expected: true},
		{name: "with cupons", text: "Lista de cupons do dia", expected: true},
		{name: "with emoji ticket", text: "🎟️ 10% OFF em todo o site", expected: true},
		{name: "with emoji tag", text: "🏷️ Desconto imperdível", expected: true},
		{name: "with codigo", text: "Insira o código PROMO15", expected: true},
		{name: "without indicator", text: "Smart TV 55 polegadas 4K em promoção", expected: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := domain.HasCouponIndicator(tt.text)
			if got != tt.expected {
				t.Errorf("HasCouponIndicator(%q) = %v, want %v", tt.text, got, tt.expected)
			}
		})
	}
}

func TestExtractCoupons(t *testing.T) {
	t.Parallel()

	t.Run("single coupon with real Amazon link", func(t *testing.T) {
		t.Parallel()
		msg := `🔥 OFERTA RELÂMPAGO AMAZON!
Echo Dot 5ª Geração com Alexa
🎟️ Cupom: ALEXA10
Apenas R$ 269,10 no Pix
Compre aqui: https://amzn.to/3example`

		coupons := domain.ExtractCoupons(msg)
		if len(coupons) != 1 {
			t.Fatalf("expected 1 coupon, got %d", len(coupons))
		}
		if coupons[0].Code != "ALEXA10" {
			t.Errorf("expected code ALEXA10, got %s", coupons[0].Code)
		}
		if coupons[0].StoreName != "Amazon" {
			t.Errorf("expected store Amazon, got %s", coupons[0].StoreName)
		}
		if coupons[0].StoreURL != "https://amzn.to/3example" {
			t.Errorf("expected url https://amzn.to/3example, got %s", coupons[0].StoreURL)
		}
	})

	t.Run("multiple coupons in single message", func(t *testing.T) {
		t.Parallel()
		msg := `🚨 SUPER CUPONAGEM MERCADO LIVRE!
Válido para produtos selecionados:
🎟️ 10% OFF: SALVAESSA
🎟️ 12% OFF: CARRINHOCHEIOML
Resgate no app: https://meli.la/2example`

		coupons := domain.ExtractCoupons(msg)
		if len(coupons) != 2 {
			t.Fatalf("expected 2 coupons, got %d", len(coupons))
		}
		if coupons[0].Code != "SALVAESSA" {
			t.Errorf("expected first code SALVAESSA, got %s", coupons[0].Code)
		}
		if coupons[1].Code != "CARRINHOCHEIOML" {
			t.Errorf("expected second code CARRINHOCHEIOML, got %s", coupons[1].Code)
		}
		if coupons[0].StoreName != "Mercado Livre" || coupons[1].StoreName != "Mercado Livre" {
			t.Errorf("expected store Mercado Livre, got %s and %s", coupons[0].StoreName, coupons[1].StoreName)
		}
		if coupons[0].StoreURL != "https://meli.la/2example" {
			t.Errorf("expected real store URL, got %s", coupons[0].StoreURL)
		}
	})

	t.Run("ignores colon before url and timestamps", func(t *testing.T) {
		t.Parallel()
		msg := `🏷️ Cupom do Magalu válido até às 14:30 de hoje!
Link da oferta: https://magazineluiza.com.br/produto/123
Use o cupom MAGALU20 no carrinho.`

		coupons := domain.ExtractCoupons(msg)
		if len(coupons) != 1 {
			t.Fatalf("expected 1 coupon, got %d (%v)", len(coupons), coupons)
		}
		if coupons[0].Code != "MAGALU20" {
			t.Errorf("expected code MAGALU20, got %s", coupons[0].Code)
		}
		if coupons[0].StoreName != "Magalu" {
			t.Errorf("expected store Magalu, got %s", coupons[0].StoreName)
		}
	})

	t.Run("store fallback url when no link present", func(t *testing.T) {
		t.Parallel()
		msg := `🎟️ Cupom exclusivo Shopee: SHOPEE50 para compras acima de R$ 200!`

		coupons := domain.ExtractCoupons(msg)
		if len(coupons) != 1 {
			t.Fatalf("expected 1 coupon, got %d", len(coupons))
		}
		if coupons[0].Code != "SHOPEE50" {
			t.Errorf("expected code SHOPEE50, got %s", coupons[0].Code)
		}
		if coupons[0].StoreName != "Shopee" {
			t.Errorf("expected store Shopee, got %s", coupons[0].StoreName)
		}
		if coupons[0].StoreURL != "https://shopee.com.br" {
			t.Errorf("expected fallback URL https://shopee.com.br, got %s", coupons[0].StoreURL)
		}
	})

	t.Run("non-coupon message returns empty slice", func(t *testing.T) {
		t.Parallel()
		msg := `Notebook Gamer Lenovo LOQ por apenas R$ 4.299 na KaBuM!`

		coupons := domain.ExtractCoupons(msg)
		if len(coupons) != 0 {
			t.Fatalf("expected 0 coupons, got %d", len(coupons))
		}
	})

	t.Run("rejects promo metadata labels, stop words, installments, voltages, and url ports", func(t *testing.T) {
		t.Parallel()
		msg := `🎟️ SUPER OFERTA AMAZON!
Validade: HOJE
Parcelas: 10X
Parcelamento: 12X
Desconto: PRIME
Cor: PRETO
Condição: NOVO
Voltagem: 110V
Voltagem: 220V
Vendido por: AMAZON
Frete: GRATIS
Link: https://amzn.to:8080/deal
🎟️ 10% OFF: SALVAESSA
🎟️ 12% OFF: CARRINHOCHEIOML
Cupom: ABC10 (Validade: AMANHA)
Código: XYZ20
Desconto: SAVE20`

		coupons := domain.ExtractCoupons(msg)
		expected := []string{"SALVAESSA", "CARRINHOCHEIOML", "ABC10", "XYZ20", "SAVE20"}
		if len(coupons) != len(expected) {
			t.Fatalf("expected %d coupons (%v), got %d (%v)", len(expected), expected, len(coupons), coupons)
		}
		for i, want := range expected {
			if coupons[i].Code != want {
				t.Errorf("coupon[%d] = %s, want %s", i, coupons[i].Code, want)
			}
		}
	})

	t.Run("rejects installment and voltage candidates in keyword pattern", func(t *testing.T) {
		t.Parallel()
		msg := `Use o cupom 10X ou código 220V ou cupom HOJE ou cupom PRIME`

		coupons := domain.ExtractCoupons(msg)
		if len(coupons) != 0 {
			t.Fatalf("expected 0 coupons, got %d (%v)", len(coupons), coupons)
		}
	})
}

func TestIdentifyStoreFromText(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		text      string
		wantStore string
		wantURL   string
	}{
		{
			name:      "specific store keyword with aggregator shortener URL preserves shortener link",
			text:      "🔥 Cupom Amazon! Use ALEXA10 em https://aoferta.net/xyz",
			wantStore: "Amazon",
			wantURL:   "https://aoferta.net/xyz",
		},
		{
			name:      "specific store URL takes precedence over aggregator URL",
			text:      "Oferta em https://aoferta.net/abc e https://amzn.to/def",
			wantStore: "Amazon",
			wantURL:   "https://amzn.to/def",
		},
		{
			name:      "aggregator URL used when no specific store matches",
			text:      "Super cupom imperdível em https://aoferta.net/xyz",
			wantStore: "A Oferta",
			wantURL:   "https://aoferta.net/xyz",
		},
		{
			name:      "aggregator keyword without URL uses aggregator fallback",
			text:      "Confira os cupons da A Oferta hoje",
			wantStore: "A Oferta",
			wantURL:   "https://aoferta.net",
		},
		{
			name:      "unknown store with URL keeps Oferta and URL",
			text:      "Promoção em https://unknown-shop.example/item",
			wantStore: "Oferta",
			wantURL:   "https://unknown-shop.example/item",
		},
		{
			name:      "unknown store without URL returns empty URL",
			text:      "Cupom exclusivo sem link definido",
			wantStore: "Oferta",
			wantURL:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			gotStore, gotURL := domain.IdentifyStoreFromText(tt.text)
			if gotStore != tt.wantStore || gotURL != tt.wantURL {
				t.Errorf("IdentifyStoreFromText(%q) = (%q, %q), want (%q, %q)",
					tt.text, gotStore, gotURL, tt.wantStore, tt.wantURL)
			}
		})
	}
}

func TestExtractCoupons_EmpiricalCorpus(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		text      string
		entities  []domain.TelegramEntityExcerpt
		wantCodes []string
		wantStore string
	}{
		{
			name:      "ID=70 multi coupon with plus and moedas",
			text:      "Novo Monitor ARZOPA 27\n💵 R$ 810 com cupom: M1RC35BR + AEMEGA15 + moedas no app no app no app\nhttps://a.aliexpress.com/_c37MYOll",
			wantCodes: []string{"M1RC35BR", "AEMEGA15"},
			wantStore: "AliExpress",
		},
		{
			name:      "ID=500 multi coupon plus and ou",
			text:      "#Aliexpress - Nvme 2TB\n🏷️ Use cupom: HB3001 + AEMEGA12 ou AEMEGA6 + Moedas no App\nhttps://a.aliexpress.com/_c2RtOLvh",
			wantCodes: []string{"HB3001", "AEMEGA12", "AEMEGA6"},
			wantStore: "AliExpress",
		},
		{
			name:      "ID=2491 multi coupon ou",
			text:      "🏷️ Use cupom: AEMEGA15 ou AEMEGA4\nhttps://a.aliexpress.com/_c3example",
			wantCodes: []string{"AEMEGA15", "AEMEGA4"},
			wantStore: "AliExpress",
		},
		{
			name:      "ID=2678 three coupons with ou",
			text:      "🏷 Cupom: IFPIU15T ou VIVAAE01 ou VAMOS03\nhttps://a.aliexpress.com/_c3example",
			wantCodes: []string{"IFPIU15T", "VIVAAE01", "VAMOS03"},
			wantStore: "AliExpress",
		},
		{
			name:      "ID=3017 ou plus moedas",
			text:      "🏷 Cupom: AFFNOV01 ou AEMEGA1 + Moedas no App\nhttps://a.aliexpress.com/_c3example",
			wantCodes: []string{"AFFNOV01", "AEMEGA1"},
			wantStore: "AliExpress",
		},
		{
			name:      "ID=3136 multi coupon ignores 63 moedas",
			text:      "🏷 Cupom: NETAC05 + AFFNOV03 ou AEMEGA3 + 63 Moedas no app\nhttps://a.aliexpress.com/_c3example",
			wantCodes: []string{"NETAC05", "AFFNOV03", "AEMEGA3"},
			wantStore: "AliExpress",
		},
		{
			name:      "ID=4139 multi coupon ignores Seleccione Pix",
			text:      "Use o Cupom: POUPEMELI ou CLIENTEVIP 🎟️ + Selecione Pix\nhttps://meli.la/2example",
			wantCodes: []string{"POUPEMELI", "CLIENTEVIP"},
			wantStore: "Mercado Livre",
		},
		{
			name:      "ID=4529 multi coupon with attached ticket emoji",
			text:      "De R$ 119 por R$ 29\nUse o Cupom: POUPEMELI ou MAISDESCONTO🎟️\nhttps://meli.la/264goLY",
			wantCodes: []string{"POUPEMELI", "MAISDESCONTO"},
			wantStore: "Mercado Livre",
		},
		{
			name:      "ID=356 rejects lowercase abaixo",
			text:      "🎟 Ative o cupom abaixo do preço!\nhttps://meli.la/2example",
			wantCodes: []string{},
		},
		{
			name:      "ID=433 rejects lowercase der and identifies Netshoes",
			text:      "FILA BÁSICO PRA CORRIDA\nUse o Cupom: BLACK11 🎟️\n\nLoja Oficial Netshoes\nhttps://promo.xetlinks.com/dVJGW4J3\n\n- se o cupom der erro, acesse pelo link",
			wantCodes: []string{"BLACK11"},
			wantStore: "Netshoes",
		},
		{
			name:      "ID=1699 rejects Title Case Mercado",
			text:      "Cupom Mercado Livre Tecnologia - R$ 300 OFF em R$ 2699\n\n-Cupom: OFERTAPRATEC",
			wantCodes: []string{"OFERTAPRATEC"},
			wantStore: "Mercado Livre",
		},
		{
			name:      "ID=1807 rejects uppercase adjective FORTE",
			text:      "Olha o cupom FORTE do Meli, aproveitem!",
			wantCodes: []string{},
		},
		{
			name:      "ID=1881 rejects Dragon Quest Title Case Echoes",
			text:      "🔥[Mídia Física] Dragon Quest XI S: Echoes of an Elusive Age NS2 e use cupom LIVROSJOGOS1311\nhttps://meli.la/2example",
			wantCodes: []string{"LIVROSJOGOS1311"},
			wantStore: "Mercado Livre",
		},
		{
			name: "MessageEntityCode extracts lowercase monospaced coupon when indicator present",
			text: "🔥 Cupom exclusivo: aplique promo10 no site",
			entities: []domain.TelegramEntityExcerpt{
				{Type: "code", Offset: 28, Length: 7},
			},
			wantCodes: []string{"PROMO10"},
		},
		{
			name: "MessageEntityCode ignores stopwords",
			text: "🎟️ Oferta no mercado hoje",
			entities: []domain.TelegramEntityExcerpt{
				{Type: "code", Text: "mercado"},
			},
			wantCodes: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := domain.ExtractCouponsWithEntities(tt.text, tt.entities)
			if len(got) != len(tt.wantCodes) {
				t.Fatalf("ExtractCouponsWithEntities() got %d coupons (%v), want %d (%v)",
					len(got), got, len(tt.wantCodes), tt.wantCodes)
			}
			for i, wantCode := range tt.wantCodes {
				if got[i].Code != wantCode {
					t.Errorf("coupon[%d].Code = %q, want %q", i, got[i].Code, wantCode)
				}
				if tt.wantStore != "" && got[i].StoreName != tt.wantStore {
					t.Errorf("coupon[%d].StoreName = %q, want %q", i, got[i].StoreName, tt.wantStore)
				}
			}
		})
	}
}

func TestIdentifyStoreFromText_SlugsAndNewDomains(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		text      string
		wantStore string
		wantURL   string
	}{
		{
			name:      "aoferta slug Kabum",
			text:      "SSD 1TB NVMe em promoção https://aoferta.net/003wMFHz-Kabum",
			wantStore: "Kabum",
			wantURL:   "https://aoferta.net/003wMFHz-Kabum",
		},
		{
			name:      "aoferta slug Terabyte",
			text:      "Fonte 650W em promoção https://aoferta.net/0033iDdD-Terabyte",
			wantStore: "Terabyte",
			wantURL:   "https://aoferta.net/0033iDdD-Terabyte",
		},
		{
			name:      "aoferta slug MercadoLivre",
			text:      "Oferta relâmpago https://aoferta.net/abc123-MercadoLivre",
			wantStore: "Mercado Livre",
			wantURL:   "https://aoferta.net/abc123-MercadoLivre",
		},
		{
			name:      "link.amazon shortener",
			text:      "Celular S25 FE https://link.amazon/B044hqQnP",
			wantStore: "Amazon",
			wantURL:   "https://link.amazon/B044hqQnP",
		},
		{
			name:      "a.aliexpress.com shortener",
			text:      "Monitor Arzopa https://a.aliexpress.com/_c37MYOll",
			wantStore: "AliExpress",
			wantURL:   "https://a.aliexpress.com/_c37MYOll",
		},
		{
			name:      "Centauro keyword",
			text:      "Cupom Centauro Tênis Running https://promo.xetlinks.com/xyz",
			wantStore: "Centauro",
			wantURL:   "https://promo.xetlinks.com/xyz",
		},
		{
			name:      "Dafiti domain",
			text:      "Moda em oferta https://www.dafiti.com.br/look",
			wantStore: "Dafiti",
			wantURL:   "https://www.dafiti.com.br/look",
		},
		{
			name:      "Nike domain",
			text:      "Tênis Air Max https://www.nike.com.br/air-max",
			wantStore: "Nike",
			wantURL:   "https://www.nike.com.br/air-max",
		},
		{
			name:      "Carrefour keyword",
			text:      "Oferta Carrefour Smart TV https://promo.xetlinks.com/tv",
			wantStore: "Carrefour",
			wantURL:   "https://promo.xetlinks.com/tv",
		},
		{
			name:      "keyword boundary prevents false meli inside melissa",
			text:      "Sandália Melissa em oferta https://lojanova.example/melissa",
			wantStore: "Oferta",
			wantURL:   "https://lojanova.example/melissa",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			gotStore, gotURL := domain.IdentifyStoreFromText(tt.text)
			if gotStore != tt.wantStore || gotURL != tt.wantURL {
				t.Errorf("IdentifyStoreFromText(%q) = (%q, %q), want (%q, %q)",
					tt.text, gotStore, gotURL, tt.wantStore, tt.wantURL)
			}
		})
	}
}

func BenchmarkExtractCoupons(b *testing.B) {
	msg := "🏷️ Use cupom: HB3001 + AEMEGA12 ou AEMEGA6 + Moedas no App\nhttps://a.aliexpress.com/_c2RtOLvh"
	entities := []domain.TelegramEntityExcerpt{
		{Type: "code", Text: "HB3001"},
		{Type: "code", Text: "AEMEGA12"},
		{Type: "code", Text: "AEMEGA6"},
	}

	b.ReportAllocs()
	for b.Loop() {
		_ = domain.ExtractCouponsWithEntities(msg, entities)
	}
}
