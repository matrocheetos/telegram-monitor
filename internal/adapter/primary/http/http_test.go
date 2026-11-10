package http_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	adapterHTTP "github.com/matrocheetos/telegram-monitor/internal/adapter/primary/http"
	"github.com/matrocheetos/telegram-monitor/internal/adapter/primary/http/i18n"
	"github.com/matrocheetos/telegram-monitor/internal/domain"
)

type mockSetupUseCase struct {
	needsSetup bool
	adminUser  *domain.User
	createErr  error
}

func (m *mockSetupUseCase) NeedsInitialSetup(ctx context.Context) (bool, error) {
	return m.needsSetup, nil
}

func (m *mockSetupUseCase) CreateInitialAdmin(ctx context.Context, username, password string) (*domain.User, error) {
	if m.createErr != nil {
		return nil, m.createErr
	}
	m.needsSetup = false
	user := &domain.User{
		ID:        1,
		Username:  username,
		Role:      domain.RoleAdmin,
		NtfyTopic: "promo/user/" + username,
	}
	m.adminUser = user
	return user, nil
}

type mockAuthUseCase struct {
	users        map[string]*domain.User
	sessions     map[string]*domain.User
	loginErr     error
	sessionCount int
}

func (m *mockAuthUseCase) Login(ctx context.Context, username, password string) (*domain.User, string, error) {
	if m.loginErr != nil {
		return nil, "", m.loginErr
	}
	user, ok := m.users[username]
	if !ok {
		return nil, "", domain.ErrInvalidCredentials
	}
	m.sessionCount++
	token := fmt.Sprintf("test-token-%d", m.sessionCount)
	m.sessions[token] = user
	return user, token, nil
}

func (m *mockAuthUseCase) Logout(ctx context.Context, token string) error {
	delete(m.sessions, token)
	return nil
}

func (m *mockAuthUseCase) AuthenticateSession(ctx context.Context, token string) (*domain.User, error) {
	user, ok := m.sessions[token]
	if !ok {
		return nil, domain.ErrUnauthorized
	}
	return user, nil
}

type mockUserAdminUseCase struct {
	users []*domain.User
}

func (m *mockUserAdminUseCase) CreateUser(ctx context.Context, username, password string, role domain.Role) (*domain.User, error) {
	u := &domain.User{
		ID:        int64(len(m.users) + 1),
		Username:  username,
		Role:      role,
		NtfyTopic: "promo/user/" + username,
	}
	m.users = append(m.users, u)
	return u, nil
}

func (m *mockUserAdminUseCase) ListUsers(ctx context.Context) ([]*domain.User, error) {
	return m.users, nil
}

func (m *mockUserAdminUseCase) ResetUserPassword(ctx context.Context, userID int64, newPassword string) error {
	return nil
}

func (m *mockUserAdminUseCase) DeleteUser(ctx context.Context, userID int64) error {
	newUsers := make([]*domain.User, 0, len(m.users))
	for _, u := range m.users {
		if u.ID != userID {
			newUsers = append(newUsers, u)
		}
	}
	m.users = newUsers
	return nil
}

type mockTelegramAuthUseCase struct {
	connected   bool
	phoneMasked string
	sendCodeErr error
	verifyErr   error
}

func (m *mockTelegramAuthUseCase) GetConnectionStatus(ctx context.Context) (bool, string, error) {
	return m.connected, m.phoneMasked, nil
}

func (m *mockTelegramAuthUseCase) SendLoginCode(ctx context.Context, apiID int, apiHash, phone string) (string, error) {
	if m.sendCodeErr != nil {
		return "", m.sendCodeErr
	}
	return "test-phone-code-hash", nil
}

func (m *mockTelegramAuthUseCase) VerifyLoginCode(ctx context.Context, phoneCode, password2FA string) error {
	if m.verifyErr != nil {
		return m.verifyErr
	}
	m.connected = true
	m.phoneMasked = "+55 11 •••••-9999"
	return nil
}

func (m *mockTelegramAuthUseCase) Disconnect(ctx context.Context) error {
	m.connected = false
	m.phoneMasked = ""
	return nil
}

type mockChannelUseCase struct {
	channels []*domain.TelegramChannel
}

func (m *mockChannelUseCase) SyncChannels(ctx context.Context) error {
	return nil
}

func (m *mockChannelUseCase) ListChannels(ctx context.Context) ([]*domain.TelegramChannel, error) {
	return m.channels, nil
}

