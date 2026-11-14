package domain

import (
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf16"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

var (
	priceRegex              = regexp.MustCompile(`R\$\s*([0-9]{1,3}(?:\.[0-9]{3})+,[0-9]{2}|[0-9]{1,3}(?:,[0-9]{3})+\.[0-9]{2}|[0-9]{1,3}(?:\.[0-9]{3})+|[0-9]+(?:[.,][0-9]{1,2})?)`)
	installmentContextRegex = regexp.MustCompile(`(?:^|\s)\d{1,2}\s*x(?:\s|$)`)

	measurementUnitSuffixes = map[string]struct{}{
		"gb": {}, "tb": {}, "mb": {}, "kb": {},
		"hz": {}, "mhz": {}, "ghz": {},
		"mah": {}, "wh": {}, "kw": {}, "w": {}, "v": {}, "kv": {}, "va": {},
		"btu": {}, "db": {}, "dpi": {}, "mp": {},
		"mm": {}, "cm": {}, "km": {}, "m": {},
		"kg": {}, "mg": {}, "mcg": {}, "g": {},
		"ml": {}, "l": {},
		"un": {}, "und": {}, "unid": {}, "pc": {}, "pcs": {}, "pol": {},
		"x": {}, "k": {},
	}

	preservedAlphaNumPrefixes = map[string]struct{}{
		"ddr": {}, "gddr": {}, "lpddr": {},
		"wifi": {}, "usb": {}, "tipoc": {}, "typec": {}, "usbc": {},
		"bt": {}, "cat": {}, "gen": {}, "dimm": {}, "sodimm": {},
		"pcie": {}, "nvme": {}, "hdmi": {}, "dp": {},
	}

	versionModifierTokens = map[string]struct{}{
		"pro": {}, "max": {}, "plus": {}, "ultra": {}, "fe": {},
		"oled": {}, "slim": {}, "lite": {}, "mini": {}, "ti": {},
		"super": {}, "xt": {}, "x3d": {},
	}

	quantifierNouns = map[string]struct{}{
		"controles": {}, "controle": {}, "jogos": {}, "jogo": {}, //nolint:misspell // Portuguese quantifier noun
		"unidades": {}, "unidade": {}, "un": {}, "und": {}, "unid": {},
		"pecas": {}, "peca": {}, "saches": {}, "sache": {},
		"latas": {}, "lata": {}, "capsulas": {}, "capsula": {},
		"doses": {}, "dose": {}, "frascos": {}, "frasco": {},
		"pacotes": {}, "pacote": {}, "kits": {}, "kit": {},
		"anos": {}, "ano": {}, "meses": {}, "mes": {},
		"dias": {}, "dia": {}, "horas": {}, "hora": {},
		"minutos": {}, "minuto": {}, "segundos": {}, "segundo": {},
		"vezes": {}, "vez": {}, "parcelas": {}, "parcela": {},
		"itens": {}, "item": {}, "pares": {}, "par": {},
		"boosters": {}, "booster": {}, "garrafas": {}, "garrafa": {},
		"cores": {}, "cor": {}, "portas": {}, "porta": {},
		"entradas": {}, "entrada": {}, "saidas": {}, "saida": {},
		"gavetas": {}, "prateleiras": {}, "cadeiras": {}, "pes": {},
		"pontos": {}, "reais": {}, "centavos": {}, "porcento": {},
		"cupom": {}, "cupons": {}, "x": {},
	}

	forwardSynonyms = map[string][]string{
		"ns2":       {"nintendo", "switch", "2"},
		"ns1":       {"nintendo", "switch"},
		"ps5":       {"playstation", "5"},
		"galaxys25": {"galaxy", "s25"},
		"ml":        {"mercado", "livre"},
	}

	alwaysSecondaryTokens = map[string]struct{}{
		"parcela": {}, "parcelas": {}, "frete": {}, "envio": {},
		"entrega": {}, "cashback": {}, "volta": {}, "era": {},
		"antes": {}, "tabela": {}, "abatimento": {}, "limite": {},
	}

	discountContextTokens = map[string]struct{}{
		"desconto": {}, "cupom": {}, "cupons": {}, "off": {},
		"economize": {}, "economia": {},
	}

	secondaryPrefixPhrases = []string{
		"acima de", "compras acima", "pedido minimo", "minimo de",
		"gastando", "limite de", "limitado a", "em compras de",
		"off em", "desconto em", "preco antigo", "valor antigo",
		"sem juros",
	}

	secondarySuffixPhrases = []string{
		"de desconto", "no cupom", "menor preco", "menor valor",
		"abaixo do", "mais barato",
	}

	unitSuffixPhrases = []string{
		"por unidade", "a unidade", "por booster", "por sache",
		"por lata", "por capsula", "por dose", "por peca",
		"por frasco", "por pacote",
	}

	unitSuffixTokens = map[string]struct{}{
		"cada": {}, "unid": {},
	}
)

type ProductMatch struct {
	InterestID     int64    `json:"interest_id"`
	UserID         int64    `json:"user_id"`
	ExtractedPrice *float64 `json:"extracted_price,omitempty"`
}

type ParsedMessage struct {
	NormalizedText string
	Tokens         []string
	TokenSet       map[string]struct{}
}

func isASCIIAlpha(b byte) bool {
	return b >= 'a' && b <= 'z'
}

func isASCIIDigit(b byte) bool {
	return b >= '0' && b <= '9'
}

func isASCIIAlphaNum(b byte) bool {
	return isASCIIAlpha(b) || isASCIIDigit(b)
}

func isAllASCIILetters(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if !isASCIIAlpha(s[i]) {
			return false
		}
	}
	return true
}

func isAllASCIIDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if !isASCIIDigit(s[i]) {
			return false
		}
	}
	return true
}

func hasDigit(s string) bool {
	for i := 0; i < len(s); i++ {
		if isASCIIDigit(s[i]) {
			return true
		}
	}
	return false
}

func isMeasurementUnitSuffix(digits, suffix string) bool {
	if _, ok := measurementUnitSuffixes[suffix]; !ok {
		return false
	}
	switch suffix {
	case "x":
		return len(digits) <= 3
	case "k":
		return len(digits) <= 2
	case "g":
		// Keep single-digit network generations (3g, 4g, 5g, 6g) intact.
		if len(digits) == 1 && (digits[0] == '3' || digits[0] == '4' || digits[0] == '5' || digits[0] == '6') {
			return false
		}
		return true
	default:
		return true
	}
}

func shouldSplitAlphaPrefix(prefix string) bool {
	if prefix == "rx" {
		return true
	}
	if _, preserved := preservedAlphaNumPrefixes[prefix]; preserved {
		return false
	}
	return len(prefix) >= 3
}

func splitSingleAlphaDigit(tok string) (letters, digits string, kind int) {
	// kind: 0 = not single-transition, 1 = alpha->digit, 2 = digit->alpha
	if len(tok) < 2 {
		return "", "", 0
	}
	firstIsDigit := isASCIIDigit(tok[0])
	transitions := 0
	splitIdx := -1
	prevIsDigit := firstIsDigit
	for i := 1; i < len(tok); i++ {
		curIsDigit := isASCIIDigit(tok[i])
		if curIsDigit != prevIsDigit {
			transitions++
			if transitions > 1 {
				return "", "", 0
			}
			splitIdx = i
			prevIsDigit = curIsDigit
		}
	}
	if transitions != 1 {
		return "", "", 0
	}
	if firstIsDigit {
		return tok[splitIdx:], tok[:splitIdx], 2
	}
	return tok[:splitIdx], tok[splitIdx:], 1
}

func appendNormalizedToken(b *strings.Builder, tok string) {
	letters, digits, kind := splitSingleAlphaDigit(tok)
	switch kind {
	case 1:
		if shouldSplitAlphaPrefix(letters) {
			b.WriteString(letters)
			b.WriteByte(' ')
			b.WriteString(digits)
			return
		}
	case 2:
		if isMeasurementUnitSuffix(digits, letters) {
			b.WriteString(digits)
			b.WriteByte(' ')
			b.WriteString(letters)
			return
		}
	}
	b.WriteString(tok)
}

