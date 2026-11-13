package gotd_test

import (
	"context"
	"errors"
	"testing"
	"time"
	"unicode/utf16"

	"github.com/gotd/td/tg"
	"github.com/matrocheetos/telegram-monitor/internal/adapter/secondary/crypto"
	"github.com/matrocheetos/telegram-monitor/internal/adapter/secondary/gotd"
	"github.com/matrocheetos/telegram-monitor/internal/adapter/secondary/sqlite"
)

func TestMaskPhone(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input    string
		expected string
	}{
		{"+5511999991234", "+5511 *****-1234"},
		{"+12345678901", "+1234 *****-8901"},
		{"12345", "***"},
		{"", "***"},
	}

	for _, tc := range tests {
		actual := gotd.MaskPhone(tc.input)
		if actual != tc.expected {
			t.Errorf("MaskPhone(%q) = %q, expected %q", tc.input, actual, tc.expected)
		}
	}
}

func TestClientManager_LifecycleAndValidation(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := sqlite.OpenDB(ctx, ":memory:")
	if err != nil {
		t.Fatalf("OpenDB failed: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	repo := sqlite.NewCredentialRepository(db)
	cryptoSvc, err := crypto.NewAESGCMCrypto("secret-encryption-key-for-tests-12345")
	if err != nil {
		t.Fatalf("NewAESGCMCrypto failed: %v", err)
	}

	mgr := gotd.NewClientManager(repo, cryptoSvc, nil)

	t.Run("IsConnected initially false", func(t *testing.T) {
		if mgr.IsConnected() {
			t.Errorf("expected IsConnected to be false initially")
		}
	})

	t.Run("StartListener without credentials", func(t *testing.T) {
		err := mgr.StartListener(ctx)
		if !errors.Is(err, gotd.ErrCredentialsNotConfig) {
			t.Errorf("expected ErrCredentialsNotConfig, got %v", err)
		}
	})

	t.Run("StopListener when not running", func(t *testing.T) {
		err := mgr.StopListener(ctx)
		if err != nil {
			t.Errorf("expected nil error stopping inactive listener, got %v", err)
		}
	})

	t.Run("SignIn without pending login", func(t *testing.T) {
		_, err := mgr.SignIn(ctx, "12345", "")
		if !errors.Is(err, gotd.ErrNoPendingLogin) {
			t.Errorf("expected ErrNoPendingLogin, got %v", err)
		}
	})

	t.Run("SendCode validation", func(t *testing.T) {
		_, err := mgr.SendCode(ctx, 0, "hash", "+5511999991234")
		if err == nil {
			t.Errorf("expected error for non-positive apiID")
		}

		_, err = mgr.SendCode(ctx, 12345, "", "+5511999991234")
		if err == nil {
			t.Errorf("expected error for empty apiHash")
		}

		_, err = mgr.SendCode(ctx, 12345, "hash", "")
		if err == nil {
			t.Errorf("expected error for empty phone")
		}
	})

	t.Run("SyncDialogs when not connected", func(t *testing.T) {
		_, err := mgr.SyncDialogs(ctx)
		if !errors.Is(err, gotd.ErrNotConnected) {
			t.Errorf("expected ErrNotConnected, got %v", err)
		}
	})
}

func TestSafeUTF16Substring(t *testing.T) {
	t.Parallel()

	// Text with emoji (🎟 has surrogate pair in UTF-16, length 2) and Portuguese accents
	text := "🎟️ Promoção: Cupom de 20% OFF"
	u16 := utf16.Encode([]rune(text))

	tests := []struct {
		name     string
		offset   int
		length   int
		expected string
	}{
		{
			name:     "out of bounds negative offset",
			offset:   -1,
			length:   5,
			expected: "",
		},
		{
			name:     "out of bounds offset beyond length",
			offset:   100,
			length:   5,
			expected: "",
		},
		{
			name:     "zero length",
			offset:   5,
			length:   0,
			expected: "",
		},
		{
			name:     "negative length",
			offset:   5,
			length:   -2,
			expected: "",
		},
		{
			name:     "length exceeds total size clamps safely",
			offset:   len(u16) - 3,
			length:   50,
			expected: string(utf16.Decode(u16[len(u16)-3:])),
		},
	}

	for _, tc := range tests {
		actual := gotd.SafeUTF16Substring(u16, tc.offset, tc.length)
		if actual != tc.expected {
			t.Errorf("%s: got %q, want %q", tc.name, actual, tc.expected)
		}
	}
}

func TestExtractTelegramMedia(t *testing.T) {
	t.Parallel()

	hasMedia, mediaType, pageURL := gotd.ExtractTelegramMedia(nil)
	if hasMedia || mediaType != "" || pageURL != "" {
		t.Fatalf("expected nil media to be empty")
	}

	hasMedia, mediaType, pageURL = gotd.ExtractTelegramMedia(&tg.MessageMediaEmpty{})
	if hasMedia || mediaType != "" || pageURL != "" {
		t.Fatalf("expected MessageMediaEmpty to be empty")
	}

	hasMedia, mediaType, pageURL = gotd.ExtractTelegramMedia(&tg.MessageMediaPhoto{})
	if !hasMedia || mediaType != "photo" || pageURL != "" {
		t.Fatalf("expected photo media, got hasMedia=%v, type=%q, pageURL=%q", hasMedia, mediaType, pageURL)
	}

	hasMedia, mediaType, pageURL = gotd.ExtractTelegramMedia(&tg.MessageMediaDocument{})
	if !hasMedia || mediaType != "document" || pageURL != "" {
		t.Fatalf("expected document media, got hasMedia=%v, type=%q, pageURL=%q", hasMedia, mediaType, pageURL)
	}

	webpage := &tg.MessageMediaWebPage{
		Webpage: &tg.WebPage{
			URL: "https://amzn.to/example",
		},
	}
	hasMedia, mediaType, pageURL = gotd.ExtractTelegramMedia(webpage)
	if !hasMedia || mediaType != "webpage" || pageURL != "https://amzn.to/example" {
		t.Fatalf("expected webpage media with URL, got hasMedia=%v, type=%q, url=%q", hasMedia, mediaType, pageURL)
	}
}

func TestExtractTelegramEntities(t *testing.T) {
	t.Parallel()

	// Text: "🔥 Link da Loja e Cupom 10OFF aqui"
	// Let's compute offsets
	text := "🔥 Link da Loja e Cupom 10OFF aqui"
	u16 := utf16.Encode([]rune(text))

	// Find offset of "Link da Loja"
	linkTarget := "Link da Loja"
	u16Link := utf16.Encode([]rune(linkTarget))
	var linkOffset int
	for i := 0; i+len(u16Link) <= len(u16); i++ {
		match := true
		for j := range u16Link {
			if u16[i+j] != u16Link[j] {
				match = false
				break
			}
		}
		if match {
			linkOffset = i
			break
		}
	}

	// Find offset of "10OFF"
	codeTarget := "10OFF"
	u16Code := utf16.Encode([]rune(codeTarget))
	var codeOffset int
	for i := 0; i+len(u16Code) <= len(u16); i++ {
		match := true
		for j := range u16Code {
			if u16[i+j] != u16Code[j] {
				match = false
				break
			}
		}
		if match {
			codeOffset = i
			break
		}
	}

	entities := []tg.MessageEntityClass{
		&tg.MessageEntityTextURL{
			Offset: linkOffset,
			Length: len(u16Link),
			URL:    "https://store.com/deal",
		},
		&tg.MessageEntityCode{
			Offset: codeOffset,
			Length: len(u16Code),
		},
		&tg.MessageEntityStrike{
			Offset: 0,
			Length: 2, // The emoji 🔥
		},
	}

	excerpts := gotd.ExtractTelegramEntities(text, entities)
	if len(excerpts) != 3 {
		t.Fatalf("expected 3 excerpts, got %d", len(excerpts))
	}

	if excerpts[0].Type != "text_url" || excerpts[0].Text != linkTarget || excerpts[0].URL != "https://store.com/deal" {
		t.Errorf("excerpt 0 mismatch: %+v", excerpts[0])
	}

	if excerpts[1].Type != "code" || excerpts[1].Text != codeTarget {
		t.Errorf("excerpt 1 mismatch: %+v", excerpts[1])
	}

	if excerpts[2].Type != "strike" || excerpts[2].Text != "🔥" {
		t.Errorf("excerpt 2 mismatch: %+v", excerpts[2])
	}
}

func TestExtractTelegramInlineButtons(t *testing.T) {
	t.Parallel()

	if btns := gotd.ExtractTelegramInlineButtons(nil); btns != nil {
		t.Fatalf("expected nil buttons for nil markup")
	}

	markup := &tg.ReplyInlineMarkup{
		Rows: []tg.KeyboardInlineButtonRow{
			{
				Buttons: []tg.KeyboardInlineButton{
					{
						Text: "Comprar na Loja",
						Type: &tg.InlineButtonTypeURL{
							URL: "https://amazon.com.br/dp/B001",
						},
					},
					{
						Text: "Autorizar Login",
						Type: &tg.InlineButtonTypeURLAuth{
							URL: "https://auth.store.com/login",
						},
					},
				},
			},
			{
				Buttons: []tg.KeyboardInlineButton{
					{
						Text: "Callback Sem URL",
						Type: &tg.InlineButtonTypeCallback{},
					},
					{
						Text: "Ver Webview",
						Type: &tg.InlineButtonTypeWebView{
							URL: "https://store.com/view",
						},
					},
				},
			},
		},
	}

	buttons := gotd.ExtractTelegramInlineButtons(markup)
	if len(buttons) != 3 {
		t.Fatalf("expected 3 url buttons, got %d", len(buttons))
	}

	if buttons[0].Text != "Comprar na Loja" || buttons[0].URL != "https://amazon.com.br/dp/B001" {
		t.Errorf("button 0 mismatch: %+v", buttons[0])
	}
	if buttons[1].Text != "Autorizar Login" || buttons[1].URL != "https://auth.store.com/login" {
		t.Errorf("button 1 mismatch: %+v", buttons[1])
	}
	if buttons[2].Text != "Ver Webview" || buttons[2].URL != "https://store.com/view" {
		t.Errorf("button 2 mismatch: %+v", buttons[2])
	}
}

func TestBuildIncomingTelegramMessage(t *testing.T) {
	t.Parallel()

	rawMsg := &tg.Message{
		ID:      4242,
		Date:    int(time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC).Unix()),
		Message: "Promoção Especial! Use o cupom TOP10",
		Media: &tg.MessageMediaPhoto{
			Photo: &tg.Photo{},
		},
		Entities: []tg.MessageEntityClass{
			&tg.MessageEntityCode{
				Offset: 31,
				Length: 5,
			},
		},
		ReplyMarkup: &tg.ReplyInlineMarkup{
			Rows: []tg.KeyboardInlineButtonRow{
				{
					Buttons: []tg.KeyboardInlineButton{
						{
							Text: "Ir para Loja",
							Type: &tg.InlineButtonTypeURL{
								URL: "https://magalu.com",
							},
						},
					},
				},
			},
		},
	}

	incoming := gotd.BuildIncomingTelegramMessage(-1001234567890, rawMsg)
	if incoming.ChannelID != -1001234567890 {
		t.Errorf("expected ChannelID -1001234567890, got %d", incoming.ChannelID)
	}
	if incoming.TelegramMessageID != 4242 {
		t.Errorf("expected TelegramMessageID 4242, got %d", incoming.TelegramMessageID)
	}
	if !incoming.HasMedia || incoming.MediaType != "photo" {
		t.Errorf("expected photo media, got hasMedia=%v, type=%q", incoming.HasMedia, incoming.MediaType)
	}
	if len(incoming.Entities) != 1 || incoming.Entities[0].Text != "TOP10" {
		t.Errorf("expected TOP10 entity code, got %+v", incoming.Entities)
	}
	if len(incoming.InlineButtons) != 1 || incoming.InlineButtons[0].URL != "https://magalu.com" {
		t.Errorf("expected magalu button, got %+v", incoming.InlineButtons)
	}
}