func (m *mockChannelUseCase) UpdateChannelFlags(ctx context.Context, chatID int64, monitorProducts, monitorCoupons bool) error {
	for _, ch := range m.channels {
		if ch.ChatID == chatID {
			ch.MonitorProducts = monitorProducts
			ch.MonitorCoupons = monitorCoupons
		}
	}
	return nil
}

type mockInterestUseCase struct {
	interests map[int64][]*domain.UserInterest
}

func (m *mockInterestUseCase) AddInterest(ctx context.Context, userID int64, rawTerm string, maxPrice *float64) (*domain.UserInterest, error) {
	interest, err := domain.NewUserInterest(userID, rawTerm, maxPrice)
	if err != nil {
		return nil, err
	}
	interest.ID = int64(len(m.interests[userID]) + 1)
	m.interests[userID] = append(m.interests[userID], interest)
	return interest, nil
}

func (m *mockInterestUseCase) ListUserInterests(ctx context.Context, userID int64) ([]*domain.UserInterest, error) {
	return m.interests[userID], nil
}

func (m *mockInterestUseCase) DeleteInterest(ctx context.Context, userID, interestID int64) error {
	list := m.interests[userID]
	updated := make([]*domain.UserInterest, 0, len(list))
	for _, item := range list {
		if item.ID != interestID {
			updated = append(updated, item)
		}
	}
	m.interests[userID] = updated
	return nil
}

type testFixture struct {
	server       *adapterHTTP.Server
	setupUC      *mockSetupUseCase
	authUC       *mockAuthUseCase
	userAdminUC  *mockUserAdminUseCase
	tgAuthUC     *mockTelegramAuthUseCase
	channelUC    *mockChannelUseCase
	interestUC   *mockInterestUseCase
	translator   *i18n.Translator
	regularUser  *domain.User
	adminUser    *domain.User
	regularToken string
	adminToken   string
}

func setupFixture(t *testing.T, needsSetup bool, tgConnected bool) *testFixture {
	t.Helper()
	tr, err := i18n.NewTranslator()
	if err != nil {
		t.Fatalf("failed to create translator: %v", err)
	}

	adminUser := &domain.User{
		ID:        1,
		Username:  "admin",
		Role:      domain.RoleAdmin,
		NtfyTopic: "promo/user/admin",
		CreatedAt: time.Now(),
	}
	regUser := &domain.User{
		ID:        2,
		Username:  "mateus",
		Role:      domain.RoleUser,
		NtfyTopic: "promo/user/mateus",
		CreatedAt: time.Now(),
	}

	setupUC := &mockSetupUseCase{needsSetup: needsSetup}
	authUC := &mockAuthUseCase{
		users: map[string]*domain.User{
			"admin":  adminUser,
			"mateus": regUser,
		},
		sessions: map[string]*domain.User{
			"admin-session":   adminUser,
			"regular-session": regUser,
		},
	}
	userAdminUC := &mockUserAdminUseCase{
		users: []*domain.User{adminUser, regUser},
	}
	tgAuthUC := &mockTelegramAuthUseCase{
		connected:   tgConnected,
		phoneMasked: "+55 11 •••••-9999",
	}
	channelUC := &mockChannelUseCase{
		channels: []*domain.TelegramChannel{
			{ChatID: -1001, Title: "Ofertas BR", MonitorProducts: true, MonitorCoupons: true},
			{ChatID: -1002, Title: "Cupons Only", MonitorProducts: false, MonitorCoupons: true},
		},
	}
	interestUC := &mockInterestUseCase{
		interests: make(map[int64][]*domain.UserInterest),
	}

	server, err := adapterHTTP.NewServer(
		adapterHTTP.Config{
			Port:               8080,
			NtfyURL:            "https://ntfy.example.com",
			NtfyTopicSeparator: "-",
		},
		setupUC,
		authUC,
		userAdminUC,
		tgAuthUC,
		channelUC,
		interestUC,
		tr,
	)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	return &testFixture{
		server:       server,
		setupUC:      setupUC,
		authUC:       authUC,
		userAdminUC:  userAdminUC,
		tgAuthUC:     tgAuthUC,
		channelUC:    channelUC,
		interestUC:   interestUC,
		translator:   tr,
		regularUser:  regUser,
		adminUser:    adminUser,
		regularToken: "regular-session",
		adminToken:   adminTokenFromSession,
	}
}