func normalizeHardwarePunctuation(s string) string {
	if !strings.ContainsAny(s, ".-") {
		return s
	}

	out := make([]byte, 0, len(s))
	n := len(s)

	for i := 0; i < n; i++ {
		ch := s[i]
		switch ch {
		case '.':
			// Preserve M.2 hardware form factor as "m2".
			if i == 1 || (i > 1 && !isASCIIAlphaNum(s[i-2])) {
				if i >= 1 && s[i-1] == 'm' && i+1 < n && s[i+1] == '2' && (i+2 == n || !isASCIIAlphaNum(s[i+2])) {
					continue
				}
			}
			out = append(out, ' ')
		case '-':
			if shouldMergeHardwareHyphen(s, i) {
				continue
			}
			out = append(out, ' ')
		default:
			out = append(out, ch)
		}
	}

	return string(out)
}

func shouldMergeHardwareHyphen(s string, hyphenIdx int) bool {
	n := len(s)
	// Named hardware terms: wi-fi, usb-c, tipo-c, type-c
	leftWords := []string{"wi", "usb", "tipo", "type"}
	rightWords := []string{"fi", "c", "c", "c"}
	for idx, lw := range leftWords {
		rw := rightWords[idx]
		start := hyphenIdx - len(lw)
		end := hyphenIdx + 1 + len(rw)
		if start >= 0 && end <= n && s[start:hyphenIdx] == lw && s[hyphenIdx+1:end] == rw {
			leftOK := start == 0 || !isASCIIAlphaNum(s[start-1])
			rightOK := end == n || !isASCIIAlphaNum(s[end]) || (lw == "wi" && isASCIIDigit(s[end]))
			if leftOK && rightOK {
				return true
			}
		}
	}

	// Hyphenated model suffixes such as "16-e" or "512-gb".
	dStart := hyphenIdx
	for dStart > 0 && isASCIIDigit(s[dStart-1]) {
		dStart--
	}
	if dStart == hyphenIdx || (dStart > 0 && isASCIIAlphaNum(s[dStart-1])) {
		return false
	}

	lEnd := hyphenIdx + 1
	for lEnd < n && isASCIIAlpha(s[lEnd]) {
		lEnd++
	}
	suffixLen := lEnd - (hyphenIdx + 1)
	if suffixLen < 1 || suffixLen > 3 {
		return false
	}
	if lEnd < n && s[lEnd] != ' ' && s[lEnd] != '.' {
		return false
	}

	return true
}

func NormalizeText(input string) string {
	if input == "" {
		return ""
	}

	needsNFD := false
	for i := 0; i < len(input); i++ {
		if input[i] >= utf8.RuneSelf {
			needsNFD = true
			break
		}
	}

	source := input
	if needsNFD {
		source = norm.NFD.String(input)
	}

	var pre strings.Builder
	pre.Grow(len(source))

	for _, r := range source {
		if r >= 0x0300 && unicode.Is(unicode.Mn, r) {
			continue
		}
		switch {
		case r >= 'A' && r <= 'Z':
			pre.WriteRune(r + 32)
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '.' || r == '-':
			pre.WriteRune(r)
		default:
			pre.WriteByte(' ')
		}
	}

	cleaned := normalizeHardwarePunctuation(pre.String())

	var out strings.Builder
	out.Grow(len(cleaned) + 8)

	i := 0
	for i < len(cleaned) {
		for i < len(cleaned) && cleaned[i] == ' ' {
			i++
		}
		if i >= len(cleaned) {
			break
		}
		start := i
		for i < len(cleaned) && cleaned[i] != ' ' {
			i++
		}
		if out.Len() > 0 {
			out.WriteByte(' ')
		}
		appendNormalizedToken(&out, cleaned[start:i])
	}

	return out.String()
}

func Tokenize(input string) []string {
	normalized := NormalizeText(input)
	if normalized == "" {
		return []string{}
	}
	return strings.Fields(normalized)
}

func BuildTokenSet(tokens []string) map[string]struct{} {
	set := make(map[string]struct{}, len(tokens))
	for _, tok := range tokens {
		set[tok] = struct{}{}
	}
	return set
}

