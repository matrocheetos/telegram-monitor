package i18n_test

import (
	"context"
	"testing"

	"github.com/matrocheetos/telegram-monitor/internal/adapter/primary/http/i18n"
)

func TestTranslator_New(t *testing.T) {
	tr, err := i18n.NewTranslator()
	if err != nil {
		t.Fatalf("unexpected error creating translator: %v", err)
	}

	if len(tr.SupportedLangs()) != 2 {
		t.Fatalf("expected 2 supported langs, got %d", len(tr.SupportedLangs()))
	}
}

func TestTranslator_NormalizeLang(t *testing.T) {
	tr, err := i18n.NewTranslator()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tests := []struct {
		input    string
		expected string
	}{
		{"pt", i18n.LangPtBR},
		{"pt-BR", i18n.LangPtBR},
		{"PT-br", i18n.LangPtBR},
		{"en", i18n.LangEnUS},
		{"en-US", i18n.LangEnUS},
		{"EN", i18n.LangEnUS},
		{"es", i18n.DefaultLang},
		{"", i18n.DefaultLang},
	}

	for _, tt := range tests {
		got := tr.NormalizeLang(tt.input)
		if got != tt.expected {
			t.Errorf("NormalizeLang(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestTranslator_T(t *testing.T) {
	tr, err := i18n.NewTranslator()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ptTitle := tr.T("pt-BR", "brand_title")
	if ptTitle != "TELEGRAM MONITOR" {
		t.Errorf("expected TELEGRAM MONITOR, got %q", ptTitle)
	}

	enTitle := tr.T("en-US", "login_title")
	if enTitle != "Dashboard Access" {
		t.Errorf("expected 'Dashboard Access', got %q", enTitle)
	}

	ptConnected := tr.T("pt-BR", "connected_as", "mateus")
	if ptConnected != "Conectado como: mateus" {
		t.Errorf("expected 'Conectado como: mateus', got %q", ptConnected)
	}

	enConnected := tr.T("en-US", "connected_as", "mateus")
	if enConnected != "Logged in as: mateus" {
		t.Errorf("expected 'Logged in as: mateus', got %q", enConnected)
	}

	missingKey := tr.T("pt-BR", "non_existing_key_xyz")
	if missingKey != "non_existing_key_xyz" {
		t.Errorf("expected key itself for missing translation, got %q", missingKey)
	}
}

func TestContextLanguage(t *testing.T) {
	ctx := context.Background()
	if got := i18n.GetLanguage(ctx); got != i18n.DefaultLang {
		t.Errorf("expected default lang %q, got %q", i18n.DefaultLang, got)
	}

	ctx = i18n.WithLanguage(ctx, i18n.LangEnUS)
	if got := i18n.GetLanguage(ctx); got != i18n.LangEnUS {
		t.Errorf("expected %q, got %q", i18n.LangEnUS, got)
	}
}