const adminTokenFromSession = "admin-session"

func getCSRFTokenFromResponse(resp *http.Response) string {
	for _, c := range resp.Cookies() {
		if c.Name == "csrf_token" {
			return c.Value
		}
	}
	return ""
}

func TestFirstBootScenario(t *testing.T) {
	fix := setupFixture(t, true, false)
	ctx := context.Background()

	// 1. Root and protected routes redirect to /setup
	req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	fix.server.ServeHTTP(w, req)
	resp := w.Result()
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/setup" {
		t.Errorf("expected 303 to /setup, got status %d loc %s", resp.StatusCode, resp.Header.Get("Location"))
	}

	req = httptest.NewRequestWithContext(ctx, http.MethodGet, "/dashboard", nil)
	w = httptest.NewRecorder()
	fix.server.ServeHTTP(w, req)
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/setup" {
		t.Errorf("dashboard should redirect to /setup on first boot")
	}

	// 2. Static files should be served normally
	req = httptest.NewRequestWithContext(ctx, http.MethodGet, "/static/style.css", nil)
	w = httptest.NewRecorder()
	fix.server.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200 for static style.css, got %d", w.Code)
	}

	// 3. GET /setup should return 200 and set csrf_token cookie
	req = httptest.NewRequestWithContext(ctx, http.MethodGet, "/setup", nil)
	w = httptest.NewRecorder()
	fix.server.ServeHTTP(w, req)
	resp = w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for /setup, got %d", resp.StatusCode)
	}
	csrf := getCSRFTokenFromResponse(resp)
	if csrf == "" {
		t.Fatalf("expected csrf_token cookie to be set")
	}

	// 4. POST /setup with password mismatch -> redirects to /setup
	form := url.Values{
		"csrf_token":       {csrf},
		"username":         {"admin"},
		"password":         {"adminpassword123"},
		"password_confirm": {"differentpassword"},
	}
	req = httptest.NewRequestWithContext(ctx, http.MethodPost, "/setup", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	//nolint:gosec // test cookie
	req.AddCookie(&http.Cookie{Name: "csrf_token", Value: csrf})
	w = httptest.NewRecorder()
	fix.server.ServeHTTP(w, req)
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/setup" {
		t.Errorf("expected redirect to /setup on password mismatch, got %d loc %s", w.Code, w.Header().Get("Location"))
	}

	// 5. POST /setup with matching passwords -> creates admin and redirects to /admin/telegram
	form.Set("password_confirm", "adminpassword123")
	req = httptest.NewRequestWithContext(ctx, http.MethodPost, "/setup", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	//nolint:gosec // test cookie
	req.AddCookie(&http.Cookie{Name: "csrf_token", Value: csrf})
	w = httptest.NewRecorder()
	fix.server.ServeHTTP(w, req)
	resp = w.Result()
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/admin/telegram" {
		t.Errorf("expected redirect to /admin/telegram, got %d loc %s", resp.StatusCode, resp.Header.Get("Location"))
	}
}