func ParseMessage(messageText string) ParsedMessage {
	normText := NormalizeText(messageText)
	if normText == "" {
		return ParsedMessage{}
	}

	tokens := strings.Fields(normText)
	tokenSet := make(map[string]struct{}, len(tokens)*2)

	for _, tok := range tokens {
		tokenSet[tok] = struct{}{}

		letters, digits, kind := splitSingleAlphaDigit(tok)
		if kind == 1 && (len(letters) >= 2 || letters == "s" || letters == "m") {
			tokenSet[letters] = struct{}{}
			tokenSet[digits] = struct{}{}
		} else if kind == 2 && len(digits) >= 4 && (letters == "x" || letters == "k") {
			tokenSet[digits] = struct{}{}
		}
	}

	for i := 0; i+1 < len(tokens); i++ {
		cur, next := tokens[i], tokens[i+1]
		if isAllASCIILetters(cur) && isAllASCIIDigits(next) {
			tokenSet[cur+next] = struct{}{}
		} else if isAllASCIIDigits(cur) && isMeasurementUnitSuffix(cur, next) {
			tokenSet[cur+next] = struct{}{}
		}
	}

	expandSynonyms(&tokenSet, tokens)

	return ParsedMessage{
		NormalizedText: normText,
		Tokens:         tokens,
		TokenSet:       tokenSet,
	}
}

func expandSynonyms(tokenSet *map[string]struct{}, tokens []string) {
	set := *tokenSet

	// Forward expansion (abbreviation -> canonical tokens).
	for _, tok := range tokens {
		if expanded, ok := forwardSynonyms[tok]; ok {
			for _, expTok := range expanded {
				set[expTok] = struct{}{}
			}
		}
	}

	// Reverse expansion with proximity/quantifier guards.
	if _, hasML := set["ml"]; !hasML {
		for i := 0; i+1 < len(tokens); i++ {
			if tokens[i] == "mercado" && tokens[i+1] == "livre" {
				set["ml"] = struct{}{}
				break
			}
		}
	}

	if _, hasNS1 := set["ns1"]; !hasNS1 {
		for i, tok := range tokens {
			if tok == "nintendo" && hasNearbyToken(tokens, i, "switch", 2) {
				set["ns1"] = struct{}{}
				break
			}
		}
	}

	if _, hasNS2 := set["ns2"]; !hasNS2 {
		for i, tok := range tokens {
			if tok == "switch" && hasNearbyToken(tokens, i, "nintendo", 2) {
				for j := max(0, i-3); j <= min(len(tokens)-1, i+3); j++ {
					if tokens[j] == "2" && isValidShortNumberOccurrence(tokens, j, nil) {
						set["ns2"] = struct{}{}
						break
					}
				}
			}
		}
	}

	if _, hasPS5 := set["ps5"]; !hasPS5 {
		for i, tok := range tokens {
			if tok == "playstation" {
				for j := max(0, i-3); j <= min(len(tokens)-1, i+3); j++ {
					if tokens[j] == "5" && isValidShortNumberOccurrence(tokens, j, nil) {
						set["ps5"] = struct{}{}
						break
					}
				}
			}
		}
	}

	if _, hasGalaxyS25 := set["galaxys25"]; !hasGalaxyS25 {
		if _, hasGalaxy := set["galaxy"]; hasGalaxy {
			if _, hasS25 := set["s25"]; hasS25 {
				set["galaxys25"] = struct{}{}
			}
		}
	}
}

func hasNearbyToken(tokens []string, center int, target string, maxDist int) bool {
	lo := max(0, center-maxDist)
	hi := min(len(tokens)-1, center+maxDist)
	for i := lo; i <= hi; i++ {
		if i != center && tokens[i] == target {
			return true
		}
	}
	return false
}

func IsDamerauLevenshteinLE1(s1, s2 string) bool {
	if s1 == s2 {
		return true
	}

	l1, l2 := len(s1), len(s2)
	diff := l1 - l2
	if diff < -1 || diff > 1 {
		return false
	}

	if diff == 0 {
		for i := 0; i < l1; i++ {
			if s1[i] != s2[i] {
				if s1[i+1:] == s2[i+1:] {
					return true
				}
				if i+1 < l1 && s1[i] == s2[i+1] && s1[i+1] == s2[i] && s1[i+2:] == s2[i+2:] {
					return true
				}
				return false
			}
		}
		return true
	}

	if l1 > l2 {
		s1, s2 = s2, s1
		l1 = l2
	}

	i := 0
	for i < l1 && s1[i] == s2[i] {
		i++
	}
	return s1[i:] == s2[i+1:]
}

