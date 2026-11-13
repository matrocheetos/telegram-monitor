package gotd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf16"

	"github.com/gotd/td/session"
	"github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/auth"
	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"

	"github.com/matrocheetos/telegram-monitor/internal/domain"
	"github.com/matrocheetos/telegram-monitor/internal/port"
)

var (
	ErrNoPendingLogin       = errors.New("telegram: no pending web login")
	ErrAlreadyRunning       = errors.New("telegram: listener is already running")
	ErrNotConnected         = errors.New("telegram: client is not connected")
	ErrCredentialsNotConfig = errors.New("telegram: credentials not configured")
)

type verifyRequest struct {
	code        string
	password2FA string
}

type verifyResponse struct {
	sessionData []byte
	err         error
}

type pendingAuth struct {
	apiID         int
	apiHash       string
	phone         string
	phoneCodeHash string
	verifyReqCh   chan verifyRequest
	verifyResCh   chan verifyResponse
	cancel        context.CancelFunc
}

type ClientManager struct {
	repo      port.CredentialRepository
	crypto    port.CryptoService
	processor port.MessageProcessor

	authMu  sync.Mutex
	pending *pendingAuth

	listenerMu     sync.Mutex
	running        bool
	connected      atomic.Bool
	listenerCtx    context.Context
	listenerCancel context.CancelFunc
	listenerClient *telegram.Client
	listenerAPI    *tg.Client
}

var _ port.TelegramClientPort = (*ClientManager)(nil)

func NewClientManager(
	repo port.CredentialRepository,
	crypto port.CryptoService,
	processor port.MessageProcessor,
) *ClientManager {
	return &ClientManager{
		repo:      repo,
		crypto:    crypto,
		processor: processor,
	}
}

func MaskPhone(phone string) string {
	clean := strings.TrimSpace(phone)
	if len(clean) < 8 {
		return "***"
	}
	prefix := clean[:5]
	suffix := clean[len(clean)-4:]
	return prefix + " *****-" + suffix
}

func (m *ClientManager) IsConnected() bool {
	return m.connected.Load()
}

func (m *ClientManager) SendCode(ctx context.Context, apiID int, apiHash, phone string) (string, error) {
	if apiID <= 0 {
		return "", errors.New("telegram: apiID must be positive")
	}
	apiHash = strings.TrimSpace(apiHash)
	if apiHash == "" {
		return "", errors.New("telegram: apiHash is required")
	}
	phone = strings.TrimSpace(phone)
	if phone == "" {
		return "", errors.New("telegram: phone is required")
	}

	m.authMu.Lock()
	if m.pending != nil {
		m.pending.cancel()
		m.pending = nil
	}

	authCtx, authCancel := context.WithTimeout(context.Background(), 5*time.Minute)

	codeSentCh := make(chan string, 1)
	codeErrCh := make(chan error, 1)
	verifyReqCh := make(chan verifyRequest, 1)
	verifyResCh := make(chan verifyResponse, 1)

	pending := &pendingAuth{
		apiID:       apiID,
		apiHash:     apiHash,
		phone:       phone,
		verifyReqCh: verifyReqCh,
		verifyResCh: verifyResCh,
		cancel:      authCancel,
	}
	m.pending = pending
	m.authMu.Unlock()

	memStorage := &session.StorageMemory{}
	tempClient := telegram.NewClient(apiID, apiHash, telegram.Options{
		SessionStorage: memStorage,
	})

	go func() {
		defer authCancel()

		_ = tempClient.Run(authCtx, func(runCtx context.Context) error {
			api := tempClient.API()
			res, err := api.AuthSendCode(runCtx, &tg.AuthSendCodeRequest{
				PhoneNumber: phone,
				APIID:       apiID,
				APIHash:     apiHash,
				Settings:    tg.CodeSettings{},
			})
			if err != nil {
				codeErrCh <- err
				return err
			}

			sentCode, ok := res.(*tg.AuthSentCode)
			if !ok {
				typeErr := fmt.Errorf("unexpected send code response type: %T", res)
				codeErrCh <- typeErr
				return typeErr
			}

			phoneCodeHash := sentCode.PhoneCodeHash
			codeSentCh <- phoneCodeHash

			select {
			case <-runCtx.Done():
				return runCtx.Err()
			case req := <-verifyReqCh:
				_, signErr := tempClient.Auth().SignIn(runCtx, phone, req.code, phoneCodeHash)
				if signErr != nil {
					if errors.Is(signErr, auth.ErrPasswordAuthNeeded) || tgerr.Is(signErr, "SESSION_PASSWORD_NEEDED") {
						if req.password2FA == "" {
							verifyResCh <- verifyResponse{err: auth.ErrPasswordAuthNeeded}
							return signErr
						}
						_, pwdErr := tempClient.Auth().Password(runCtx, req.password2FA)
						if pwdErr != nil {
							verifyResCh <- verifyResponse{err: pwdErr}
							return pwdErr
						}
					} else {
						verifyResCh <- verifyResponse{err: signErr}
						return signErr
					}
				}

				var buf bytes.Buffer
				_ = memStorage.Dump(&buf)
				verifyResCh <- verifyResponse{sessionData: buf.Bytes()}
				return nil
			}
		})
	}()

	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case err := <-codeErrCh:
		m.authMu.Lock()
		m.pending = nil
		m.authMu.Unlock()
		return "", fmt.Errorf("telegram: send code failed: %w", err)
	case hash := <-codeSentCh:
		m.authMu.Lock()
		if m.pending != nil {
			m.pending.phoneCodeHash = hash
		}
		m.authMu.Unlock()
		return hash, nil
	}
}

