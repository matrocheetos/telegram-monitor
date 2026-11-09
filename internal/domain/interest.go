package domain

import (
	"strings"
	"time"
)

type UserInterest struct {
	ID             int64     `json:"id" db:"id"`
	UserID         int64     `json:"user_id" db:"user_id"`
	RawTerm        string    `json:"raw_term" db:"raw_term"`
	PositiveTokens []string  `json:"positive_tokens" db:"positive_tokens"`
	NegativeTokens []string  `json:"negative_tokens" db:"negative_tokens"`
	MaxPrice       *float64  `json:"max_price,omitempty" db:"max_price"`
	Active         bool      `json:"active" db:"active"`
	CreatedAt      time.Time `json:"created_at" db:"created_at"`
}

func NewUserInterest(userID int64, rawTerm string, maxPrice *float64) (*UserInterest, error) {
	if userID <= 0 {
		return nil, ErrUnauthorized
	}
	if maxPrice != nil && *maxPrice <= 0 {
		return nil, ErrInvalidPrice
	}

	cleanRaw := strings.TrimSpace(rawTerm)
	if cleanRaw == "" {
		return nil, ErrEmptyInterestTerm
	}

	words := strings.Fields(cleanRaw)
	posSet := make(map[string]struct{}, len(words))
	negSet := make(map[string]struct{}, len(words))

	positiveTokens := make([]string, 0, len(words))
	negativeTokens := make([]string, 0, len(words))

	for _, word := range words {
		if strings.HasPrefix(word, "-") {
			stripped := strings.TrimPrefix(word, "-")
			tokens := Tokenize(stripped)
			for _, tok := range tokens {
				if _, exists := negSet[tok]; !exists {
					negSet[tok] = struct{}{}
					negativeTokens = append(negativeTokens, tok)
				}
			}
			continue
		}

		tokens := Tokenize(word)
		for _, tok := range tokens {
			if _, exists := posSet[tok]; !exists {
				posSet[tok] = struct{}{}
				positiveTokens = append(positiveTokens, tok)
			}
		}
	}

	if len(positiveTokens) == 0 {
		return nil, ErrEmptyInterestTerm
	}

	return &UserInterest{
		UserID:         userID,
		RawTerm:        cleanRaw,
		PositiveTokens: positiveTokens,
		NegativeTokens: negativeTokens,
		MaxPrice:       maxPrice,
		Active:         true,
		CreatedAt:      time.Now().UTC(),
	}, nil
}
