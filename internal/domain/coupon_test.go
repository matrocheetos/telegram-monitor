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
}
