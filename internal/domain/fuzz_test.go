package domain_test

import (
	"math"
	"testing"
	"unicode/utf8"

	"github.com/matrocheetos/telegram-monitor/internal/domain"
)

func FuzzExtractCoupons(f *testing.F) {
	seeds := []string{
		"",
		"Cupom: ALEXA10 na Amazon https://amzn.to/3example",
		"🎟️ 10% OFF: SALVAESSA\n🎟️ 12% OFF: CARRINHOCHEIOML\nhttps://meli.la/2example",
		"🎟️ Validade: HOJE\nParcelas: 10X\nDesconto: PRIME\nCor: PRETO\nCondição: NOVO\nVendido por: AMAZON\nFrete: GRATIS\nVoltagem: 220V",
		"Cupom aqui: https://aoferta.net:8080/deal",
		"Use o cupom MAGALU20 até às 14:30",
		"Notebook Gamer sem indicador de desconto R$ 4.299,00",
		"\xff\xfe🎟️ Cupom: \xffABC10",
	}

	for _, seed := range seeds {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, input string) {
		coupons := domain.ExtractCoupons(input)
		seen := make(map[string]struct{}, len(coupons))

		for _, c := range coupons {
			if c.Code == "" {
				t.Fatalf("ExtractCoupons(%q) produced empty code", input)
			}
			if len(c.Code) < 3 || len(c.Code) > 30 {
				t.Fatalf("ExtractCoupons(%q) produced out-of-range code %q", input, c.Code)
			}
			if !utf8.ValidString(c.Code) {
				t.Fatalf("ExtractCoupons(%q) produced non-UTF-8 code %q", input, c.Code)
			}
			if c.StoreName == "" || !utf8.ValidString(c.StoreName) {
				t.Fatalf("ExtractCoupons(%q) produced invalid StoreName %q", input, c.StoreName)
			}
			if _, dup := seen[c.Code]; dup {
				t.Fatalf("ExtractCoupons(%q) produced duplicate code %q", input, c.Code)
			}
			seen[c.Code] = struct{}{}
		}
	})
}

func FuzzMatchProduct(f *testing.F) {
	f.Add("switch oled branco -capa", "🔥 Console Nintendo Switch OLED Branco por R$ 1.899,00 ou 12x de R$ 175,00 sem juros", true, 2200.0)
	f.Add("rtx 4060", "Placa RTX4060 por R$ 1.500 + Frete R$ 25,00", false, 0.0)
	f.Add("notebook", "Parcelado em 12x de R$ 299,90 sem juros", true, 500.0)
	f.Add("tv 4k", "Smart TV R$ 1.000.000 ou R$ 1,299.00 com cashback de R$ 50,00", false, 0.0)
	f.Add("", "qualquer texto R$ 99,90", false, 0.0)
	f.Add("-capa -pelicula", "Capa de Switch R$ 49,90", false, 0.0)
	f.Add("switch", "Switch \xff\xfe R$ 149.90", true, 200.0)

	f.Fuzz(func(t *testing.T, term, message string, useMaxPrice bool, maxPrice float64) {
		for _, p := range domain.ExtractAllPrices(message) {
			if p <= 0 || math.IsNaN(p) || math.IsInf(p, 0) {
				t.Fatalf("ExtractAllPrices(%q) produced non-positive/invalid price %v", message, p)
			}
		}

		if lowest, found := domain.ExtractLowestPrice(message); found {
			if lowest <= 0 || math.IsNaN(lowest) || math.IsInf(lowest, 0) {
				t.Fatalf("ExtractLowestPrice(%q) produced non-positive/invalid price %v", message, lowest)
			}
		}

		var maxPtr *float64
		if useMaxPrice && maxPrice > 0 && !math.IsNaN(maxPrice) && !math.IsInf(maxPrice, 0) {
			v := maxPrice
			maxPtr = &v
		}

		interest, err := domain.NewUserInterest(1, term, maxPtr)
		if err != nil {
			return
		}

		match, ok := domain.MatchProduct(interest, message)
		if !ok {
			if match != nil {
				t.Fatalf("MatchProduct returned non-nil match with ok=false")
			}
			return
		}

		if match == nil {
			t.Fatalf("MatchProduct returned nil match with ok=true")
		}
		if match.InterestID != interest.ID || match.UserID != interest.UserID {
			t.Fatalf("MatchProduct returned mismatched IDs: %+v", match)
		}
		if match.ExtractedPrice != nil {
			p := *match.ExtractedPrice
			if p <= 0 || math.IsNaN(p) || math.IsInf(p, 0) {
				t.Fatalf("MatchProduct produced invalid ExtractedPrice %v", p)
			}
		}
		if interest.MaxPrice != nil {
			if match.ExtractedPrice == nil || *match.ExtractedPrice > *interest.MaxPrice {
				t.Fatalf("MatchProduct violated MaxPrice constraint: got %v, max %v", match.ExtractedPrice, *interest.MaxPrice)
			}
		}
	})
}