func TestAnonymousAndSetupCompleted(t *testing.T) {
	fix := setupFixture(t, false, false)
	ctx := context.Background()

	// 1. GET /setup when setup is completed redirects to /login
	req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/setup", nil)
	w := httptest.NewRecorder()
	fix.server.ServeHTTP(w, req)
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/login" {
		t.Errorf("expected redirect to /login from /setup, got %d loc %s", w.Code, w.Header().Get("Location"))
	}

	// 2. POST /setup when setup is completed returns 403 Forbidden
	req = httptest.NewRequestWithContext(ctx, http.MethodGet, "/login", nil)
	w = httptest.NewRecorder()
	fix.server.ServeHTTP(w, req)
	csrf := getCSRFTokenFromResponse(w.Result())

	form := url.Values{
		"csrf_token":       {csrf},
		"username":         {"admin"},
		"password":         {"pass123456"},
		"password_confirm": {"pass123456"},
	}
	req = httptest.NewRequestWithContext(ctx, http.MethodPost, "/setup", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	//nolint:gosec // test cookie
	req.AddCookie(&http.Cookie{Name: "csrf_token", Value: csrf})
	w = httptest.NewRecorder()
	fix.server.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403 Forbidden for POST /setup after completion, got %d", w.Code)
	}

	// 3. Protected routes redirect to /login
	protected := []string{"/dashboard", "/admin/telegram", "/admin/channels", "/admin/users"}
	for _, path := range protected {
		req = httptest.NewRequestWithContext(ctx, http.MethodGet, path, nil)
		w = httptest.NewRecorder()
		fix.server.ServeHTTP(w, req)
		if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/login" {
			t.Errorf("path %s should redirect to /login for anon, got %d", path, w.Code)
		}
	}

	// 4. POST /login with invalid credentials
	formLogin := url.Values{
		"csrf_token": {csrf},
		"username":   {"invaliduser"},
		"password":   {"wrongpassword"},
	}
	req = httptest.NewRequestWithContext(ctx, http.MethodPost, "/login", strings.NewReader(formLogin.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	//nolint:gosec // test cookie
	req.AddCookie(&http.Cookie{Name: "csrf_token", Value: csrf})
	w = httptest.NewRecorder()
	fix.server.ServeHTTP(w, req)
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/login" {
		t.Errorf("expected redirect to /login on failed login, got %d", w.Code)
	}

	// 5. POST /login with valid admin credentials (without telegram session) -> redirects to /admin/telegram
	formLogin.Set("username", "admin")
	formLogin.Set("password", "correctpass")
	req = httptest.NewRequestWithContext(ctx, http.MethodPost, "/login", strings.NewReader(formLogin.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	//nolint:gosec // test cookie
	req.AddCookie(&http.Cookie{Name: "csrf_token", Value: csrf})
	w = httptest.NewRecorder()
	fix.server.ServeHTTP(w, req)
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/admin/telegram" {
		t.Errorf("expected admin without telegram to redirect to /admin/telegram, got %d loc %s", w.Code, w.Header().Get("Location"))
	}
}

func TestRegularUserRoleRBAC(t *testing.T) {
	fix := setupFixture(t, false, true)
	ctx := context.Background()

	// User session cookie
	//nolint:gosec // test cookie
	userCookie := &http.Cookie{Name: "session_id", Value: fix.regularToken}

	// 1. Dashboard is accessible
	req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/dashboard", nil)
	req.AddCookie(userCookie)
	w := httptest.NewRecorder()
	fix.server.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for user /dashboard, got %d", w.Code)
	}
	csrf := getCSRFTokenFromResponse(w.Result())

	// 2. Add interest
	formInterest := url.Values{
		"csrf_token": {csrf},
		"term":       {"Nintendo Switch 2 -case"},
		"max_price":  {"2500.00"},
	}
	req = httptest.NewRequestWithContext(ctx, http.MethodPost, "/interests", strings.NewReader(formInterest.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(userCookie)
	//nolint:gosec // test cookie
	req.AddCookie(&http.Cookie{Name: "csrf_token", Value: csrf})
	w = httptest.NewRecorder()
	fix.server.ServeHTTP(w, req)
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/dashboard" {
		t.Errorf("expected redirect to /dashboard after adding interest, got %d", w.Code)
	}

	if len(fix.interestUC.interests[fix.regularUser.ID]) != 1 {
		t.Errorf("expected 1 interest added, got %d", len(fix.interestUC.interests[fix.regularUser.ID]))
	}

	// 3. Admin routes must return 403 Forbidden for role: user
	adminPaths := []string{
		"/admin/telegram",
		"/admin/channels",
		"/admin/users",
	}
	for _, p := range adminPaths {
		req = httptest.NewRequestWithContext(ctx, http.MethodGet, p, nil)
		req.AddCookie(userCookie)
		w = httptest.NewRecorder()
		fix.server.ServeHTTP(w, req)
		if w.Code != http.StatusForbidden {
			t.Errorf("expected 403 Forbidden for user on %s, got %d", p, w.Code)
		}
	}

	// 4. Logout
	formLogout := url.Values{"csrf_token": {csrf}}
	req = httptest.NewRequestWithContext(ctx, http.MethodPost, "/logout", strings.NewReader(formLogout.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(userCookie)
	//nolint:gosec // test cookie
	req.AddCookie(&http.Cookie{Name: "csrf_token", Value: csrf})
	w = httptest.NewRecorder()
	fix.server.ServeHTTP(w, req)
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/login" {
		t.Errorf("expected redirect to /login on logout, got %d", w.Code)
	}
}

func TestAdminRoleWorkflows(t *testing.T) {
	ctx := context.Background()

	// First test Admin with Telegram NOT connected: should be forced to /admin/telegram when hitting /dashboard
	fixNoTG := setupFixture(t, false, false)
	//nolint:gosec // test cookie
	adminCookie := &http.Cookie{Name: "session_id", Value: fixNoTG.adminToken}

	req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/dashboard", nil)
	req.AddCookie(adminCookie)
	w := httptest.NewRecorder()
	fixNoTG.server.ServeHTTP(w, req)
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/admin/telegram" {
		t.Errorf("admin without telegram hitting /dashboard must redirect to /admin/telegram, got %d loc %s", w.Code, w.Header().Get("Location"))
	}

	// Now test Admin with Telegram connected
	fix := setupFixture(t, false, true)
	//nolint:gosec // test cookie
	adminCookie = &http.Cookie{Name: "session_id", Value: fix.adminToken}

	// 1. GET /admin/telegram
	req = httptest.NewRequestWithContext(ctx, http.MethodGet, "/admin/telegram", nil)
	req.AddCookie(adminCookie)
	w = httptest.NewRecorder()
	fix.server.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200 for /admin/telegram, got %d", w.Code)
	}
	csrf := getCSRFTokenFromResponse(w.Result())

	// 2. POST /admin/telegram/send-code
	formCode := url.Values{
		"csrf_token": {csrf},
		"api_id":     {"37846961"},
		"api_hash":   {"cd1e0e6e98dae6a46362b03acc5508db"},
		"phone":      {"+5511999999999"},
	}
	req = httptest.NewRequestWithContext(ctx, http.MethodPost, "/admin/telegram/send-code", strings.NewReader(formCode.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(adminCookie)
	//nolint:gosec // test cookie
	req.AddCookie(&http.Cookie{Name: "csrf_token", Value: csrf})
	w = httptest.NewRecorder()
	fix.server.ServeHTTP(w, req)
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/admin/telegram" {
		t.Errorf("expected redirect to /admin/telegram after send-code, got %d", w.Code)
	}

	// 3. POST /admin/telegram/verify
	formVerify := url.Values{
		"csrf_token":   {csrf},
		"phone_code":   {"12345"},
		"password_2fa": {"my2fapassword"},
	}
	req = httptest.NewRequestWithContext(ctx, http.MethodPost, "/admin/telegram/verify", strings.NewReader(formVerify.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(adminCookie)
	//nolint:gosec // test cookie
	req.AddCookie(&http.Cookie{Name: "csrf_token", Value: csrf})
	w = httptest.NewRecorder()
	fix.server.ServeHTTP(w, req)
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/admin/channels" {
		t.Errorf("expected redirect to /admin/channels after verify, got %d loc %s", w.Code, w.Header().Get("Location"))
	}

	// 4. GET /admin/channels & update channel flags
	req = httptest.NewRequestWithContext(ctx, http.MethodGet, "/admin/channels", nil)
	req.AddCookie(adminCookie)
	w = httptest.NewRecorder()
	fix.server.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200 for /admin/channels, got %d", w.Code)
	}

	formChannels := url.Values{
		"csrf_token":             {csrf},
		"channel_ids":            {"-1001", "-1002"},
		"monitor_products_-1001": {"true"},
		"monitor_coupons_-1001":  {"false"},
		"monitor_products_-1002": {"false"},
		"monitor_coupons_-1002":  {"true"},
	}
	req = httptest.NewRequestWithContext(ctx, http.MethodPost, "/admin/channels/update", strings.NewReader(formChannels.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(adminCookie)
	//nolint:gosec // test cookie
	req.AddCookie(&http.Cookie{Name: "csrf_token", Value: csrf})
	w = httptest.NewRecorder()
	fix.server.ServeHTTP(w, req)
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/admin/channels" {
		t.Errorf("expected redirect to /admin/channels after update, got %d", w.Code)
	}

	// 5. GET /admin/users & Create user
	req = httptest.NewRequestWithContext(ctx, http.MethodGet, "/admin/users", nil)
	req.AddCookie(adminCookie)
	w = httptest.NewRecorder()
	fix.server.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200 for /admin/users, got %d", w.Code)
	}

	formCreateUser := url.Values{
		"csrf_token": {csrf},
		"username":   {"novousuario"},
		"password":   {"novasenha123"},
		"role":       {"user"},
	}
	req = httptest.NewRequestWithContext(ctx, http.MethodPost, "/admin/users", strings.NewReader(formCreateUser.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(adminCookie)
	//nolint:gosec // test cookie
	req.AddCookie(&http.Cookie{Name: "csrf_token", Value: csrf})
	w = httptest.NewRecorder()
	fix.server.ServeHTTP(w, req)
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/admin/users" {
		t.Errorf("expected redirect to /admin/users after creating user, got %d", w.Code)
	}

	// Self-deletion check: admin cannot delete their own account (id: 1)
	formDelete := url.Values{"csrf_token": {csrf}}
	req = httptest.NewRequestWithContext(ctx, http.MethodPost, "/admin/users/1/delete", strings.NewReader(formDelete.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(adminCookie)
	//nolint:gosec // test cookie
	req.AddCookie(&http.Cookie{Name: "csrf_token", Value: csrf})
	w = httptest.NewRecorder()
	fix.server.ServeHTTP(w, req)
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/admin/users" {
		t.Errorf("expected redirect to /admin/users, got %d", w.Code)
	}
	// Verify user 1 is still present
	users, _ := fix.userAdminUC.ListUsers(context.Background())
	foundSelf := false
	for _, u := range users {
		if u.ID == 1 {
			foundSelf = true
		}
	}
	if !foundSelf {
		t.Errorf("admin self-deletion should have been prevented")
	}
}

func TestCSRFAndSecurityHeaders(t *testing.T) {
	fix := setupFixture(t, false, true)
	ctx := context.Background()

	// 1. GET response must have security headers
	req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/login", nil)
	w := httptest.NewRecorder()
	fix.server.ServeHTTP(w, req)

	resp := w.Result()
	if resp.Header.Get("X-Content-Type-Options") != "nosniff" {
		t.Errorf("expected nosniff, got %q", resp.Header.Get("X-Content-Type-Options"))
	}
	if resp.Header.Get("X-Frame-Options") != "DENY" {
		t.Errorf("expected DENY, got %q", resp.Header.Get("X-Frame-Options"))
	}
	if resp.Header.Get("Referrer-Policy") != "same-origin" {
		t.Errorf("expected same-origin, got %q", resp.Header.Get("Referrer-Policy"))
	}

	// 2. POST without CSRF token must return 403 Forbidden
	form := url.Values{
		"username": {"admin"},
		"password": {"password"},
	}
	req = httptest.NewRequestWithContext(ctx, http.MethodPost, "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w = httptest.NewRecorder()
	fix.server.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403 Forbidden for missing CSRF, got %d", w.Code)
	}
}

func TestI18nSwitchLanguage(t *testing.T) {
	fix := setupFixture(t, false, true)
	ctx := context.Background()

	// 1. Get login page with PT default
	req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/login", nil)
	w := httptest.NewRecorder()
	fix.server.ServeHTTP(w, req)
	body := w.Body.String()
	if !strings.Contains(body, "Acesso ao Painel") {
		t.Errorf("expected Portuguese text 'Acesso ao Painel' in default login page")
	}

	// 2. Query param ?lang=en-US sets English
	req = httptest.NewRequestWithContext(ctx, http.MethodGet, "/login?lang=en-US", nil)
	w = httptest.NewRecorder()
	fix.server.ServeHTTP(w, req)
	body = w.Body.String()
	if !strings.Contains(body, "Dashboard Access") {
		t.Errorf("expected English text 'Dashboard Access' with ?lang=en-US")
	}

	// 3. POST /lang sets cookie and redirects to referer
	csrf := getCSRFTokenFromResponse(w.Result())
	formLang := url.Values{
		"csrf_token": {csrf},
		"lang":       {"en-US"},
	}
	req = httptest.NewRequestWithContext(ctx, http.MethodPost, "/lang", strings.NewReader(formLang.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Referer", "/login")
	//nolint:gosec // test cookie
	req.AddCookie(&http.Cookie{Name: "csrf_token", Value: csrf})
	w = httptest.NewRecorder()
	fix.server.ServeHTTP(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/login" {
		t.Errorf("expected redirect to /login from /lang, got %d", resp.StatusCode)
	}

	foundCookie := false
	for _, c := range resp.Cookies() {
		if c.Name == "lang" && c.Value == "en-US" {
			foundCookie = true
		}
	}
	if !foundCookie {
		t.Errorf("expected lang=en-US cookie set on POST /lang")
	}
}
