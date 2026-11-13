package domain

import (
	"regexp"
	"strings"
	"time"
)

var (
	urlRegex             = regexp.MustCompile(`https?://[^\s<>"'\)]+`)
	colonPatternRegex    = regexp.MustCompile(`:\s*([A-Za-z0-9_-]{3,30})`)
	keywordPatternRegex  = regexp.MustCompile(`(?i)(?:cupom|c[oó]digo)\s+(?:[eé]\s+)?([A-Za-z0-9_-]{3,30})(?:[ \t\r\n]|$)`)
	timestampCheckRegex  = regexp.MustCompile(`^[0-9]{2}$`)
	installmentCodeRegex = regexp.MustCompile(`^[0-9]{1,2}X$`)
	voltageCodeRegex     = regexp.MustCompile(`^[0-9]{2,3}V$`)
	couponStopWords      = map[string]struct{}{
		"HTTP": {}, "HTTPS": {}, "COM": {}, "BR": {}, "LINK": {}, "LOJA": {},
		"VEJA": {}, "CONFIRA": {}, "CLIQUE": {}, "AQUI": {}, "VALOR": {}, "PRECO": {},
		"FRETE": {}, "GRATIS": {}, "PARCELADO": {}, "AVISTA": {}, "DESCONTO": {},
		"DE": {}, "DO": {}, "DA": {}, "NO": {}, "NA": {}, "EM": {}, "POR": {},
		"PARA": {}, "VALIDO": {}, "EXCLUSIVO": {}, "ATIVO": {}, "APLICADO": {}, "REAIS": {},
		"KABUM": {}, "AMAZON": {}, "SHOPEE": {}, "MAGALU": {}, "ALIEXPRESS": {},
		"PICHAU": {}, "TERABYTE": {}, "MERCADOLIVRE": {}, "MELI": {}, "SITE": {}, "APP": {},
		"HOJE": {}, "AMANHA": {}, "ONTEM": {}, "PRIME": {}, "PIX": {}, "BOLETO": {},
		"CARTAO": {}, "PARCELA": {}, "PARCELAS": {}, "VALIDADE": {}, "CONDICAO": {},
		"PRETO": {}, "BRANCO": {}, "AZUL": {}, "VERMELHO": {}, "VERDE": {}, "CINZA": {},
		"ROSA": {}, "DOURADO": {}, "PRATA": {}, "NOVO": {}, "USADO": {}, "LACRADO": {},
		"ORIGINAL": {}, "OFICIAL": {}, "NACIONAL": {}, "INTERNACIONAL": {}, "TODOS": {},
		"TODAS": {}, "SELECIONADOS": {}, "UNIDADES": {}, "ESTOQUE": {}, "GARANTIA": {},
		"BIVOLT": {}, "CUPOM": {}, "CUPONS": {}, "CODIGO": {}, "OFF": {},
	}
	couponLineCues = map[string]struct{}{
		"cupom": {}, "cupons": {}, "codigo": {}, "cod": {}, "off": {},
		"desconto": {}, "use": {}, "aplique": {}, "vale": {}, "voucher": {},
	}
	nonCouponMetadataLabels = map[string]struct{}{
		"validade": {}, "parcela": {}, "parcelas": {}, "cor": {}, "tamanho": {},
		"modelo": {}, "voltagem": {}, "condicao": {}, "vendido": {}, "entregue": {},
		"loja": {}, "site": {}, "link": {}, "compre": {}, "acesse": {}, "frete": {},
		"envio": {}, "pagamento": {}, "forma": {}, "valor": {}, "preco": {},
		"de": {}, "por": {}, "ate": {}, "obs": {}, "nota": {}, "aviso": {},
		"regras": {}, "status": {}, "estoque": {}, "garantia": {}, "marca": {},
		"categoria": {},
	}
)

type storeDefinition struct {
	name        string
	fallbackURL string
	domains     []string
	keywords    []string
}

var specificStores = []storeDefinition{
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
}

