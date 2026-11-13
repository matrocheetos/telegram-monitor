package gotd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
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

const (
	defaultListenerReadyTimeout = 3 * time.Second
	maxDialogPages              = 50
	dialogPageLimit             = 100
)

var (
	ErrNoPendingLogin       = errors.New("telegram: no pending web login")
	ErrAlreadyRunning       = errors.New("telegram: listener is already running")
	ErrNotConnected         = errors.New("telegram: client is not connected")
	ErrCredentialsNotConfig = errors.New("telegram: credentials not configured")
)

type authClient interface {
	Run(ctx context.Context, f func(ctx context.Context) error) error
	SendCode(ctx context.Context, req *tg.AuthSendCodeRequest) (tg.AuthSentCodeClass, error)
	SignIn(ctx context.Context, phone, code, codeHash string) (*tg.AuthAuthorization, error)
	Password(ctx context.Context, password string) (*tg.AuthAuthorization, error)
	DumpSession() ([]byte, error)
}

type gotdAuthClient struct {
	client  *telegram.Client
	storage *session.StorageMemory
}

func (g *gotdAuthClient) Run(ctx context.Context, f func(ctx context.Context) error) error {
	return g.client.Run(ctx, f)
}

func (g *gotdAuthClient) SendCode(ctx context.Context, req *tg.AuthSendCodeRequest) (tg.AuthSentCodeClass, error) {
	return g.client.API().AuthSendCode(ctx, req)
}

func (g *gotdAuthClient) SignIn(ctx context.Context, phone, code, codeHash string) (*tg.AuthAuthorization, error) {
	return g.client.Auth().SignIn(ctx, phone, code, codeHash)
}

func (g *gotdAuthClient) Password(ctx context.Context, password string) (*tg.AuthAuthorization, error) {
	return g.client.Auth().Password(ctx, password)
}

