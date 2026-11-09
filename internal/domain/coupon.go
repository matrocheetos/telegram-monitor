package domain

import (
	"regexp"
	"strings"
	"time"
)

var (
	urlRegex               = regexp.MustCompile(`https?://[^\s<>"'\)]+`)
	colonPatternRegex      = regexp.MustCompile(`(?i)(?:🎟️?|🏷️?|cupom|c[oó]digo)?\s*:\s*([A-Za-z0-9_-]{3,30})`)
	keywordPatternRegex    = regexp.MustCompile(`(?i)(?:cupom|c[oó]digo)\s+(?:[eé]\s+)?([A-Za-z0-9_-]{3,30})`)
	timestampCheckRegex    = regexp.MustCompile(`^[0-9]{2}$`)
	couponStopWords        = map[string]struct{}{
		"HTTP": {}, "HTTPS": {}, "COM": {}, "BR": {}, "LINK": {}, "LOJA": {},
		"VEJA": {}, "CONFIRA": {}, "CLIQUE": {}, "AQUI": {}, "VALOR": {}, "PRECO": {},
		"FRETE": {}, "GRATIS": {}, "PARCELADO": {}, "AVISTA": {}, "DESCONTO": {},
		"DE": {}, "DO": {}, "DA": {}, "NO": {}, "NA": {}, "EM": {}, "POR": {},
		"PARA": {}, "VALIDO": {}, "EXCLUSIVO": {}, "ATIVO": {}, "APLICADO": {}, "REAIS": {},
	}
)

type storeDefinition struct {
	name        string
	fallbackURL string
	domains     []string
	keywords    []string
}

var supportedStores = []storeDefinition{
	{
		name:        "Amazon",
		fallbackURL: "https://www.amazon.com.br",
		domains:     []string{"amzn.to", "amazon.com.br", "amazon.com"},
		keywords:    []string{"amazon"},
	},
	{
		name:        "Shopee",
		fallbackURL: "https://shopee.com.br",
		domains:     []string{"s.shopee.com.br", "shopee.com.br", "shopee.com"},
		keywords:    []string{"shopee"},
	},
	{
		name:        "Mercado Livre",
		fallbackURL: "https://www.mercadolivre.com.br",
		domains:     []string{"mercadolivre.com.br", "mercadolivre.com", "meli.la"},
		keywords:    []string{"mercado livre", "mercadolivre", "meli"},
	},
	{
		name:        "Magalu",
		fallbackURL: "https://www.magazineluiza.com.br",
		domains:     []string{"magazineluiza.com.br", "mglu.io", "magalu.com"},
		keywords:    []string{"magalu", "magazine luiza", "magazineluiza"},
	},
	{
		name:        "AliExpress",
		fallbackURL: "https://pt.aliexpress.com",
		domains:     []string{"aliexpress.com", "s.click.aliexpress.com"},
		keywords:    []string{"aliexpress", "ali express"},
	},
	{
		name:        "Kabum",
		fallbackURL: "https://www.kabum.com.br",
		domains:     []string{"kabum.com.br"},
		keywords:    []string{"kabum"},
	},
	{
		name:        "Terabyte",
		fallbackURL: "https://www.terabyteshop.com.br",
		domains:     []string{"terabyteshop.com.br"},
		keywords:    []string{"terabyte", "terabyteshop"},
	},
	{
		name:        "Pichau",
		fallbackURL: "https://www.pichau.com.br",
		domains:     []string{"pichau.com.br"},
		keywords:    []string{"pichau"},
	},
	{
		name:        "A Oferta",
		fallbackURL: "https://aoferta.net",
		domains:     []string{"aoferta.net"},
		keywords:    []string{"aoferta", "a oferta"},
	},
}

type Coupon struct {
	Code      string    `json:"code" db:"code"`
	StoreName string    `json:"store_name" db:"store_name"`
	StoreURL  string    `json:"store_url" db:"store_url"`
	CreatedAt time.Time `json:"created_at" db:"created_at"`
}

func HasCouponIndicator(text string) bool {
	lower := strings.ToLower(text)
	indicators := []string{"cupom", "cupons", "🎟", "🏷", "código", "codigo"}
	for _, ind := range indicators {
		if strings.Contains(lower, ind) {
			return true
		}
	}
	return false
}

func identifyStore(text string, foundURLs []string) (string, string) {
	for _, u := range foundURLs {
		lowerURL := strings.ToLower(u)
		for _, store := range supportedStores {
			for _, domain := range store.domains {
				if strings.Contains(lowerURL, domain) {
					return store.name, u
				}
			}
		}
	}

	lowerText := strings.ToLower(text)
	for _, store := range supportedStores {
		for _, kw := range store.keywords {
			if strings.Contains(lowerText, kw) {
				if len(foundURLs) > 0 {
					return store.name, foundURLs[0]
				}
				return store.name, store.fallbackURL
			}
		}
	}

	if len(foundURLs) > 0 {
		return "Oferta", foundURLs[0]
	}
	return "Oferta", "https://t.me"
}

func ExtractCoupons(messageText string) []Coupon {
	if !HasCouponIndicator(messageText) {
		return []Coupon{}
	}

	foundURLs := urlRegex.FindAllString(messageText, -1)
	storeName, storeURL := identifyStore(messageText, foundURLs)

	extractedSet := make(map[string]struct{})
	extractedCodes := make([]string, 0, 4)

	addCode := func(raw string) {
		code := strings.Trim(raw, " \t\r\n.,;:!?'\"")
		upper := strings.ToUpper(code)

		if len(upper) < 3 || len(upper) > 30 {
			return
		}
		if timestampCheckRegex.MatchString(upper) {
			return
		}
		if _, isStopWord := couponStopWords[upper]; isStopWord {
			return
		}
		if strings.HasPrefix(upper, "HTTP") || strings.HasPrefix(upper, "//") {
			return
		}
		if _, exists := extractedSet[upper]; !exists {
			extractedSet[upper] = struct{}{}
			extractedCodes = append(extractedCodes, upper)
		}
	}

	colonMatches := colonPatternRegex.FindAllStringSubmatch(messageText, -1)
	for _, match := range colonMatches {
		if len(match) > 1 {
			addCode(match[1])
		}
	}

	keywordMatches := keywordPatternRegex.FindAllStringSubmatch(messageText, -1)
	for _, match := range keywordMatches {
		if len(match) > 1 {
			addCode(match[1])
		}
	}

	now := time.Now().UTC()
	coupons := make([]Coupon, 0, len(extractedCodes))
	for _, code := range extractedCodes {
		coupons = append(coupons, Coupon{
			Code:      code,
			StoreName: storeName,
			StoreURL:  storeURL,
			CreatedAt: now,
		})
	}

	return coupons
}
