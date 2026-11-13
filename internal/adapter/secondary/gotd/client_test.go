package gotd

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf16"

	"github.com/gotd/td/session"
	"github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/auth"
	"github.com/gotd/td/tg"
	"go.uber.org/goleak"

	"github.com/matrocheetos/telegram-monitor/internal/adapter/secondary/crypto"
	"github.com/matrocheetos/telegram-monitor/internal/adapter/secondary/sqlite"
	"github.com/matrocheetos/telegram-monitor/internal/domain"
	"github.com/matrocheetos/telegram-monitor/internal/port"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}

type mockAuthClient struct {
	runFn      func(ctx context.Context, f func(ctx context.Context) error) error
	sendCodeFn func(ctx context.Context, req *tg.AuthSendCodeRequest) (tg.AuthSentCodeClass, error)
	signInFn   func(ctx context.Context, phone, code, codeHash string) (*tg.AuthAuthorization, error)
	passwordFn func(ctx context.Context, password string) (*tg.AuthAuthorization, error)
	dumpFn     func() ([]byte, error)
}

func (m *mockAuthClient) Run(ctx context.Context, f func(ctx context.Context) error) error {
	if m.runFn != nil {
		return m.runFn(ctx, f)
	}
	return f(ctx)
}

func (m *mockAuthClient) SendCode(ctx context.Context, req *tg.AuthSendCodeRequest) (tg.AuthSentCodeClass, error) {
	return m.sendCodeFn(ctx, req)
}

func (m *mockAuthClient) SignIn(ctx context.Context, phone, code, codeHash string) (*tg.AuthAuthorization, error) {
	return m.signInFn(ctx, phone, code, codeHash)
}

func (m *mockAuthClient) Password(ctx context.Context, password string) (*tg.AuthAuthorization, error) {
	return m.passwordFn(ctx, password)
}

func (m *mockAuthClient) DumpSession() ([]byte, error) {
	if m.dumpFn != nil {
		return m.dumpFn()
	}
	return []byte("mock-session"), nil
}

type mockDialogsAPI struct {
	fn func(ctx context.Context, request *tg.MessagesGetDialogsRequest) (tg.MessagesDialogsClass, error)
}

func (m *mockDialogsAPI) MessagesGetDialogs(ctx context.Context, request *tg.MessagesGetDialogsRequest) (tg.MessagesDialogsClass, error) {
	return m.fn(ctx, request)
}

type mockListenerClient struct {
	runFn func(ctx context.Context, f func(ctx context.Context) error) error
	api   dialogsAPI
}

func (m *mockListenerClient) Run(ctx context.Context, f func(ctx context.Context) error) error {
	if m.runFn != nil {
		return m.runFn(ctx, f)
	}
	return f(ctx)
}

func (m *mockListenerClient) API() dialogsAPI {
	return m.api
}

func newTestDeps(t *testing.T) (port.CredentialRepository, port.CryptoService) {
	t.Helper()
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
	return repo, cryptoSvc
}

func saveTestCredentials(t *testing.T, repo port.CredentialRepository, cryptoSvc port.CryptoService) {
	t.Helper()
	encAPIID, err := cryptoSvc.Encrypt([]byte("12345"))
	if err != nil {
		t.Fatalf("encrypt api id: %v", err)
	}
	encAPIHash, err := cryptoSvc.Encrypt([]byte("test-api-hash"))
	if err != nil {
		t.Fatalf("encrypt api hash: %v", err)
	}
	encSession, err := cryptoSvc.Encrypt([]byte("test-session-blob"))
	if err != nil {
		t.Fatalf("encrypt session: %v", err)
	}
	err = repo.SaveTelegramCredentials(context.Background(), &port.TelegramCredentials{
		ID:               1,
		EncryptedAPIID:   encAPIID,
		EncryptedAPIHash: encAPIHash,
		EncryptedSession: encSession,
		PhoneMasked:      "+5511 *****-1234",
		UpdatedAt:        time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("save credentials: %v", err)
	}
}

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
		actual := MaskPhone(tc.input)
		if actual != tc.expected {
			t.Errorf("MaskPhone(%q) = %q, expected %q", tc.input, actual, tc.expected)
		}
	}
}