func (m *ClientManager) SignIn(ctx context.Context, code, password2FA string) ([]byte, error) {
	code = strings.TrimSpace(code)
	if code == "" {
		return nil, errors.New("telegram: code is required")
	}

	m.authMu.Lock()
	pending := m.pending
	m.authMu.Unlock()

	if pending == nil {
		return nil, ErrNoPendingLogin
	}

	select {
	case pending.verifyReqCh <- verifyRequest{code: code, password2FA: strings.TrimSpace(password2FA)}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case res := <-pending.verifyResCh:
		if res.err != nil {
			return nil, res.err
		}

		encAPIID, err := m.crypto.Encrypt([]byte(strconv.Itoa(pending.apiID)))
		if err != nil {
			return nil, fmt.Errorf("encrypt api id: %w", err)
		}

		encAPIHash, err := m.crypto.Encrypt([]byte(pending.apiHash))
		if err != nil {
			return nil, fmt.Errorf("encrypt api hash: %w", err)
		}

		encSession, err := m.crypto.Encrypt(res.sessionData)
		if err != nil {
			return nil, fmt.Errorf("encrypt session: %w", err)
		}

		creds := &port.TelegramCredentials{
			ID:               1,
			EncryptedAPIID:   encAPIID,
			EncryptedAPIHash: encAPIHash,
			EncryptedSession: encSession,
			PhoneMasked:      MaskPhone(pending.phone),
			UpdatedAt:        time.Now().UTC(),
		}

		if err := m.repo.SaveTelegramCredentials(ctx, creds); err != nil {
			return nil, fmt.Errorf("save credentials: %w", err)
		}

		m.authMu.Lock()
		m.pending = nil
		m.authMu.Unlock()

		_ = m.StartListener(context.Background())

		return res.sessionData, nil
	}
}

