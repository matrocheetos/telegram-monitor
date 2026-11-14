package domain

import (
	"regexp"
	"strings"
	"time"
)

var (
	urlRegex             = regexp.MustCompile(`https?://[^\s<>"'\)]+`)
	keywordCueRegex      = regexp.MustCompile(`(?i)\b(?:cupom|cupons|c[oó]digo)\b(?:\s+(?:o|a|[eé]))?`)
	installmentCodeRegex = regexp.MustCompile(`^[0-9]{1,2}X$`)
	voltageCodeRegex     = regexp.MustCompile(`^[0-9]{2,3}V$`)

	couponStopWords = map[string]struct{}{
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
		"BIVOLT": {}, "CUPOM": {}, "CUPONS": {}, "CODIGO": {}, "CODIGOS": {}, "OFF": {},
		"FORTE": {}, "MERCADO": {}, "LIVRE": {}, "ABAIXO": {}, "ACIMA": {}, "ERRO": {},
		"AGORA": {}, "MEGA": {}, "SUPER": {}, "VIP": {}, "MOEDAS": {}, "MOEDA": {},
		"SELECIONE": {}, "CARRINHO": {}, "APLICAR": {}, "APLIQUE": {}, "RESGATE": {},
		"GANHE": {}, "USE": {}, "TECNOLOGIA": {}, "ANUNCIO": {}, "PAGINA": {},
		"OFERTA": {}, "OFERTAS": {}, "APROVEITE": {}, "APROVEITEM": {}, "GARANTA": {},
		"FINALIZE": {}, "PRODUTO": {}, "PRODUTOS": {},
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

	aggregatorStoreSlugs = map[string]string{
		"kabum":         "Kabum",
		"terabyte":      "Terabyte",
		"terabyteshop":  "Terabyte",
		"amazon":        "Amazon",
		"magalu":        "Magalu",
		"magazineluiza": "Magalu",
		"mercadolivre":  "Mercado Livre",
		"meli":          "Mercado Livre",
		"shopee":        "Shopee",
		"aliexpress":    "AliExpress",
		"pichau":        "Pichau",
		"netshoes":      "Netshoes",
		"centauro":      "Centauro",
		"dafiti":        "Dafiti",
		"nike":          "Nike",
		"carrefour":     "Carrefour",
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
		domains:     []string{"amzn.to", "link.amazon", "amazon.com.br", "amazon.com"},
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
		domains:     []string{"a.aliexpress.com", "s.click.aliexpress.com", "aliexpress.com"},
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
		name:        "Netshoes",
		fallbackURL: "https://www.netshoes.com.br",
		domains:     []string{"netshoes.com.br"},
		keywords:    []string{"netshoes"},
	},
	{
		name:        "Centauro",
		fallbackURL: "https://www.centauro.com.br",
		domains:     []string{"centauro.com.br"},
		keywords:    []string{"centauro"},
	},
	{
		name:        "Dafiti",
		fallbackURL: "https://www.dafiti.com.br",
		domains:     []string{"dafiti.com.br"},
		keywords:    []string{"dafiti"},
	},
	{
		name:        "Nike",
		fallbackURL: "https://www.nike.com.br",
		domains:     []string{"nike.com.br"},
		keywords:    []string{"nike"},
	},
	{
		name:        "Carrefour",
		fallbackURL: "https://www.carrefour.com.br",
		domains:     []string{"carrefour.com.br"},
		keywords:    []string{"carrefour"},
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

func matchStoreByAggregatorSlug(foundURLs []string) (string, string, bool) {
	for _, u := range foundURLs {
		if u == "" {
			continue
		}
		lowerURL := strings.ToLower(u)
		idx := strings.Index(lowerURL, "aoferta.net/")
		if idx < 0 {
			continue
		}
		pathPart := lowerURL[idx+len("aoferta.net/"):]
		if qIdx := strings.IndexAny(pathPart, "?#"); qIdx >= 0 {
			pathPart = pathPart[:qIdx]
		}
		pathPart = strings.TrimRight(pathPart, "/")
		dashIdx := strings.LastIndexByte(pathPart, '-')
		if dashIdx < 0 || dashIdx+1 >= len(pathPart) {
			continue
		}
		slug := pathPart[dashIdx+1:]
		if storeName, ok := aggregatorStoreSlugs[slug]; ok {
			return storeName, u, true
		}
	}
	return "", "", false
}

func isKeywordInText(lowerText, kw string) bool {
	start := 0
	for {
		idx := strings.Index(lowerText[start:], kw)
		if idx < 0 {
			return false
		}
		pos := start + idx
		end := pos + len(kw)
		leftOK := pos == 0 || !isASCIIAlphaNum(lowerText[pos-1])
		rightOK := end == len(lowerText) || !isASCIIAlphaNum(lowerText[end])
		if leftOK && rightOK {
			return true
		}
		start = pos + 1
	}
}

func matchStoreByKeyword(stores []storeDefinition, lowerText string, foundURLs []string) (string, string, bool) {
	for _, store := range stores {
		for _, kw := range store.keywords {
			if isKeywordInText(lowerText, kw) {
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

	if name, url, ok := matchStoreByAggregatorSlug(foundURLs); ok {
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

func isValidColonContext(line string, colonIdx int) bool {
	linePrefix := line[:colonIdx]

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

func isCouponTokenRune(r rune) bool {
	return (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || r == '-'
}

func cleanCouponToken(raw string) string {
	return strings.TrimFunc(raw, func(r rune) bool {
		return !isCouponTokenRune(r)
	})
}

func normalizeLikelyCouponToken(raw string, fromCodeEntity bool) (string, bool) {
	cleaned := cleanCouponToken(raw)
	if len(cleaned) < 4 || len(cleaned) > 30 {
		return "", false
	}

	hasLetter := false
	hasDigitChar := false
	hasUpper := false
	hasLower := false

	for i := 0; i < len(cleaned); i++ {
		b := cleaned[i]
		switch {
		case b >= 'A' && b <= 'Z':
			hasLetter = true
			hasUpper = true
		case b >= 'a' && b <= 'z':
			hasLetter = true
			hasLower = true
		case b >= '0' && b <= '9':
			hasDigitChar = true
		case b == '_' || b == '-':
		default:
			return "", false
		}
	}

	if !hasLetter {
		return "", false
	}

	upper := strings.ToUpper(cleaned)
	if strings.HasPrefix(upper, "HTTP") || strings.HasPrefix(upper, "WWW") {
		return "", false
	}
	if installmentCodeRegex.MatchString(upper) || voltageCodeRegex.MatchString(upper) {
		return "", false
	}
	if _, isStopWord := couponStopWords[upper]; isStopWord {
		return "", false
	}

	if !fromCodeEntity {
		allUpper := !hasLower && hasUpper
		mixedWithDigit := hasDigitChar && hasUpper
		if !allUpper && !mixedWithDigit {
			return "", false
		}
	}

	return upper, true
}

func scanCouponChain(tail string) []string {
	var results []string
	n := len(tail)
	i := 0
	expectCode := true

	for i < n {
		for i < n && (tail[i] == ' ' || tail[i] == '\t') {
			i++
		}
		if i >= n {
			break
		}

		if tail[i] == '(' || tail[i] == '[' || tail[i] == ';' || tail[i] == '|' {
			break
		}

		if tail[i] == '+' || tail[i] == '/' || tail[i] == ',' || tail[i] == '&' {
			if expectCode && len(results) > 0 {
				break
			}
			expectCode = true
			i++
			continue
		}

		start := i
		for i < n && tail[i] != ' ' && tail[i] != '\t' &&
			tail[i] != '+' && tail[i] != '/' && tail[i] != ',' && tail[i] != '&' {
			i++
		}
		segment := tail[start:i]
		lowerSeg := strings.ToLower(segment)
		if strings.HasPrefix(lowerSeg, "http://") || strings.HasPrefix(lowerSeg, "https://") {
			break
		}

		if lowerSeg == "ou" || lowerSeg == "e" {
			if len(results) == 0 || expectCode {
				break
			}
			expectCode = true
			continue
		}

		if expectCode {
			cleaned := cleanCouponToken(segment)
			if cleaned == "" {
				// Skip pure emoji/punctuation decorations without breaking the chain.
				continue
			}
			code, ok := normalizeLikelyCouponToken(segment, false)
			if !ok {
				break
			}
			results = append(results, code)
			expectCode = false
			continue
		}

		// When expecting a connector, only skip trailing emojis attached as a separate token.
		if cleanCouponToken(segment) == "" {
			continue
		}
		break
	}

	return results
}

func ExtractCouponsWithEntities(messageText string, entities []TelegramEntityExcerpt) []Coupon {
	if !HasCouponIndicator(messageText) {
		return []Coupon{}
	}

	foundURLs := ExtractAllURLs(messageText)
	storeName, storeURL := identifyStore(messageText, foundURLs)

	extractedSet := make(map[string]struct{})
	extractedCodes := make([]string, 0, 4)

	addNormalizedCode := func(upper string) {
		if _, exists := extractedSet[upper]; !exists {
			extractedSet[upper] = struct{}{}
			extractedCodes = append(extractedCodes, upper)
		}
	}

	for _, ent := range entities {
		if ent.Type != "code" {
			continue
		}
		rawCode := strings.TrimSpace(ent.Text)
		if rawCode == "" {
			if s, e, ok := utf16SpanToByteSpan(messageText, ent.Offset, ent.Length); ok {
				rawCode = strings.TrimSpace(messageText[s:e])
			}
		}
		if rawCode == "" || strings.ContainsAny(rawCode, " \t\r\n") {
			continue
		}
		if upper, ok := normalizeLikelyCouponToken(rawCode, true); ok {
			addNormalizedCode(upper)
		}
	}

	lines := strings.Split(messageText, "\n")
	for _, line := range lines {
		line = strings.TrimRight(line, "\r")
		for idx := 0; idx < len(line); idx++ {
			if line[idx] == ':' && isValidColonContext(line, idx) {
				for _, code := range scanCouponChain(line[idx+1:]) {
					addNormalizedCode(code)
				}
			}
		}

		kwMatches := keywordCueRegex.FindAllStringIndex(line, -1)
		for _, m := range kwMatches {
			for _, code := range scanCouponChain(line[m[1]:]) {
				addNormalizedCode(code)
			}
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

func ExtractCoupons(messageText string) []Coupon {
	return ExtractCouponsWithEntities(messageText, nil)
}