func TestClientManager_LifecycleAndValidation(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repo, cryptoSvc := newTestDeps(t)
	mgr := NewClientManager(repo, cryptoSvc, nil)

	t.Run("IsConnected initially false", func(t *testing.T) {
		if mgr.IsConnected() {
			t.Errorf("expected IsConnected to be false initially")
		}
	})

	t.Run("StartListener without credentials", func(t *testing.T) {
		err := mgr.StartListener(ctx)
		if !errors.Is(err, ErrCredentialsNotConfig) {
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
		if !errors.Is(err, ErrNoPendingLogin) {
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
		if !errors.Is(err, ErrNotConnected) {
			t.Errorf("expected ErrNotConnected, got %v", err)
		}
	})
}

func TestClientManager_SendCodeCancellation(t *testing.T) {
	t.Parallel()

	repo, cryptoSvc := newTestDeps(t)
	mgr := NewClientManager(repo, cryptoSvc, nil)

	started := make(chan struct{})
	mgr.newAuthClient = func(apiID int, apiHash string) authClient {
		return &mockAuthClient{
			sendCodeFn: func(ctx context.Context, req *tg.AuthSendCodeRequest) (tg.AuthSentCodeClass, error) {
				close(started)
				<-ctx.Done()
				return nil, ctx.Err()
			},
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-started
		cancel()
	}()

	_, err := mgr.SendCode(ctx, 12345, "api-hash", "+5511999991234")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}

	mgr.authMu.Lock()
	pending := mgr.pending
	mgr.authMu.Unlock()
	if pending != nil {
		t.Fatalf("expected m.pending to be cleared after SendCode cancellation")
	}
}

func TestClientManager_SignIn2FAFlow(t *testing.T) {
	t.Parallel()

	repo, cryptoSvc := newTestDeps(t)
	mgr := NewClientManager(repo, cryptoSvc, nil)
	t.Cleanup(func() { _ = mgr.StopListener(context.Background()) })

	var passwordCalls atomic.Int32
	mgr.newAuthClient = func(apiID int, apiHash string) authClient {
		return &mockAuthClient{
			sendCodeFn: func(ctx context.Context, req *tg.AuthSendCodeRequest) (tg.AuthSentCodeClass, error) {
				return &tg.AuthSentCode{PhoneCodeHash: "hash-2fa"}, nil
			},
			signInFn: func(ctx context.Context, phone, code, codeHash string) (*tg.AuthAuthorization, error) {
				if code != "99999" || codeHash != "hash-2fa" {
					return nil, errors.New("unexpected code or hash")
				}
				return nil, auth.ErrPasswordAuthNeeded
			},
			passwordFn: func(ctx context.Context, password string) (*tg.AuthAuthorization, error) {
				passwordCalls.Add(1)
				if password != "my-secret-2fa" {
					return nil, errors.New("invalid password")
				}
				return &tg.AuthAuthorization{}, nil
			},
			dumpFn: func() ([]byte, error) {
				return []byte("authenticated-2fa-session"), nil
			},
		}
	}
	mgr.newListenerClient = func(apiID int, apiHash string, sessionStorage session.Storage, handler telegram.UpdateHandler) listenerClient {
		return &mockListenerClient{api: &mockDialogsAPI{}}
	}

	ctx := context.Background()
	hash, err := mgr.SendCode(ctx, 12345, "api-hash", "+5511999991234")
	if err != nil {
		t.Fatalf("SendCode failed: %v", err)
	}
	if hash != "hash-2fa" {
		t.Fatalf("expected hash-2fa, got %q", hash)
	}

	_, err = mgr.SignIn(ctx, "99999", "")
	if !errors.Is(err, domain.ErrTelegram2FARequired) {
		t.Fatalf("expected ErrTelegram2FARequired, got %v", err)
	}

	mgr.authMu.Lock()
	stillPending := mgr.pending != nil
	mgr.authMu.Unlock()
	if !stillPending {
		t.Fatalf("expected pending auth session to remain alive for 2FA retry")
	}

	_, err = mgr.SignIn(ctx, "99999", "")
	if !errors.Is(err, domain.ErrTelegram2FARequired) {
		t.Fatalf("expected ErrTelegram2FARequired on second empty password attempt, got %v", err)
	}

	sessionData, err := mgr.SignIn(ctx, "99999", "my-secret-2fa")
	if err != nil {
		t.Fatalf("SignIn with 2FA password failed: %v", err)
	}
	if string(sessionData) != "authenticated-2fa-session" {
		t.Fatalf("unexpected session data: %q", string(sessionData))
	}
	if passwordCalls.Load() != 1 {
		t.Fatalf("expected Password to be called once, got %d", passwordCalls.Load())
	}

	mgr.authMu.Lock()
	afterPending := mgr.pending
	mgr.authMu.Unlock()
	if afterPending != nil {
		t.Fatalf("expected m.pending to be cleared after successful 2FA login")
	}
	if !mgr.IsConnected() {
		t.Fatalf("expected listener to be connected after SignIn")
	}
}

func TestClientManager_StartAndStopListenerLifecycle(t *testing.T) {
	t.Parallel()

	repo, cryptoSvc := newTestDeps(t)
	saveTestCredentials(t, repo, cryptoSvc)

	mgr := NewClientManager(repo, cryptoSvc, nil)
	mgr.newListenerClient = func(apiID int, apiHash string, sessionStorage session.Storage, handler telegram.UpdateHandler) listenerClient {
		return &mockListenerClient{
			api: &mockDialogsAPI{
				fn: func(ctx context.Context, request *tg.MessagesGetDialogsRequest) (tg.MessagesDialogsClass, error) {
					return &tg.MessagesDialogs{
						Chats: []tg.ChatClass{
							&tg.Channel{ID: 101, Title: "Deals Channel", Username: "deals"},
						},
					}, nil
				},
			},
		}
	}

	ctx := context.Background()
	if err := mgr.StartListener(ctx); err != nil {
		t.Fatalf("StartListener failed: %v", err)
	}

	if !mgr.IsConnected() {
		t.Fatalf("expected IsConnected to be true immediately after StartListener returns")
	}

	channels, err := mgr.SyncDialogs(ctx)
	if err != nil {
		t.Fatalf("SyncDialogs immediately after StartListener failed: %v", err)
	}
	if len(channels) != 1 || channels[0].ChatID != -1000000000101 {
		t.Fatalf("unexpected channels: %+v", channels)
	}

	if err := mgr.StartListener(ctx); err != nil {
		t.Fatalf("idempotent StartListener failed: %v", err)
	}

	var wg sync.WaitGroup
	for range 3 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := mgr.StopListener(ctx); err != nil {
				t.Errorf("concurrent StopListener failed: %v", err)
			}
		}()
	}
	wg.Wait()

	if mgr.IsConnected() {
		t.Fatalf("expected IsConnected to be false after StopListener")
	}
}