func (m *ClientManager) StartListener(ctx context.Context) error {
	m.listenerMu.Lock()
	defer m.listenerMu.Unlock()

	if m.running {
		return nil
	}

	creds, err := m.repo.GetTelegramCredentials(ctx)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return ErrCredentialsNotConfig
		}
		return fmt.Errorf("get telegram credentials: %w", err)
	}

	if len(creds.EncryptedSession) == 0 {
		return ErrCredentialsNotConfig
	}

	apiIDBytes, err := m.crypto.Decrypt(creds.EncryptedAPIID)
	if err != nil {
		return fmt.Errorf("decrypt api id: %w", err)
	}

	apiID, err := strconv.Atoi(string(apiIDBytes))
	if err != nil {
		return fmt.Errorf("parse api id: %w", err)
	}

	apiHashBytes, err := m.crypto.Decrypt(creds.EncryptedAPIHash)
	if err != nil {
		return fmt.Errorf("decrypt api hash: %w", err)
	}
	apiHash := string(apiHashBytes)

	dispatcher := tg.NewUpdateDispatcher()

	dispatcher.OnNewChannelMessage(func(ctx context.Context, entities tg.Entities, u *tg.UpdateNewChannelMessage) error {
		if m.processor == nil {
			return nil
		}
		msg, ok := u.Message.(*tg.Message)
		if !ok || msg.Out {
			return nil
		}

		var chatID int64
		if peer, ok := msg.PeerID.(*tg.PeerChannel); ok {
			chatID = -1000000000000 - peer.ChannelID
		}

		incoming := BuildIncomingTelegramMessage(chatID, msg)
		return m.processor.ProcessIncomingTelegramMessage(ctx, incoming)
	})

	dispatcher.OnNewMessage(func(ctx context.Context, entities tg.Entities, u *tg.UpdateNewMessage) error {
		if m.processor == nil {
			return nil
		}
		msg, ok := u.Message.(*tg.Message)
		if !ok || msg.Out {
			return nil
		}

		var chatID int64
		switch peer := msg.PeerID.(type) {
		case *tg.PeerChannel:
			chatID = -1000000000000 - peer.ChannelID
		case *tg.PeerChat:
			chatID = -peer.ChatID
		case *tg.PeerUser:
			chatID = peer.UserID
		}

		incoming := BuildIncomingTelegramMessage(chatID, msg)
		return m.processor.ProcessIncomingTelegramMessage(ctx, incoming)
	})

	sessionStorage := NewSessionStorage(m.repo, m.crypto)
	client := telegram.NewClient(apiID, apiHash, telegram.Options{
		SessionStorage: sessionStorage,
		UpdateHandler:  dispatcher,
	})

	listenerCtx, listenerCancel := context.WithCancel(context.Background())
	m.listenerCtx = listenerCtx
	m.listenerCancel = listenerCancel
	m.listenerClient = client
	m.running = true

	go func() {
		defer func() {
			m.connected.Store(false)
			m.listenerMu.Lock()
			m.running = false
			m.listenerMu.Unlock()
		}()

		_ = client.Run(listenerCtx, func(runCtx context.Context) error {
			m.connected.Store(true)
			m.listenerMu.Lock()
			m.listenerAPI = client.API()
			m.listenerMu.Unlock()

			<-runCtx.Done()
			return runCtx.Err()
		})
	}()

	return nil
}

func (m *ClientManager) StopListener(ctx context.Context) error {
	m.listenerMu.Lock()
	defer m.listenerMu.Unlock()

	if !m.running {
		return nil
	}

	if m.listenerCancel != nil {
		m.listenerCancel()
	}
	m.running = false
	m.connected.Store(false)
	return nil
}

func (m *ClientManager) SyncDialogs(ctx context.Context) ([]*domain.TelegramChannel, error) {
	m.listenerMu.Lock()
	api := m.listenerAPI
	isConnected := m.connected.Load()
	m.listenerMu.Unlock()

	if !isConnected || api == nil {
		return nil, ErrNotConnected
	}

	res, err := api.MessagesGetDialogs(ctx, &tg.MessagesGetDialogsRequest{
		OffsetPeer: &tg.InputPeerEmpty{},
		Limit:      100,
	})
	if err != nil {
		return nil, fmt.Errorf("telegram: get dialogs failed: %w", err)
	}

	var chats []tg.ChatClass
	switch d := res.(type) {
	case *tg.MessagesDialogs:
		chats = d.Chats
	case *tg.MessagesDialogsSlice:
		chats = d.Chats
	default:
		return nil, fmt.Errorf("unexpected dialogs type: %T", res)
	}

	var channels []*domain.TelegramChannel
	for _, c := range chats {
		switch ch := c.(type) {
		case *tg.Channel:
			if ch.Left {
				continue
			}
			username, _ := ch.GetUsername()
			accessHash, _ := ch.GetAccessHash()
			channels = append(channels, &domain.TelegramChannel{
				ChatID:     -1000000000000 - ch.ID,
				AccessHash: accessHash,
				Username:   username,
				Title:      ch.Title,
			})
		case *tg.Chat:
			if ch.Left || ch.Deactivated {
				continue
			}
			channels = append(channels, &domain.TelegramChannel{
				ChatID: -ch.ID,
				Title:  ch.Title,
			})
		}
	}

	return channels, nil
}

func SafeUTF16Substring(u16 []uint16, offset, length int) string {
	if offset < 0 || offset >= len(u16) || length <= 0 {
		return ""
	}
	end := offset + length
	if end > len(u16) {
		end = len(u16)
	}
	if end <= offset {
		return ""
	}
	return string(utf16.Decode(u16[offset:end]))
}