func (g *gotdAuthClient) DumpSession() ([]byte, error) {
	var buf bytes.Buffer
	if err := g.storage.Dump(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

type dialogsAPI interface {
	MessagesGetDialogs(ctx context.Context, request *tg.MessagesGetDialogsRequest) (tg.MessagesDialogsClass, error)
}

type listenerClient interface {
	Run(ctx context.Context, f func(ctx context.Context) error) error
	API() dialogsAPI
}

type gotdListenerClient struct {
	client *telegram.Client
}

func (g *gotdListenerClient) Run(ctx context.Context, f func(ctx context.Context) error) error {
	return g.client.Run(ctx, f)
}

func (g *gotdListenerClient) API() dialogsAPI {
	return g.client.API()
}

type verifyRequest struct {
	code        string
	password2FA string
}

type verifyResponse struct {
	sessionData []byte
	err         error
}

type pendingAuth struct {
	mu            sync.Mutex
	apiID         int
	apiHash       string
	phone         string
	phoneCodeHash string
	verifyReqCh   chan verifyRequest
	verifyResCh   chan verifyResponse
	cancel        context.CancelFunc
	done          chan struct{}
}

type ClientManager struct {
	repo      port.CredentialRepository
	crypto    port.CryptoService
	processor port.MessageProcessor

	newAuthClient     func(apiID int, apiHash string) authClient
	newListenerClient func(apiID int, apiHash string, sessionStorage session.Storage, handler telegram.UpdateHandler) listenerClient
	readyTimeout      time.Duration

	authMu  sync.Mutex
	pending *pendingAuth

	listenerMu     sync.Mutex
	running        bool
	connected      atomic.Bool
	listenerCancel context.CancelFunc
	listenerClient listenerClient
	listenerAPI    dialogsAPI
	listenerDone   chan struct{}
	listenerReady  chan struct{}
}

var _ port.TelegramClientPort = (*ClientManager)(nil)

func NewClientManager(
	repo port.CredentialRepository,
	crypto port.CryptoService,
	processor port.MessageProcessor,
) *ClientManager {
	return &ClientManager{
		repo:         repo,
		crypto:       crypto,
		processor:    processor,
		readyTimeout: defaultListenerReadyTimeout,
		newAuthClient: func(apiID int, apiHash string) authClient {
			memStorage := &session.StorageMemory{}
			client := telegram.NewClient(apiID, apiHash, telegram.Options{
				SessionStorage: memStorage,
			})
			return &gotdAuthClient{
				client:  client,
				storage: memStorage,
			}
		},
		newListenerClient: func(apiID int, apiHash string, sessionStorage session.Storage, handler telegram.UpdateHandler) listenerClient {
			client := telegram.NewClient(apiID, apiHash, telegram.Options{
				SessionStorage: sessionStorage,
				UpdateHandler:  handler,
			})
			return &gotdListenerClient{client: client}
		},
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

func sendVerifyResponse(ctx context.Context, ch chan<- verifyResponse, res verifyResponse) {
	select {
	case ch <- res:
	case <-ctx.Done():
	}
}

func (m *ClientManager) clearPending(pending *pendingAuth) {
	m.authMu.Lock()
	if m.pending == pending {
		m.pending = nil
	}
	m.authMu.Unlock()
}

func (m *ClientManager) finishPending(pending *pendingAuth) {
	m.clearPending(pending)
	pending.cancel()
	<-pending.done
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
		done:        make(chan struct{}),
	}

	m.authMu.Lock()
	prev := m.pending
	if prev != nil {
		prev.cancel()
	}
	m.pending = pending
	m.authMu.Unlock()

	if prev != nil {
		<-prev.done
	}

	tempClient := m.newAuthClient(apiID, apiHash)

	go func() {
		defer func() {
			authCancel()
			m.clearPending(pending)
			close(pending.done)
		}()

		_ = tempClient.Run(authCtx, func(runCtx context.Context) error {
			res, err := tempClient.SendCode(runCtx, &tg.AuthSendCodeRequest{
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

			awaiting2FA := false
			for {
				select {
				case <-runCtx.Done():
					return runCtx.Err()
				case req := <-verifyReqCh:
					if !awaiting2FA {
						_, signErr := tempClient.SignIn(runCtx, phone, req.code, phoneCodeHash)
						if signErr != nil {
							if errors.Is(signErr, auth.ErrPasswordAuthNeeded) || tgerr.Is(signErr, "SESSION_PASSWORD_NEEDED") {
								if req.password2FA == "" {
									awaiting2FA = true
									sendVerifyResponse(runCtx, verifyResCh, verifyResponse{err: domain.ErrTelegram2FARequired})
									continue
								}
								if _, pwdErr := tempClient.Password(runCtx, req.password2FA); pwdErr != nil {
									sendVerifyResponse(runCtx, verifyResCh, verifyResponse{err: pwdErr})
									return pwdErr
								}
							} else {
								sendVerifyResponse(runCtx, verifyResCh, verifyResponse{err: signErr})
								return signErr
							}
						}
					} else {
						if req.password2FA == "" {
							sendVerifyResponse(runCtx, verifyResCh, verifyResponse{err: domain.ErrTelegram2FARequired})
							continue
						}
						if _, pwdErr := tempClient.Password(runCtx, req.password2FA); pwdErr != nil {
							sendVerifyResponse(runCtx, verifyResCh, verifyResponse{err: pwdErr})
							return pwdErr
						}
					}

					sessionData, dumpErr := tempClient.DumpSession()
					if dumpErr != nil {
						sendVerifyResponse(runCtx, verifyResCh, verifyResponse{err: dumpErr})
						return dumpErr
					}
					sendVerifyResponse(runCtx, verifyResCh, verifyResponse{sessionData: sessionData})
					return nil
				}
			}
		})
	}()

	select {
	case <-ctx.Done():
		m.finishPending(pending)
		return "", ctx.Err()
	case err := <-codeErrCh:
		m.finishPending(pending)
		return "", fmt.Errorf("telegram: send code failed: %w", err)
	case hash := <-codeSentCh:
		m.authMu.Lock()
		if m.pending == pending {
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

	pending.mu.Lock()
	defer pending.mu.Unlock()

	m.authMu.Lock()
	stillCurrent := m.pending == pending
	m.authMu.Unlock()
	if !stillCurrent {
		return nil, ErrNoPendingLogin
	}

	req := verifyRequest{
		code:        code,
		password2FA: strings.TrimSpace(password2FA),
	}

	select {
	case pending.verifyReqCh <- req:
	case <-pending.done:
		m.clearPending(pending)
		return nil, ErrNoPendingLogin
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	var res verifyResponse
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case res = <-pending.verifyResCh:
	case <-pending.done:
		select {
		case res = <-pending.verifyResCh:
		default:
			m.clearPending(pending)
			return nil, ErrNoPendingLogin
		}
	}

	if res.err != nil {
		if errors.Is(res.err, domain.ErrTelegram2FARequired) {
			return nil, domain.ErrTelegram2FARequired
		}
		m.finishPending(pending)
		return nil, res.err
	}

	err := m.saveAuthenticatedSession(ctx, pending, res.sessionData)
	m.finishPending(pending)
	if err != nil {
		return nil, err
	}

	_ = m.StartListener(context.Background())

	return res.sessionData, nil
}

func (m *ClientManager) saveAuthenticatedSession(ctx context.Context, pending *pendingAuth, sessionData []byte) error {
	encAPIID, err := m.crypto.Encrypt([]byte(strconv.Itoa(pending.apiID)))
	if err != nil {
		return fmt.Errorf("encrypt api id: %w", err)
	}

	encAPIHash, err := m.crypto.Encrypt([]byte(pending.apiHash))
	if err != nil {
		return fmt.Errorf("encrypt api hash: %w", err)
	}

	encSession, err := m.crypto.Encrypt(sessionData)
	if err != nil {
		return fmt.Errorf("encrypt session: %w", err)
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
		return fmt.Errorf("save credentials: %w", err)
	}
	return nil
}

func (m *ClientManager) loadDecryptedCredentials(ctx context.Context) (int, string, error) {
	creds, err := m.repo.GetTelegramCredentials(ctx)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return 0, "", ErrCredentialsNotConfig
		}
		return 0, "", fmt.Errorf("get telegram credentials: %w", err)
	}

	if len(creds.EncryptedSession) == 0 {
		return 0, "", ErrCredentialsNotConfig
	}

	apiIDBytes, err := m.crypto.Decrypt(creds.EncryptedAPIID)
	if err != nil {
		return 0, "", fmt.Errorf("decrypt api id: %w", err)
	}

	apiID, err := strconv.Atoi(string(apiIDBytes))
	if err != nil {
		return 0, "", fmt.Errorf("parse api id: %w", err)
	}

	apiHashBytes, err := m.crypto.Decrypt(creds.EncryptedAPIHash)
	if err != nil {
		return 0, "", fmt.Errorf("decrypt api hash: %w", err)
	}

	return apiID, string(apiHashBytes), nil
}

func (m *ClientManager) buildUpdateDispatcher() tg.UpdateDispatcher {
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

	return dispatcher
}

func (m *ClientManager) waitForListenerReady(ctx context.Context, readyCh, doneCh <-chan struct{}) error {
	timeout := m.readyTimeout
	if timeout <= 0 {
		timeout = defaultListenerReadyTimeout
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case <-readyCh:
		return nil
	case <-doneCh:
		return nil
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (m *ClientManager) StartListener(ctx context.Context) error {
	m.listenerMu.Lock()
	if m.running {
		readyCh := m.listenerReady
		doneCh := m.listenerDone
		m.listenerMu.Unlock()
		if readyCh != nil && doneCh != nil {
			return m.waitForListenerReady(ctx, readyCh, doneCh)
		}
		return nil
	}
	m.listenerMu.Unlock()

	apiID, apiHash, err := m.loadDecryptedCredentials(ctx)
	if err != nil {
		return err
	}

	m.listenerMu.Lock()
	if m.running {
		readyCh := m.listenerReady
		doneCh := m.listenerDone
		m.listenerMu.Unlock()
		if readyCh != nil && doneCh != nil {
			return m.waitForListenerReady(ctx, readyCh, doneCh)
		}
		return nil
	}

	dispatcher := m.buildUpdateDispatcher()
	sessionStorage := NewSessionStorage(m.repo, m.crypto)
	client := m.newListenerClient(apiID, apiHash, sessionStorage, dispatcher)

	listenerCtx, listenerCancel := context.WithCancel(context.Background())
	doneCh := make(chan struct{})
	readyCh := make(chan struct{})

	m.listenerCancel = listenerCancel
	m.listenerClient = client
	m.listenerDone = doneCh
	m.listenerReady = readyCh
	m.running = true
	m.listenerMu.Unlock()

	go func() {
		defer func() {
			m.listenerMu.Lock()
			m.connected.Store(false)
			m.running = false
			m.listenerCancel = nil
			m.listenerClient = nil
			m.listenerAPI = nil
			m.listenerDone = nil
			m.listenerReady = nil
			m.listenerMu.Unlock()
			close(doneCh)
		}()

		runErr := client.Run(listenerCtx, func(runCtx context.Context) error {
			m.listenerMu.Lock()
			m.listenerAPI = client.API()
			m.connected.Store(true)
			m.listenerMu.Unlock()
			close(readyCh)

			<-runCtx.Done()
			return runCtx.Err()
		})
		if runErr != nil && !errors.Is(runErr, context.Canceled) {
			slog.ErrorContext(listenerCtx, "telegram mtproto listener exited with error", "error", runErr)
		}
	}()

	return m.waitForListenerReady(ctx, readyCh, doneCh)
}

func (m *ClientManager) StopListener(ctx context.Context) error {
	m.listenerMu.Lock()
	cancel := m.listenerCancel
	doneCh := m.listenerDone
	m.listenerCancel = nil
	m.running = false
	m.connected.Store(false)
	m.listenerMu.Unlock()

	if cancel != nil {
		cancel()
	}

	if doneCh != nil {
		select {
		case <-doneCh:
		case <-ctx.Done():
			return ctx.Err()
		}
	}

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

	var (
		channels   []*domain.TelegramChannel
		offsetDate int
		offsetID   int
		offsetPeer tg.InputPeerClass = &tg.InputPeerEmpty{}
	)
	seen := make(map[int64]struct{})

	for page := 0; page < maxDialogPages; page++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		res, err := api.MessagesGetDialogs(ctx, &tg.MessagesGetDialogsRequest{
			OffsetDate: offsetDate,
			OffsetID:   offsetID,
			OffsetPeer: offsetPeer,
			Limit:      dialogPageLimit,
		})
		if err != nil {
			return nil, fmt.Errorf("telegram: get dialogs failed: %w", err)
		}

		switch d := res.(type) {
		case *tg.MessagesDialogs:
			channels = append(channels, extractDialogsPage(d.Chats, seen)...)
			return channels, nil
		case *tg.MessagesDialogsSlice:
			channels = append(channels, extractDialogsPage(d.Chats, seen)...)
			if len(d.Dialogs) == 0 || len(d.Dialogs) < dialogPageLimit {
				return channels, nil
			}
			nextDate, nextID, nextPeer, ok := nextDialogsCursor(d.Dialogs, d.Messages, d.Chats, d.Users)
			if !ok {
				return channels, nil
			}
			if nextDate == offsetDate && nextID == offsetID && sameInputPeer(nextPeer, offsetPeer) {
				return channels, nil
			}
			offsetDate = nextDate
			offsetID = nextID
			offsetPeer = nextPeer
		default:
			return nil, fmt.Errorf("unexpected dialogs type: %T", res)
		}
	}

	return channels, nil
}

func extractDialogsPage(chats []tg.ChatClass, seen map[int64]struct{}) []*domain.TelegramChannel {
	var channels []*domain.TelegramChannel
	for _, c := range chats {
		switch ch := c.(type) {
		case *tg.Channel:
			if ch.Left {
				continue
			}
			chatID := -1000000000000 - ch.ID
			if _, exists := seen[chatID]; exists {
				continue
			}
			seen[chatID] = struct{}{}
			username, _ := ch.GetUsername()
			accessHash, _ := ch.GetAccessHash()
			channels = append(channels, &domain.TelegramChannel{
				ChatID:     chatID,
				AccessHash: accessHash,
				Username:   username,
				Title:      ch.Title,
			})
		case *tg.Chat:
			if ch.Left || ch.Deactivated {
				continue
			}
			chatID := -ch.ID
			if _, exists := seen[chatID]; exists {
				continue
			}
			seen[chatID] = struct{}{}
			channels = append(channels, &domain.TelegramChannel{
				ChatID: chatID,
				Title:  ch.Title,
			})
		}
	}
	return channels
}

func nextDialogsCursor(
	dialogs []tg.DialogClass,
	messages []tg.MessageClass,
	chats []tg.ChatClass,
	users []tg.UserClass,
) (offsetDate int, offsetID int, offsetPeer tg.InputPeerClass, ok bool) {
	var lastDialog *tg.Dialog
	for i := len(dialogs) - 1; i >= 0; i-- {
		if dlg, isDlg := dialogs[i].(*tg.Dialog); isDlg {
			lastDialog = dlg
			break
		}
	}
	if lastDialog == nil {
		return 0, 0, nil, false
	}

	nextPeer, peerOK := buildInputPeerFromPeer(lastDialog.Peer, chats, users)
	if !peerOK {
		return 0, 0, nil, false
	}

	nextDate := findMessageDate(messages, lastDialog.TopMessage, lastDialog.Peer)
	if nextDate == 0 {
		return 0, 0, nil, false
	}

	return nextDate, lastDialog.TopMessage, nextPeer, true
}

func findMessageDate(messages []tg.MessageClass, topMessageID int, peer tg.PeerClass) int {
	fallbackDate := 0
	for _, m := range messages {
		var (
			msgID   int
			msgDate int
			msgPeer tg.PeerClass
		)
		switch msg := m.(type) {
		case *tg.Message:
			msgID = msg.ID
			msgDate = msg.Date
			msgPeer = msg.PeerID
		case *tg.MessageService:
			msgID = msg.ID
			msgDate = msg.Date
			msgPeer = msg.PeerID
		default:
			continue
		}
		if msgID != topMessageID {
			continue
		}
		if samePeer(msgPeer, peer) {
			return msgDate
		}
		if fallbackDate == 0 {
			fallbackDate = msgDate
		}
	}
	return fallbackDate
}

func samePeer(a, b tg.PeerClass) bool {
	if a == nil || b == nil {
		return false
	}
	switch pa := a.(type) {
	case *tg.PeerChannel:
		pb, ok := b.(*tg.PeerChannel)
		return ok && pa.ChannelID == pb.ChannelID
	case *tg.PeerChat:
		pb, ok := b.(*tg.PeerChat)
		return ok && pa.ChatID == pb.ChatID
	case *tg.PeerUser:
		pb, ok := b.(*tg.PeerUser)
		return ok && pa.UserID == pb.UserID
	default:
		return false
	}
}

func sameInputPeer(a, b tg.InputPeerClass) bool {
	if a == nil || b == nil {
		return false
	}
	switch pa := a.(type) {
	case *tg.InputPeerEmpty:
		_, ok := b.(*tg.InputPeerEmpty)
		return ok
	case *tg.InputPeerChannel:
		pb, ok := b.(*tg.InputPeerChannel)
		return ok && pa.ChannelID == pb.ChannelID
	case *tg.InputPeerChat:
		pb, ok := b.(*tg.InputPeerChat)
		return ok && pa.ChatID == pb.ChatID
	case *tg.InputPeerUser:
		pb, ok := b.(*tg.InputPeerUser)
		return ok && pa.UserID == pb.UserID
	default:
		return false
	}
}

func buildInputPeerFromPeer(peer tg.PeerClass, chats []tg.ChatClass, users []tg.UserClass) (tg.InputPeerClass, bool) {
	switch p := peer.(type) {
	case *tg.PeerChannel:
		for _, c := range chats {
			switch ch := c.(type) {
			case *tg.Channel:
				if ch.ID == p.ChannelID {
					return &tg.InputPeerChannel{
						ChannelID:  ch.ID,
						AccessHash: ch.AccessHash,
					}, true
				}
			case *tg.ChannelForbidden:
				if ch.ID == p.ChannelID {
					return &tg.InputPeerChannel{
						ChannelID:  ch.ID,
						AccessHash: ch.AccessHash,
					}, true
				}
			}
		}
		return nil, false
	case *tg.PeerChat:
		return &tg.InputPeerChat{ChatID: p.ChatID}, true
	case *tg.PeerUser:
		for _, u := range users {
			if usr, ok := u.(*tg.User); ok && usr.ID == p.UserID {
				return &tg.InputPeerUser{
					UserID:     usr.ID,
					AccessHash: usr.AccessHash,
				}, true
			}
		}
		return nil, false
	default:
		return nil, false
	}
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