func TestSyncDialogs_PaginationAndDeduplication(t *testing.T) {
	t.Parallel()

	repo, cryptoSvc := newTestDeps(t)
	saveTestCredentials(t, repo, cryptoSvc)

	mgr := NewClientManager(repo, cryptoSvc, nil)
	t.Cleanup(func() { _ = mgr.StopListener(context.Background()) })

	var pageCalls int
	mgr.newListenerClient = func(apiID int, apiHash string, sessionStorage session.Storage, handler telegram.UpdateHandler) listenerClient {
		return &mockListenerClient{
			api: &mockDialogsAPI{
				fn: func(ctx context.Context, req *tg.MessagesGetDialogsRequest) (tg.MessagesDialogsClass, error) {
					pageCalls++
					switch pageCalls {
					case 1:
						if req.OffsetDate != 0 || req.OffsetID != 0 {
							t.Fatalf("expected zero initial offsets, got date=%d id=%d", req.OffsetDate, req.OffsetID)
						}
						dialogs := make([]tg.DialogClass, dialogPageLimit)
						for i := range dialogPageLimit {
							dialogs[i] = &tg.Dialog{
								Peer:       &tg.PeerChannel{ChannelID: int64(i + 1)},
								TopMessage: 1000 + i,
							}
						}
						return &tg.MessagesDialogsSlice{
							Count:   205,
							Dialogs: dialogs,
							Messages: []tg.MessageClass{
								&tg.Message{
									ID:     1000 + dialogPageLimit - 1,
									Date:   1700000100,
									PeerID: &tg.PeerChannel{ChannelID: int64(dialogPageLimit)},
								},
							},
							Chats: []tg.ChatClass{
								&tg.Channel{ID: 1, AccessHash: 11, Title: "Channel One", Username: "one"},
								&tg.Channel{ID: int64(dialogPageLimit), AccessHash: 999, Title: "Channel Hundred"},
								&tg.Channel{ID: 2, AccessHash: 22, Title: "Left Channel", Left: true},
							},
						}, nil
					case 2:
						peerCh, ok := req.OffsetPeer.(*tg.InputPeerChannel)
						if !ok || peerCh.ChannelID != int64(dialogPageLimit) || peerCh.AccessHash != 999 {
							t.Fatalf("unexpected page 2 offset peer: %#v", req.OffsetPeer)
						}
						if req.OffsetDate != 1700000100 || req.OffsetID != 1000+dialogPageLimit-1 {
							t.Fatalf("unexpected page 2 offsets: date=%d id=%d", req.OffsetDate, req.OffsetID)
						}
						dialogs := make([]tg.DialogClass, dialogPageLimit)
						for i := range dialogPageLimit {
							dialogs[i] = &tg.Dialog{
								Peer:       &tg.PeerUser{UserID: int64(500 + i)},
								TopMessage: 2000 + i,
							}
						}
						return &tg.MessagesDialogsSlice{
							Count:   205,
							Dialogs: dialogs,
							Messages: []tg.MessageClass{
								&tg.Message{
									ID:     2000 + dialogPageLimit - 1,
									Date:   1700000050,
									PeerID: &tg.PeerUser{UserID: int64(500 + dialogPageLimit - 1)},
								},
							},
							Chats: []tg.ChatClass{
								&tg.Channel{ID: 1, AccessHash: 11, Title: "Channel One Duplicate"},
								&tg.Chat{ID: 55, Title: "Basic Group"},
							},
							Users: []tg.UserClass{
								&tg.User{ID: int64(500 + dialogPageLimit - 1), AccessHash: 777},
							},
						}, nil
					case 3:
						peerUsr, ok := req.OffsetPeer.(*tg.InputPeerUser)
						if !ok || peerUsr.UserID != int64(500+dialogPageLimit-1) || peerUsr.AccessHash != 777 {
							t.Fatalf("unexpected page 3 offset peer: %#v", req.OffsetPeer)
						}
						return &tg.MessagesDialogs{
							Dialogs: []tg.DialogClass{
								&tg.Dialog{Peer: &tg.PeerChat{ChatID: 88}, TopMessage: 3001},
							},
							Chats: []tg.ChatClass{
								&tg.Chat{ID: 88, Title: "Final Group"},
								&tg.Chat{ID: 89, Title: "Deactivated Group", Deactivated: true},
							},
						}, nil
					default:
						t.Fatalf("unexpected extra GetDialogs call #%d", pageCalls)
						return nil, nil
					}
				},
			},
		}
	}

	ctx := context.Background()
	if err := mgr.StartListener(ctx); err != nil {
		t.Fatalf("StartListener failed: %v", err)
	}

	channels, err := mgr.SyncDialogs(ctx)
	if err != nil {
		t.Fatalf("SyncDialogs failed: %v", err)
	}
	if pageCalls != 3 {
		t.Fatalf("expected 3 dialog pages, got %d", pageCalls)
	}
	if len(channels) != 4 {
		t.Fatalf("expected 4 unique active channels/groups, got %d: %+v", len(channels), channels)
	}
}