func IsLevenshteinLE1(s1, s2 string) bool {
	return IsDamerauLevenshteinLE1(s1, s2)
}

func isPluralEquivalent(a, b string) bool {
	la, lb := len(a), len(b)
	if la < 4 || lb < 4 || hasDigit(a) || hasDigit(b) {
		return false
	}
	if la > lb {
		a, b = b, a
		la, lb = lb, la
	}
	if la+1 == lb && b[la] == 's' && b[:la] == a {
		return true
	}
	if la+2 == lb && (a[la-1] == 'r' || a[la-1] == 'z') && b[la] == 'e' && b[la+1] == 's' && b[:la] == a {
		return true
	}
	return false
}

func isShortModifierToken(tok string) bool {
	if isAllASCIIDigits(tok) && (len(tok) == 1 || len(tok) == 2) {
		return true
	}
	_, isVersionMod := versionModifierTokens[tok]
	return isVersionMod
}

func isValidShortNumberOccurrence(tokens []string, idx int, positiveTokens []string) bool {
	if idx+1 >= len(tokens) {
		return true
	}
	next := tokens[idx+1]
	for _, pos := range positiveTokens {
		if pos == next {
			return true
		}
	}
	if _, isQuant := quantifierNouns[next]; isQuant {
		return false
	}
	return true
}

func tokenMatchesSingle(msgTok, queryTok string) bool {
	if msgTok == queryTok {
		return true
	}
	if isPluralEquivalent(msgTok, queryTok) {
		return true
	}
	if len(queryTok) >= 5 && len(msgTok) >= 5 && !hasDigit(queryTok) && !hasDigit(msgTok) {
		if IsDamerauLevenshteinLE1(queryTok, msgTok) {
			return true
		}
	}
	if expanded, ok := forwardSynonyms[msgTok]; ok {
		for _, expTok := range expanded {
			if expTok == queryTok {
				return true
			}
		}
	}
	letters, digits, kind := splitSingleAlphaDigit(msgTok)
	if kind == 1 && (len(letters) >= 2 || letters == "s" || letters == "m") {
		if letters == queryTok || digits == queryTok {
			return true
		}
	} else if kind == 2 && len(digits) >= 4 && (letters == "x" || letters == "k") && digits == queryTok {
		return true
	}
	return false
}

func tokenMatchesAt(tokens []string, idx int, queryTok string, positiveTokens []string) bool {
	if tokenMatchesSingle(tokens[idx], queryTok) {
		if isAllASCIIDigits(tokens[idx]) && tokens[idx] == queryTok && len(queryTok) <= 2 {
			if !isValidShortNumberOccurrence(tokens, idx, positiveTokens) {
				return false
			}
		}
		return true
	}

	if idx+1 < len(tokens) && tokens[idx]+tokens[idx+1] == queryTok {
		return true
	}
	if idx > 0 && tokens[idx-1]+tokens[idx] == queryTok {
		return true
	}

	switch queryTok {
	case "ps5":
		if (tokens[idx] == "playstation" && hasNearbyToken(tokens, idx, "5", 3)) ||
			(tokens[idx] == "5" && hasNearbyToken(tokens, idx, "playstation", 3)) {
			return true
		}
	case "ns2":
		if (tokens[idx] == "switch" || tokens[idx] == "nintendo") && hasNearbyToken(tokens, idx, "2", 3) {
			return true
		}
	case "ns1":
		if (tokens[idx] == "switch" && hasNearbyToken(tokens, idx, "nintendo", 2)) ||
			(tokens[idx] == "nintendo" && hasNearbyToken(tokens, idx, "switch", 2)) {
			return true
		}
	case "galaxys25":
		if tokens[idx] == "galaxy" || tokens[idx] == "s25" {
			return true
		}
	case "ml":
		if (tokens[idx] == "mercado" && idx+1 < len(tokens) && tokens[idx+1] == "livre") ||
			(tokens[idx] == "livre" && idx > 0 && tokens[idx-1] == "mercado") {
			return true
		}
	}

	return false
}