func ExtractTelegramMedia(media tg.MessageMediaClass) (hasMedia bool, mediaType string, webPageURL string) {
	if media == nil {
		return false, "", ""
	}
	switch m := media.(type) {
	case *tg.MessageMediaPhoto:
		return true, "photo", ""
	case *tg.MessageMediaWebPage:
		var pageURL string
		if wp, ok := m.Webpage.(*tg.WebPage); ok {
			pageURL = wp.URL
		}
		return true, "webpage", pageURL
	case *tg.MessageMediaDocument:
		return true, "document", ""
	case *tg.MessageMediaEmpty:
		return false, "", ""
	default:
		return true, "", ""
	}
}

func ExtractTelegramEntities(rawText string, entities []tg.MessageEntityClass) []domain.TelegramEntityExcerpt {
	if len(entities) == 0 {
		return nil
	}
	u16 := utf16.Encode([]rune(rawText))
	excerpts := make([]domain.TelegramEntityExcerpt, 0, len(entities))

	for _, ent := range entities {
		var (
			entityType string
			entityURL  string
			offset     int
			length     int
		)

		switch e := ent.(type) {
		case *tg.MessageEntityTextURL:
			entityType = "text_url"
			entityURL = e.URL
			offset = e.Offset
			length = e.Length
		case *tg.MessageEntityURL:
			entityType = "url"
			offset = e.Offset
			length = e.Length
		case *tg.MessageEntityCode:
			entityType = "code"
			offset = e.Offset
			length = e.Length
		case *tg.MessageEntityPre:
			entityType = "pre"
			offset = e.Offset
			length = e.Length
		case *tg.MessageEntityBold:
			entityType = "bold"
			offset = e.Offset
			length = e.Length
		case *tg.MessageEntityStrike:
			entityType = "strike"
			offset = e.Offset
			length = e.Length
		case *tg.MessageEntityBlockquote:
			entityType = "blockquote"
			offset = e.Offset
			length = e.Length
		default:
			continue
		}

		extractedText := SafeUTF16Substring(u16, offset, length)
		excerpts = append(excerpts, domain.TelegramEntityExcerpt{
			Type:   entityType,
			Offset: offset,
			Length: length,
			Text:   extractedText,
			URL:    entityURL,
		})
	}
	return excerpts
}

func ExtractTelegramInlineButtons(replyMarkup tg.ReplyMarkupClass) []domain.TelegramInlineButton {
	if replyMarkup == nil {
		return nil
	}
	rim, ok := replyMarkup.(*tg.ReplyInlineMarkup)
	if !ok || len(rim.Rows) == 0 {
		return nil
	}
	var buttons []domain.TelegramInlineButton
	for _, row := range rim.Rows {
		for _, btn := range row.Buttons {
			var btnURL string
			switch bt := btn.Type.(type) {
			case *tg.InlineButtonTypeURL:
				btnURL = bt.URL
			case *tg.InlineButtonTypeURLAuth:
				btnURL = bt.URL
			case *tg.InlineButtonTypeWebView:
				btnURL = bt.URL
			}
			if btnURL != "" {
				buttons = append(buttons, domain.TelegramInlineButton{
					Text: btn.Text,
					URL:  btnURL,
				})
			}
		}
	}
	return buttons
}

func BuildIncomingTelegramMessage(chatID int64, msg *tg.Message) domain.IncomingTelegramMessage {
	if msg == nil {
		return domain.IncomingTelegramMessage{ChannelID: chatID}
	}
	hasMedia, mediaType, webPageURL := ExtractTelegramMedia(msg.Media)
	entities := ExtractTelegramEntities(msg.Message, msg.Entities)
	buttons := ExtractTelegramInlineButtons(msg.ReplyMarkup)

	msgDate := time.Unix(int64(msg.Date), 0).UTC()

	return domain.IncomingTelegramMessage{
		ChannelID:         chatID,
		TelegramMessageID: msg.ID,
		MessageDate:       msgDate,
		Text:              msg.Message,
		HasMedia:          hasMedia,
		MediaType:         mediaType,
		WebPageURL:        webPageURL,
		Entities:          entities,
		InlineButtons:     buttons,
	}
}