func TestExtractDialogsPage_Deduplication(t *testing.T) {
	t.Parallel()

	seen := make(map[int64]struct{})
	page1 := extractDialogsPage([]tg.ChatClass{
		&tg.Channel{ID: 10, AccessHash: 1, Title: "Ch 10"},
		&tg.Channel{ID: 10, AccessHash: 1, Title: "Ch 10 Dup"},
		&tg.Chat{ID: 20, Title: "Group 20"},
		&tg.Chat{ID: 21, Title: "Left Group", Left: true},
	}, seen)

	if len(page1) != 2 {
		t.Fatalf("expected 2 channels from page1, got %d", len(page1))
	}

	page2 := extractDialogsPage([]tg.ChatClass{
		&tg.Channel{ID: 10, AccessHash: 1, Title: "Ch 10 Again"},
		&tg.Chat{ID: 22, Title: "Group 22"},
	}, seen)

	if len(page2) != 1 || page2[0].ChatID != -22 {
		t.Fatalf("expected 1 new channel (-22) from page2, got %+v", page2)
	}
}

func TestSafeUTF16Substring(t *testing.T) {
	t.Parallel()

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
		actual := SafeUTF16Substring(u16, tc.offset, tc.length)
		if actual != tc.expected {
			t.Errorf("%s: got %q, want %q", tc.name, actual, tc.expected)
		}
	}
}

