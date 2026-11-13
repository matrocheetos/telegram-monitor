package domain

import (
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

var (
	letterDigitBoundaryRegex = regexp.MustCompile(`([a-z])([0-9])`)
	digitLetterBoundaryRegex = regexp.MustCompile(`([0-9])([a-z])`)
	priceRegex               = regexp.MustCompile(`R\$\s*([0-9]{1,3}(?:\.[0-9]{3})+,[0-9]{2}|[0-9]{1,3}(?:\.[0-9]{3})+|[0-9]+(?:[.,][0-9]{1,2})?)`)
)

type ProductMatch struct {
	InterestID     int64    `json:"interest_id"`
	UserID         int64    `json:"user_id"`
	ExtractedPrice *float64 `json:"extracted_price,omitempty"`
}

func NormalizeText(input string) string {
	if input == "" {
		return ""
	}

	decomposed := norm.NFD.String(input)
	var builder strings.Builder
	builder.Grow(len(decomposed) + 8)

	for _, r := range decomposed {
		if unicode.Is(unicode.Mn, r) {
			continue
		}
		switch {
		case r >= 'A' && r <= 'Z':
			builder.WriteRune(r + 32)
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			builder.WriteRune(r)
		default:
			builder.WriteByte(' ')
		}
	}

	normalized := builder.String()
	normalized = letterDigitBoundaryRegex.ReplaceAllString(normalized, "$1 $2")
	normalized = digitLetterBoundaryRegex.ReplaceAllString(normalized, "$1 $2")

	return strings.Join(strings.Fields(normalized), " ")
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

func IsLevenshteinLE1(s1, s2 string) bool {
	l1 := len(s1)
	l2 := len(s2)
	diff := l1 - l2
	if diff < -1 || diff > 1 {
		return false
	}

	if diff == 0 {
		mismatches := 0
		for i := 0; i < l1; i++ {
			if s1[i] != s2[i] {
				mismatches++
				if mismatches > 1 {
					return false
				}
			}
		}
		return true
	}

	if l1 > l2 {
		s1, s2 = s2, s1
		l1, l2 = l2, l1
	}

	i, j := 0, 0
	mismatches := 0
	for i < l1 && j < l2 {
		if s1[i] != s2[j] {
			mismatches++
			if mismatches > 1 {
				return false
			}
			j++
		} else {
			i++
			j++
		}
	}

	return mismatches+(l2-j) <= 1
}

func ExtractLowestPrice(text string) (float64, bool) {
	matches := priceRegex.FindAllStringSubmatch(text, -1)
	if len(matches) == 0 {
		return 0, false
	}

	var lowest float64
	found := false

	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		raw := match[1]
		val, err := parsePrice(raw)
		if err != nil || val <= 0 {
			continue
		}

		if !found || val < lowest {
			lowest = val
			found = true
		}
	}

	return lowest, found
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
		clean = strings.ReplaceAll(clean, ".", "")
		clean = strings.ReplaceAll(clean, ",", ".")
	case hasComma:
		clean = strings.ReplaceAll(clean, ",", ".")
	case hasDot:
		parts := strings.Split(clean, ".")
		if len(parts) == 2 && len(parts[1]) == 3 {
			clean = parts[0] + parts[1]
		}
	}

	return strconv.ParseFloat(clean, 64)
}

func MatchProduct(interest *UserInterest, messageText string) (*ProductMatch, bool) {
	if interest == nil || !interest.Active || len(interest.PositiveTokens) == 0 {
		return nil, false
	}

	msgTokens := Tokenize(messageText)
	if len(msgTokens) == 0 {
		return nil, false
	}

	tokenSet := BuildTokenSet(msgTokens)

	for _, neg := range interest.NegativeTokens {
		if _, exists := tokenSet[neg]; exists {
			return nil, false
		}
	}

	for _, pos := range interest.PositiveTokens {
		matched := false
		if _, exists := tokenSet[pos]; exists {
			matched = true
		} else if len(pos) >= 5 {
			for tok := range tokenSet {
				if IsLevenshteinLE1(pos, tok) {
					matched = true
					break
				}
			}
		}

		if !matched {
			return nil, false
		}
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