func checkModifierProximity(positiveTokens []string, parsed *ParsedMessage) bool {
	if len(positiveTokens) < 2 {
		return true
	}

	hasAnyModifier := false
	hasAnchor := false
	for _, pos := range positiveTokens {
		if isShortModifierToken(pos) {
			hasAnyModifier = true
		} else {
			hasAnchor = true
		}
	}
	if !hasAnyModifier {
		return true
	}

	const maxWindow = 3
	for modIdx, mod := range positiveTokens {
		if !isShortModifierToken(mod) {
			continue
		}

		satisfied := false
		for i := range parsed.Tokens {
			if !tokenMatchesAt(parsed.Tokens, i, mod, positiveTokens) {
				continue
			}

			lo := max(0, i-maxWindow)
			hi := min(len(parsed.Tokens)-1, i+maxWindow)
			for j := lo; j <= hi && !satisfied; j++ {
				for ancIdx, anc := range positiveTokens {
					if ancIdx == modIdx {
						continue
					}
					if hasAnchor && isShortModifierToken(anc) {
						continue
					}
					if tokenMatchesAt(parsed.Tokens, j, anc, positiveTokens) {
						satisfied = true
						break
					}
				}
			}
			if satisfied {
				break
			}
		}

		if !satisfied {
			return false
		}
	}

	return true
}

func MatchParsedMessage(interest *UserInterest, parsed *ParsedMessage) bool {
	if interest == nil || !interest.Active || len(interest.PositiveTokens) == 0 || parsed == nil || len(parsed.Tokens) == 0 {
		return false
	}

	for _, neg := range interest.NegativeTokens {
		if _, exists := parsed.TokenSet[neg]; exists {
			return false
		}
		if len(neg) >= 4 && !hasDigit(neg) {
			for tok := range parsed.TokenSet {
				if isPluralEquivalent(neg, tok) {
					return false
				}
			}
		}
	}

	for _, pos := range interest.PositiveTokens {
		if _, exists := parsed.TokenSet[pos]; exists {
			continue
		}
		matched := false
		if !hasDigit(pos) && len(pos) >= 4 {
			for tok := range parsed.TokenSet {
				if isPluralEquivalent(pos, tok) {
					matched = true
					break
				}
				if len(pos) >= 5 && len(tok) >= 5 && !hasDigit(tok) && IsDamerauLevenshteinLE1(pos, tok) {
					matched = true
					break
				}
			}
		}
		if !matched {
			return false
		}
	}

	return checkModifierProximity(interest.PositiveTokens, parsed)
}

func utf16SpanToByteSpan(text string, utf16Offset, utf16Length int) (int, int, bool) {
	if utf16Offset < 0 || utf16Length <= 0 || text == "" {
		return 0, 0, false
	}

	targetEnd := utf16Offset + utf16Length
	u16Pos := 0
	byteStart := -1

	for byteIdx, r := range text {
		if byteStart < 0 && u16Pos >= utf16Offset {
			byteStart = byteIdx
		}
		step := utf16.RuneLen(r)
		if step < 0 {
			step = 1
		}
		u16Pos += step
		if byteStart >= 0 && u16Pos >= targetEnd {
			return byteStart, byteIdx + utf8.RuneLen(r), true
		}
	}

	if byteStart >= 0 && byteStart < len(text) {
		return byteStart, len(text), true
	}
	return 0, 0, false
}

func extractPriceClausePrefix(text string, prevMatchEnd, matchStart int) string {
	lineStart := strings.LastIndexAny(text[:matchStart], "\r\n") + 1
	boundStart := lineStart
	if prevMatchEnd > boundStart {
		boundStart = prevMatchEnd
	}
	if matchStart-boundStart > 48 {
		boundStart = matchStart - 48
	}
	for boundStart < matchStart && !utf8.RuneStart(text[boundStart]) {
		boundStart++
	}

	raw := text[boundStart:matchStart]
	for i := len(raw) - 1; i >= 0; i-- {
		switch raw[i] {
		case '!', '?', ';', '|':
			return raw[i+1:]
		case ',', '.':
			if i+1 >= len(raw) || raw[i+1] < '0' || raw[i+1] > '9' {
				return raw[i+1:]
			}
		}
	}

	return raw
}