func TestExtractTelegramMedia(t *testing.T) {
	t.Parallel()

	hasMedia, mediaType, pageURL := ExtractTelegramMedia(nil)
	if hasMedia || mediaType != "" || pageURL != "" {
		t.Fatalf("expected nil media to be empty")
	}

	hasMedia, mediaType, pageURL = ExtractTelegramMedia(&tg.MessageMediaEmpty{})
	if hasMedia || mediaType != "" || pageURL != "" {
		t.Fatalf("expected MessageMediaEmpty to be empty")
	}

	hasMedia, mediaType, pageURL = ExtractTelegramMedia(&tg.MessageMediaPhoto{})
	if !hasMedia || mediaType != "photo" || pageURL != "" {
		t.Fatalf("expected photo media, got hasMedia=%v, type=%q, pageURL=%q", hasMedia, mediaType, pageURL)
	}

	hasMedia, mediaType, pageURL = ExtractTelegramMedia(&tg.MessageMediaDocument{})
	if !hasMedia || mediaType != "document" || pageURL != "" {
		t.Fatalf("expected document media, got hasMedia=%v, type=%q, pageURL=%q", hasMedia, mediaType, pageURL)
	}

	webpage := &tg.MessageMediaWebPage{
		Webpage: &tg.WebPage{
			URL: "https://amzn.to/example",
		},
	}
	hasMedia, mediaType, pageURL = ExtractTelegramMedia(webpage)
	if !hasMedia || mediaType != "webpage" || pageURL != "https://amzn.to/example" {
		t.Fatalf("expected webpage media with URL, got hasMedia=%v, type=%q, url=%q", hasMedia, mediaType, pageURL)
	}
}

func TestExtractTelegramEntities(t *testing.T) {
	t.Parallel()

	text := "🔥 Link da Loja e Cupom 10OFF aqui"
	u16 := utf16.Encode([]rune(text))

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
			Length: 2,
		},
	}

	excerpts := ExtractTelegramEntities(text, entities)
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

	if btns := ExtractTelegramInlineButtons(nil); btns != nil {
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

	buttons := ExtractTelegramInlineButtons(markup)
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

	incoming := BuildIncomingTelegramMessage(-1001234567890, rawMsg)
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
