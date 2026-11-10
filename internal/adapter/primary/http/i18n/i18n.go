package i18n

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"strings"
)

//go:embed locales/*.json
var localesFS embed.FS

const (
	LangPtBR    = "pt-BR"
	LangEnUS    = "en-US"
	DefaultLang = LangPtBR
)

type contextKey string

const langContextKey = contextKey("i18n_lang")

type Translator struct {
	messages map[string]map[string]string
}

func NewTranslator() (*Translator, error) {
	langs := []string{LangPtBR, LangEnUS}
	messages := make(map[string]map[string]string, len(langs))

	for _, lang := range langs {
		data, err := localesFS.ReadFile("locales/" + lang + ".json")
		if err != nil {
			return nil, fmt.Errorf("read locale file %s: %w", lang, err)
		}

		var dict map[string]string
		if err := json.Unmarshal(data, &dict); err != nil {
			return nil, fmt.Errorf("unmarshal locale file %s: %w", lang, err)
		}

		messages[lang] = dict
	}

	return &Translator{messages: messages}, nil
}

func (t *Translator) NormalizeLang(lang string) string {
	clean := strings.TrimSpace(strings.ToLower(lang))
	switch {
	case strings.HasPrefix(clean, "pt"):
		return LangPtBR
	case strings.HasPrefix(clean, "en"):
		return LangEnUS
	default:
		return DefaultLang
	}
}

func (t *Translator) T(lang, key string, args ...any) string {
	normalized := t.NormalizeLang(lang)
	dict, ok := t.messages[normalized]
	if !ok {
		dict = t.messages[DefaultLang]
	}

	msg, ok := dict[key]
	if !ok {
		if fallbackDict, hasFallback := t.messages[DefaultLang]; hasFallback {
			msg, ok = fallbackDict[key]
		}
	}

	if !ok {
		return key
	}

	if len(args) > 0 {
		return fmt.Sprintf(msg, args...)
	}

	return msg
}

func (t *Translator) SupportedLangs() []string {
	return []string{LangPtBR, LangEnUS}
}

func WithLanguage(ctx context.Context, lang string) context.Context {
	return context.WithValue(ctx, langContextKey, lang)
}

func GetLanguage(ctx context.Context) string {
	if val, ok := ctx.Value(langContextKey).(string); ok && val != "" {
		return val
	}
	return DefaultLang
}