func extractPriceClauseSuffix(text string, matchEnd, nextMatchStart int) string {
	boundEnd := len(text)
	if nextMatchStart > matchEnd && nextMatchStart < boundEnd {
		boundEnd = nextMatchStart
	}
	if nl := strings.IndexAny(text[matchEnd:boundEnd], "\r\n"); nl >= 0 {
		boundEnd = matchEnd + nl
	}
	if boundEnd-matchEnd > 40 {
		boundEnd = matchEnd + 40
	}
	for boundEnd > matchEnd && boundEnd < len(text) && !utf8.RuneStart(text[boundEnd]) {
		boundEnd--
	}

	return text[matchEnd:boundEnd]
}

func hasTokenWord(norm, target string) bool {
	start := 0
	for {
		idx := strings.Index(norm[start:], target)
		if idx < 0 {
			return false
		}
		pos := start + idx
		leftOK := pos == 0 || norm[pos-1] == ' '
		end := pos + len(target)
		rightOK := end == len(norm) || norm[end] == ' '
		if leftOK && rightOK {
			return true
		}
		start = pos + 1
	}
}

func isSecondaryPricePrefix(norm, text string, matchEnd, nextMatchStart int) bool {
	if norm == "" {
		return false
	}

	// Historical "De R$ X" when followed in the same clause by "por R$ Y".
	if (norm == "de" || strings.HasSuffix(norm, " de")) && nextMatchStart < len(text) {
		between := NormalizeText(text[matchEnd:nextMatchStart])
		if between == "por" || strings.HasSuffix(between, " por") {
			return true
		}
	}

	if idx := strings.LastIndex(norm, " ou "); idx >= 0 {
		norm = norm[idx+4:]
	} else if norm == "ou" || strings.HasSuffix(norm, " ou") {
		return false
	} else if strings.HasPrefix(norm, "ou ") {
		norm = norm[3:]
	}

	if norm == "" {
		return false
	}

	if installmentContextRegex.MatchString(norm) {
		return true
	}

	for _, phrase := range secondaryPrefixPhrases {
		if strings.Contains(norm, phrase) {
			return true
		}
	}

	tokens := strings.Fields(norm)
	hasDiscountCue := false
	for _, tok := range tokens {
		if _, exists := alwaysSecondaryTokens[tok]; exists {
			return true
		}
		if _, exists := discountContextTokens[tok]; exists {
			hasDiscountCue = true
		}
	}

	if hasDiscountCue {
		if strings.Contains(norm, "sai por") || strings.Contains(norm, "fica por") || strings.Contains(norm, "saindo por") {
			return false
		}
		return true
	}

	return false
}

func classifyPriceSuffix(rawSuffix string) (isSecondary bool, isUnit bool) {
	trimmed := strings.TrimSpace(strings.ToLower(rawSuffix))
	if strings.HasPrefix(trimmed, "/un") || strings.HasPrefix(trimmed, "/kg") ||
		(strings.HasPrefix(trimmed, "/l") && (len(trimmed) == 2 || !isASCIIAlpha(trimmed[2]))) {
		return false, true
	}

	norm := NormalizeText(rawSuffix)
	if norm == "" {
		return false, false
	}

	if hasTokenWord(norm, "off") {
		return true, false
	}
	for _, phrase := range secondarySuffixPhrases {
		if strings.Contains(norm, phrase) {
			return true, false
		}
	}

	for _, phrase := range unitSuffixPhrases {
		if strings.Contains(norm, phrase) {
			return false, true
		}
	}
	for tok := range unitSuffixTokens {
		if hasTokenWord(norm, tok) {
			return false, true
		}
	}

	return false, false
}

func overlapsAnySpan(start, end int, spans [][2]int) bool {
	for _, sp := range spans {
		if start < sp[1] && end > sp[0] {
			return true
		}
	}
	return false
}