var aggregatorStores = []storeDefinition{
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

func ExtractFirstURL(text string) string {
	match := urlRegex.FindString(text)
	if match == "" {
		return ""
	}
	return strings.Trim(match, ".,;:!?')\"")
}

func ExtractAllURLs(text string) []string {
	matches := urlRegex.FindAllString(text, -1)
	if len(matches) == 0 {
		return []string{}
	}

	urls := make([]string, 0, len(matches))
	for _, m := range matches {
		cleaned := strings.Trim(m, ".,;:!?')\"")
		if cleaned != "" {
			urls = append(urls, cleaned)
		}
	}
	return urls
}

func IdentifyStoreFromText(text string) (string, string) {
	urls := ExtractAllURLs(text)
	return identifyStore(text, urls)
}

func matchStoreByURL(stores []storeDefinition, foundURLs []string) (string, string, bool) {
	for _, u := range foundURLs {
		if u == "" {
			continue
		}
		lowerURL := strings.ToLower(u)
		for _, store := range stores {
			for _, domain := range store.domains {
				if strings.Contains(lowerURL, domain) {
					return store.name, u, true
				}
			}
		}
	}
	return "", "", false
}

func matchStoreByKeyword(stores []storeDefinition, lowerText string, foundURLs []string) (string, string, bool) {
	for _, store := range stores {
		for _, kw := range store.keywords {
			if strings.Contains(lowerText, kw) {
				if len(foundURLs) > 0 && foundURLs[0] != "" {
					return store.name, foundURLs[0], true
				}
				return store.name, store.fallbackURL, true
			}
		}
	}
	return "", "", false
}

func identifyStore(text string, foundURLs []string) (string, string) {
	if name, url, ok := matchStoreByURL(specificStores, foundURLs); ok {
		return name, url
	}

	lowerText := strings.ToLower(text)
	if name, url, ok := matchStoreByKeyword(specificStores, lowerText, foundURLs); ok {
		return name, url
	}

	if name, url, ok := matchStoreByURL(aggregatorStores, foundURLs); ok {
		return name, url
	}

	if name, url, ok := matchStoreByKeyword(aggregatorStores, lowerText, foundURLs); ok {
		return name, url
	}

	if len(foundURLs) > 0 && foundURLs[0] != "" {
		return "Oferta", foundURLs[0]
	}
	return "Oferta", ""
}

func isValidColonContext(text string, colonIdx int) bool {
	lineStart := strings.LastIndexAny(text[:colonIdx], "\r\n") + 1
	linePrefix := text[lineStart:colonIdx]

	// Ignore port or path colons inside URLs (e.g. https://example.com:8080).
	tokenStart := strings.LastIndexAny(linePrefix, " \t") + 1
	tokenLower := strings.ToLower(linePrefix[tokenStart:])
	if strings.HasPrefix(tokenLower, "http://") || strings.HasPrefix(tokenLower, "https://") {
		return false
	}

	tokens := Tokenize(linePrefix)
	if len(tokens) > 0 {
		lastToken := tokens[len(tokens)-1]
		if _, isMetadata := nonCouponMetadataLabels[lastToken]; isMetadata {
			return false
		}
	}

	if strings.Contains(linePrefix, "🎟") || strings.Contains(linePrefix, "🏷") {
		return true
	}

	for _, tok := range tokens {
		if _, isCue := couponLineCues[tok]; isCue {
			return true
		}
	}

	return false
}

func isValidCouponCandidate(upper string) bool {
	if len(upper) < 3 || len(upper) > 30 {
		return false
	}
	if timestampCheckRegex.MatchString(upper) || installmentCodeRegex.MatchString(upper) || voltageCodeRegex.MatchString(upper) {
		return false
	}
	if _, isStopWord := couponStopWords[upper]; isStopWord {
		return false
	}
	if strings.HasPrefix(upper, "HTTP") || strings.HasPrefix(upper, "//") {
		return false
	}
	return true
}

func ExtractCoupons(messageText string) []Coupon {
	if !HasCouponIndicator(messageText) {
		return []Coupon{}
	}

	foundURLs := ExtractAllURLs(messageText)
	storeName, storeURL := identifyStore(messageText, foundURLs)

	extractedSet := make(map[string]struct{})
	extractedCodes := make([]string, 0, 4)

	addCode := func(raw string) {
		code := strings.Trim(raw, " \t\r\n.,;:!?'\"")
		upper := strings.ToUpper(code)
		if !isValidCouponCandidate(upper) {
			return
		}
		if _, exists := extractedSet[upper]; !exists {
			extractedSet[upper] = struct{}{}
			extractedCodes = append(extractedCodes, upper)
		}
	}

	colonMatches := colonPatternRegex.FindAllStringSubmatchIndex(messageText, -1)
	for _, match := range colonMatches {
		if len(match) >= 4 && isValidColonContext(messageText, match[0]) {
			addCode(messageText[match[2]:match[3]])
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