func ExtractLowestPriceWithEntities(text string, entities []TelegramEntityExcerpt) (float64, bool) {
	matches := priceRegex.FindAllStringSubmatchIndex(text, -1)
	if len(matches) == 0 {
		return 0, false
	}

	var strikeSpans [][2]int
	for _, ent := range entities {
		if ent.Type == "strike" {
			if s, e, ok := utf16SpanToByteSpan(text, ent.Offset, ent.Length); ok {
				strikeSpans = append(strikeSpans, [2]int{s, e})
			}
		}
	}

	var primaryMin float64
	hasPrimary := false
	var unitMin float64
	hasUnit := false
	prevMatchEnd := 0

	for idx, match := range matches {
		if len(match) < 4 {
			continue
		}
		matchStart, matchEnd := match[0], match[1]
		raw := text[match[2]:match[3]]
		val, err := parsePrice(raw)

		prevEnd := prevMatchEnd
		prevMatchEnd = matchEnd

		if err != nil || val <= 0 {
			continue
		}
		if overlapsAnySpan(matchStart, matchEnd, strikeSpans) {
			continue
		}

		nextMatchStart := len(text)
		if idx+1 < len(matches) {
			nextMatchStart = matches[idx+1][0]
		}

		prefixNorm := NormalizeText(extractPriceClausePrefix(text, prevEnd, matchStart))
		if isSecondaryPricePrefix(prefixNorm, text, matchEnd, nextMatchStart) {
			continue
		}

		rawSuffix := extractPriceClauseSuffix(text, matchEnd, nextMatchStart)
		secSuffix, unitSuffix := classifyPriceSuffix(rawSuffix)
		if secSuffix {
			continue
		}

		if unitSuffix {
			if !hasUnit || val < unitMin {
				unitMin = val
				hasUnit = true
			}
			continue
		}

		if !hasPrimary || val < primaryMin {
			primaryMin = val
			hasPrimary = true
		}
	}

	if hasPrimary {
		return primaryMin, true
	}
	if hasUnit {
		return unitMin, true
	}
	return 0, false
}

func ExtractLowestPrice(text string) (float64, bool) {
	return ExtractLowestPriceWithEntities(text, nil)
}

func ExtractAllPrices(text string) []float64 {
	matches := priceRegex.FindAllStringSubmatch(text, -1)
	if len(matches) == 0 {
		return []float64{}
	}

	prices := make([]float64, 0, len(matches))
	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		val, err := parsePrice(match[1])
		if err != nil || val <= 0 {
			continue
		}
		prices = append(prices, val)
	}

	return prices
}

func parsePrice(raw string) (float64, error) {
	clean := strings.TrimSpace(raw)
	hasComma := strings.Contains(clean, ",")
	hasDot := strings.Contains(clean, ".")

	switch {
	case hasComma && hasDot:
		lastComma := strings.LastIndex(clean, ",")
		lastDot := strings.LastIndex(clean, ".")
		if lastComma > lastDot {
			clean = strings.ReplaceAll(clean, ".", "")
			clean = strings.ReplaceAll(clean, ",", ".")
		} else {
			clean = strings.ReplaceAll(clean, ",", "")
		}
	case hasComma:
		clean = strings.ReplaceAll(clean, ",", ".")
	case hasDot:
		parts := strings.Split(clean, ".")
		if len(parts) > 2 {
			clean = strings.ReplaceAll(clean, ".", "")
		} else if len(parts) == 2 && len(parts[1]) == 3 && len(parts[0]) >= 1 && len(parts[0]) <= 3 {
			clean = parts[0] + parts[1]
		}
	}

	return strconv.ParseFloat(clean, 64)
}

func MatchProduct(interest *UserInterest, messageText string) (*ProductMatch, bool) {
	if interest == nil || !interest.Active || len(interest.PositiveTokens) == 0 {
		return nil, false
	}

	parsed := ParseMessage(messageText)
	if !MatchParsedMessage(interest, &parsed) {
		return nil, false
	}

	lowestPrice, hasPrice := ExtractLowestPrice(messageText)
	var extractedPrice *float64
	if hasPrice {
		extractedPrice = &lowestPrice
	}

	if interest.MaxPrice != nil {
		if !hasPrice || lowestPrice > *interest.MaxPrice {
			return nil, false
		}
	}

	return &ProductMatch{
		InterestID:     interest.ID,
		UserID:         interest.UserID,
		ExtractedPrice: extractedPrice,
	}, true
}
