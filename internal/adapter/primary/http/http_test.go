package http_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	adapterHTTP "github.com/matrocheetos/telegram-monitor/internal/adapter/primary/http"
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
	if password == "wrongpassword" {
		return nil, "", domain.ErrInvalidCredentials
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
		CreatedAt: time.Now(),
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
	interest.Active = true
	interest.CreatedAt = time.Now()
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

type mockOfferHistoryUseCase struct {
	matchedProducts map[int64][]domain.UserMatchedProduct
	coupons         []domain.ExtractedCoupon
}

func (m *mockOfferHistoryUseCase) ListUserMatchedProducts(ctx context.Context, userID int64, searchQuery string, limit, offset int) ([]domain.UserMatchedProduct, int, error) {
	all := m.matchedProducts[userID]
	var filtered []domain.UserMatchedProduct
	query := strings.ToLower(searchQuery)
	for _, p := range all {
		if query == "" ||
			strings.Contains(strings.ToLower(p.MessageText), query) ||
			strings.Contains(strings.ToLower(p.InterestTerm), query) ||
			strings.Contains(strings.ToLower(p.ChannelTitle), query) ||
			strings.Contains(strings.ToLower(p.ChannelHandle), query) {
			filtered = append(filtered, p)
		}
	}
	total := len(filtered)
	if offset > len(filtered) {
		return nil, total, nil
	}
	end := offset + limit
	if end > len(filtered) {
		end = len(filtered)
	}
	return filtered[offset:end], total, nil
}

func (m *mockOfferHistoryUseCase) ListExtractedCoupons(ctx context.Context, searchQuery string, limit, offset int) ([]domain.ExtractedCoupon, int, error) {
	var filtered []domain.ExtractedCoupon
	query := strings.ToLower(searchQuery)
	for _, c := range m.coupons {
		if query == "" ||
			strings.Contains(strings.ToLower(c.Code), query) ||
			strings.Contains(strings.ToLower(c.StoreName), query) ||
			strings.Contains(strings.ToLower(c.MessageText), query) ||
			strings.Contains(strings.ToLower(c.ChannelTitle), query) {
			filtered = append(filtered, c)
		}
	}
	total := len(filtered)
	if offset > len(filtered) {
		return nil, total, nil
	}
	end := offset + limit
	if end > len(filtered) {
		end = len(filtered)
	}
	return filtered[offset:end], total, nil
}

func (m *mockOfferHistoryUseCase) RunRetentionCleanup(ctx context.Context) error {
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
	offerUC      *mockOfferHistoryUseCase
	regularUser  *domain.User
	adminUser    *domain.User
	regularToken string
	adminToken   string
}

func ptr[T any](v T) *T {
	return &v
}

func setupFixture(t *testing.T, needsSetup bool, tgConnected bool) *testFixture {
	t.Helper()

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
			"admin-session-token": adminUser,
			"user-session-token":  regUser,
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
			{
				ChatID:          1001,
				Title:           "Canal Ofertas",
				Username:        "canalofertas",
				MonitorProducts: true,
				MonitorCoupons:  false,
				UpdatedAt:       time.Now(),
			},
			{
				ChatID:          1002,
				Title:           "Canal Cupons",
				Username:        "canalcupons",
				MonitorProducts: false,
				MonitorCoupons:  true,
				UpdatedAt:       time.Now(),
			},
		},
	}
	interestUC := &mockInterestUseCase{
		interests: map[int64][]*domain.UserInterest{
			2: {
				{
					ID:             1,
					UserID:         2,
					RawTerm:        "switch oled",
					PositiveTokens: []string{"switch", "oled"},
					Active:         true,
					CreatedAt:      time.Now(),
				},
			},
		},
	}
	offerUC := &mockOfferHistoryUseCase{
		matchedProducts: map[int64][]domain.UserMatchedProduct{
			2: {
				{
					ID:             1,
					UserID:         2,
					InterestTerm:   "switch oled",
					ChannelTitle:   "Canal Ofertas",
					ChannelHandle:  "canalofertas",
					ExtractedPrice: ptr(1999.99),
					MessageText:    "Console Nintendo Switch OLED",
					OfferURL:       "https://amzn.to/test",
					CreatedAt:      time.Now(),
				},
			},
		},
		coupons: []domain.ExtractedCoupon{
			{
				ID:            1,
				Code:          "DESCONTO10",
				StoreName:     "Amazon",
				StoreURL:      "https://amazon.com.br",
				MessageText:   "Cupom 10% OFF: DESCONTO10",
				ChannelTitle:  "Canal Cupons",
				ChannelHandle: "canalcupons",
				CreatedAt:     time.Now(),
			},
		},
	}

	server, err := adapterHTTP.NewServer(
		adapterHTTP.Config{
			Port:               8080,
			NtfyURL:            "https://ntfy.sh",
			NtfyTopicSeparator: "-",
			CORSAllowedOrigins: "http://localhost:5173",
		},
		setupUC,
		authUC,
		userAdminUC,
		tgAuthUC,
		channelUC,
		interestUC,
		offerUC,
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
		offerUC:      offerUC,
		regularUser:  regUser,
		adminUser:    adminUser,
		regularToken: "user-session-token",
		adminToken:   "admin-session-token",
	}
}

//nolint:gosec // test helper creates mock session cookie
func newSessionCookie(val string) *http.Cookie {
	return &http.Cookie{
		Name:     "session_id",
		Value:    val,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	}
}

//nolint:gosec // test helper creates mock csrf cookie
func newCSRFCookie(val string) *http.Cookie {
	return &http.Cookie{
		Name:     "csrf_token",
		Value:    val,
		Path:     "/",
		HttpOnly: false,
		SameSite: http.SameSiteLaxMode,
	}
}

func newJSONRequest(ctx context.Context, method, target string, body any, sessionCookie *http.Cookie, csrfToken string) *http.Request {
	var bodyReader io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		bodyReader = bytes.NewReader(b)
	}
	req := httptest.NewRequestWithContext(ctx, method, target, bodyReader)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if sessionCookie != nil {
		req.AddCookie(sessionCookie)
	}
	if csrfToken != "" {
		req.AddCookie(newCSRFCookie(csrfToken))
		req.Header.Set("X-CSRF-Token", csrfToken)
	}
	return req
}

func TestHealth(t *testing.T) {
	ctx := context.Background()
	fix := setupFixture(t, false, true)

	req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/api/v1/health", nil)
	rec := httptest.NewRecorder()
	fix.server.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var resp map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode json: %v", err)
	}
	if resp["status"] != "ok" {
		t.Fatalf("expected status ok, got %v", resp["status"])
	}
}

func TestSwaggerUI(t *testing.T) {
	ctx := context.Background()
	fix := setupFixture(t, false, true)

	req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/swagger/index.html", nil)
	rec := httptest.NewRecorder()
	fix.server.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for swagger index.html, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "swagger-ui") {
		t.Fatalf("expected swagger-ui content in response")
	}
}

func TestFirstBootGuard(t *testing.T) {
	ctx := context.Background()
	fix := setupFixture(t, true, false)

	// 1. Session check on first boot
	req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/api/v1/auth/session", nil)
	rec := httptest.NewRecorder()
	fix.server.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var sessionResp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &sessionResp); err != nil {
		t.Fatalf("decode session json: %v", err)
	}
	if sessionResp["needsInitialSetup"] != true {
		t.Fatalf("expected needsInitialSetup=true, got %v", sessionResp["needsInitialSetup"])
	}
	csrfToken, ok := sessionResp["csrfToken"].(string)
	if !ok || csrfToken == "" {
		t.Fatalf("expected csrfToken string, got %v", sessionResp["csrfToken"])
	}

	// 2. Access to any other route without setup returns 403 setup_required
	otherReq := httptest.NewRequestWithContext(ctx, http.MethodGet, "/api/v1/dashboard", nil)
	otherRec := httptest.NewRecorder()
	fix.server.ServeHTTP(otherRec, otherReq)
	if otherRec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 on unconfigured dashboard, got %d", otherRec.Code)
	}

	// 3. Setup with password mismatch
	badSetupReq := newJSONRequest(ctx, http.MethodPost, "/api/v1/setup", map[string]string{
		"username":        "admin",
		"password":        "password123",
		"passwordConfirm": "different123",
	}, nil, csrfToken)
	badSetupRec := httptest.NewRecorder()
	fix.server.ServeHTTP(badSetupRec, badSetupReq)
	if badSetupRec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 on password mismatch, got %d", badSetupRec.Code)
	}

	// 4. Setup with valid passwords -> 201 Created
	setupReq := newJSONRequest(ctx, http.MethodPost, "/api/v1/setup", map[string]string{
		"username":        "admin",
		"password":        "password123",
		"passwordConfirm": "password123",
	}, nil, csrfToken)
	setupRec := httptest.NewRecorder()
	fix.server.ServeHTTP(setupRec, setupReq)

	if setupRec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", setupRec.Code, setupRec.Body.String())
	}

	// Verify session cookie was set
	cookies := setupRec.Result().Cookies()
	var sessionCookie *http.Cookie
	for _, c := range cookies {
		if c.Name == "session_id" {
			sessionCookie = c
			break
		}
	}
	if sessionCookie == nil {
		t.Fatalf("expected session_id cookie to be set")
	}

	// 5. Subsequent call to setup returns 403 setup_already_completed
	postSetupReq := newJSONRequest(ctx, http.MethodPost, "/api/v1/setup", map[string]string{
		"username":        "admin2",
		"password":        "password123",
		"passwordConfirm": "password123",
	}, nil, csrfToken)
	postSetupRec := httptest.NewRecorder()
	fix.server.ServeHTTP(postSetupRec, postSetupReq)
	if postSetupRec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 on setup already completed, got %d", postSetupRec.Code)
	}
}

func TestAuthWorkflow(t *testing.T) {
	ctx := context.Background()
	fix := setupFixture(t, false, true)

	//nolint:gosec // dummy token for testing
	csrfToken := "test-csrf-token"

	// 1. Invalid login
	badLoginReq := newJSONRequest(ctx, http.MethodPost, "/api/v1/auth/login", map[string]string{
		"username": "admin",
		"password": "wrongpassword",
	}, nil, csrfToken)
	badLoginRec := httptest.NewRecorder()
	fix.server.ServeHTTP(badLoginRec, badLoginReq)
	if badLoginRec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", badLoginRec.Code)
	}

	// 2. Valid login
	loginReq := newJSONRequest(ctx, http.MethodPost, "/api/v1/auth/login", map[string]string{
		"username": "admin",
		"password": "correct",
	}, nil, csrfToken)
	loginRec := httptest.NewRecorder()
	fix.server.ServeHTTP(loginRec, loginReq)
	if loginRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", loginRec.Code, loginRec.Body.String())
	}

	cookies := loginRec.Result().Cookies()
	var sessionCookie *http.Cookie
	for _, c := range cookies {
		if c.Name == "session_id" {
			sessionCookie = c
			break
		}
	}
	if sessionCookie == nil {
		t.Fatalf("expected session_id cookie to be set")
	}

	// 3. Check session with cookie
	sessionReq := newJSONRequest(ctx, http.MethodGet, "/api/v1/auth/session", nil, sessionCookie, "")
	sessionRec := httptest.NewRecorder()
	fix.server.ServeHTTP(sessionRec, sessionReq)
	if sessionRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", sessionRec.Code)
	}

	var sessResp map[string]any
	_ = json.Unmarshal(sessionRec.Body.Bytes(), &sessResp)
	if sessResp["authenticated"] != true {
		t.Fatalf("expected authenticated=true, got %v", sessResp["authenticated"])
	}

	// 4. Logout
	logoutReq := newJSONRequest(ctx, http.MethodPost, "/api/v1/auth/logout", nil, sessionCookie, csrfToken)
	logoutRec := httptest.NewRecorder()
	fix.server.ServeHTTP(logoutRec, logoutReq)
	if logoutRec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", logoutRec.Code)
	}
}

func TestRBAC(t *testing.T) {
	ctx := context.Background()
	fix := setupFixture(t, false, true)

	// 1. Unauthenticated request to protected route -> 401
	unauthReq := httptest.NewRequestWithContext(ctx, http.MethodGet, "/api/v1/dashboard", nil)
	unauthRec := httptest.NewRecorder()
	fix.server.ServeHTTP(unauthRec, unauthReq)
	if unauthRec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", unauthRec.Code)
	}

	userCookie := newSessionCookie(fix.regularToken)
	adminCookie := newSessionCookie(fix.adminToken)

	// 2. Regular user access to dashboard -> 200
	userDashReq := newJSONRequest(ctx, http.MethodGet, "/api/v1/dashboard", nil, userCookie, "")
	userDashRec := httptest.NewRecorder()
	fix.server.ServeHTTP(userDashRec, userDashReq)
	if userDashRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", userDashRec.Code)
	}

	// 3. Regular user access to admin routes -> 403
	adminEndpoints := []string{
		"/api/v1/admin/telegram",
		"/api/v1/admin/channels",
		"/api/v1/admin/users",
	}
	for _, ep := range adminEndpoints {
		req := newJSONRequest(ctx, http.MethodGet, ep, nil, userCookie, "")
		rec := httptest.NewRecorder()
		fix.server.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("expected 403 on %s for regular user, got %d", ep, rec.Code)
		}
	}

	// 4. Admin user access to admin routes -> 200
	for _, ep := range adminEndpoints {
		req := newJSONRequest(ctx, http.MethodGet, ep, nil, adminCookie, "")
		rec := httptest.NewRecorder()
		fix.server.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 on %s for admin user, got %d", ep, rec.Code)
		}
	}
}

func TestCSRFProtection(t *testing.T) {
	ctx := context.Background()
	fix := setupFixture(t, false, true)
	adminCookie := newSessionCookie(fix.adminToken)

	// Mutating request without CSRF -> 403
	reqWithoutCSRF := httptest.NewRequestWithContext(ctx, http.MethodPost, "/api/v1/interests", strings.NewReader(`{"term":"tv"}`))
	reqWithoutCSRF.Header.Set("Content-Type", "application/json")
	reqWithoutCSRF.AddCookie(adminCookie)
	recWithoutCSRF := httptest.NewRecorder()
	fix.server.ServeHTTP(recWithoutCSRF, reqWithoutCSRF)
	if recWithoutCSRF.Code != http.StatusForbidden {
		t.Fatalf("expected 403 without CSRF token, got %d", recWithoutCSRF.Code)
	}

	// Mutating request with mismatched CSRF token -> 403
	reqMismatched := httptest.NewRequestWithContext(ctx, http.MethodPost, "/api/v1/interests", strings.NewReader(`{"term":"tv"}`))
	reqMismatched.Header.Set("Content-Type", "application/json")
	reqMismatched.AddCookie(adminCookie)
	reqMismatched.AddCookie(newCSRFCookie("token-a"))
	reqMismatched.Header.Set("X-CSRF-Token", "token-b")
	recMismatched := httptest.NewRecorder()
	fix.server.ServeHTTP(recMismatched, reqMismatched)
	if recMismatched.Code != http.StatusForbidden {
		t.Fatalf("expected 403 with mismatched CSRF token, got %d", recMismatched.Code)
	}

	// Mutating request with valid CSRF token -> 201 Created
	reqValid := newJSONRequest(ctx, http.MethodPost, "/api/v1/interests", map[string]string{
		"term": "nintendo switch",
	}, adminCookie, "valid-token")
	recValid := httptest.NewRecorder()
	fix.server.ServeHTTP(recValid, reqValid)
	if recValid.Code != http.StatusCreated {
		t.Fatalf("expected 201 with valid CSRF token, got %d: %s", recValid.Code, recValid.Body.String())
	}
}

func TestCORS(t *testing.T) {
	ctx := context.Background()
	fix := setupFixture(t, false, true)

	// Preflight OPTIONS request
	optReq := httptest.NewRequestWithContext(ctx, http.MethodOptions, "/api/v1/interests", nil)
	optReq.Header.Set("Origin", "http://localhost:5173")
	optRec := httptest.NewRecorder()
	fix.server.ServeHTTP(optRec, optReq)

	if optRec.Code != http.StatusNoContent {
		t.Fatalf("expected 204 on OPTIONS preflight, got %d", optRec.Code)
	}
	if optRec.Header().Get("Access-Control-Allow-Origin") != "http://localhost:5173" {
		t.Fatalf("unexpected origin header: %s", optRec.Header().Get("Access-Control-Allow-Origin"))
	}
	if optRec.Header().Get("Access-Control-Allow-Credentials") != "true" {
		t.Fatalf("expected credentials header to be true")
	}
}

func TestInterestsCRUD(t *testing.T) {
	ctx := context.Background()
	fix := setupFixture(t, false, true)
	userCookie := newSessionCookie(fix.regularToken)
	csrfToken := "test-token"

	// 1. List interests
	listReq := newJSONRequest(ctx, http.MethodGet, "/api/v1/interests", nil, userCookie, "")
	listRec := httptest.NewRecorder()
	fix.server.ServeHTTP(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", listRec.Code)
	}

	var list []map[string]any
	if err := json.Unmarshal(listRec.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 interest, got %d", len(list))
	}

	// 2. Add interest
	addReq := newJSONRequest(ctx, http.MethodPost, "/api/v1/interests", map[string]any{
		"term":     "ps5 pro",
		"maxPrice": 6500.0,
	}, userCookie, csrfToken)
	addRec := httptest.NewRecorder()
	fix.server.ServeHTTP(addRec, addReq)
	if addRec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", addRec.Code, addRec.Body.String())
	}

	// 3. Add interest with empty term -> 400
	badAddReq := newJSONRequest(ctx, http.MethodPost, "/api/v1/interests", map[string]string{
		"term": "   ",
	}, userCookie, csrfToken)
	badAddRec := httptest.NewRecorder()
	fix.server.ServeHTTP(badAddRec, badAddReq)
	if badAddRec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 on empty term, got %d", badAddRec.Code)
	}

	// 4. Delete interest
	delReq := newJSONRequest(ctx, http.MethodDelete, "/api/v1/interests/1", nil, userCookie, csrfToken)
	delRec := httptest.NewRecorder()
	fix.server.ServeHTTP(delRec, delReq)
	if delRec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", delRec.Code)
	}
}

func TestOffersEndpoint(t *testing.T) {
	ctx := context.Background()
	fix := setupFixture(t, false, true)
	userCookie := newSessionCookie(fix.regularToken)

	req := newJSONRequest(ctx, http.MethodGet, "/api/v1/offers?q=switch", nil, userCookie, "")
	rec := httptest.NewRecorder()
	fix.server.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode offers json: %v", err)
	}

	products, ok := resp["products"].([]any)
	if !ok || len(products) != 1 {
		t.Fatalf("expected 1 product, got %v", resp["products"])
	}
}

func TestAdminOperations(t *testing.T) {
	ctx := context.Background()
	fix := setupFixture(t, false, true)
	adminCookie := newSessionCookie(fix.adminToken)
	csrfToken := "test-token"

	// 1. Telegram status
	tgReq := newJSONRequest(ctx, http.MethodGet, "/api/v1/admin/telegram", nil, adminCookie, "")
	tgRec := httptest.NewRecorder()
	fix.server.ServeHTTP(tgRec, tgReq)
	if tgRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", tgRec.Code)
	}

	// 2. Telegram send code
	sendCodeReq := newJSONRequest(ctx, http.MethodPost, "/api/v1/admin/telegram/send-code", map[string]any{
		"apiId":   123456,
		"apiHash": "abcdef",
		"phone":   "+5511999999999",
	}, adminCookie, csrfToken)
	sendCodeRec := httptest.NewRecorder()
	fix.server.ServeHTTP(sendCodeRec, sendCodeReq)
	if sendCodeRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", sendCodeRec.Code)
	}

	// 3. Telegram verify
	verifyReq := newJSONRequest(ctx, http.MethodPost, "/api/v1/admin/telegram/verify", map[string]string{
		"phoneCode": "12345",
	}, adminCookie, csrfToken)
	verifyRec := httptest.NewRecorder()
	fix.server.ServeHTTP(verifyRec, verifyReq)
	if verifyRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", verifyRec.Code)
	}

	// 4. Channels list & update
	chListReq := newJSONRequest(ctx, http.MethodGet, "/api/v1/admin/channels", nil, adminCookie, "")
	chListRec := httptest.NewRecorder()
	fix.server.ServeHTTP(chListRec, chListReq)
	if chListRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", chListRec.Code)
	}

	chUpdateReq := newJSONRequest(ctx, http.MethodPut, "/api/v1/admin/channels", map[string]any{
		"channels": []map[string]any{
			{"chatId": 1001, "monitorProducts": false, "monitorCoupons": true},
		},
	}, adminCookie, csrfToken)
	chUpdateRec := httptest.NewRecorder()
	fix.server.ServeHTTP(chUpdateRec, chUpdateReq)
	if chUpdateRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", chUpdateRec.Code)
	}

	// 5. Users CRUD
	usersListReq := newJSONRequest(ctx, http.MethodGet, "/api/v1/admin/users", nil, adminCookie, "")
	usersListRec := httptest.NewRecorder()
	fix.server.ServeHTTP(usersListRec, usersListReq)
	if usersListRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", usersListRec.Code)
	}

	userCreateReq := newJSONRequest(ctx, http.MethodPost, "/api/v1/admin/users", map[string]string{
		"username": "novousuario",
		"password": "password123",
		"role":     "user",
	}, adminCookie, csrfToken)
	userCreateRec := httptest.NewRecorder()
	fix.server.ServeHTTP(userCreateRec, userCreateReq)
	if userCreateRec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d", userCreateRec.Code)
	}

	userPassReq := newJSONRequest(ctx, http.MethodPut, "/api/v1/admin/users/2/password", map[string]string{
		"password": "newpassword123",
	}, adminCookie, csrfToken)
	userPassRec := httptest.NewRecorder()
	fix.server.ServeHTTP(userPassRec, userPassReq)
	if userPassRec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", userPassRec.Code)
	}

	// Cannot delete self
	delSelfReq := newJSONRequest(ctx, http.MethodDelete, "/api/v1/admin/users/1", nil, adminCookie, csrfToken)
	delSelfRec := httptest.NewRecorder()
	fix.server.ServeHTTP(delSelfRec, delSelfReq)
	if delSelfRec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 when deleting self, got %d", delSelfRec.Code)
	}

	// Delete other user
	delUserReq := newJSONRequest(ctx, http.MethodDelete, "/api/v1/admin/users/2", nil, adminCookie, csrfToken)
	delUserRec := httptest.NewRecorder()
	fix.server.ServeHTTP(delUserRec, delUserReq)
	if delUserRec.Code != http.StatusNoContent {
		t.Fatalf("expected 204 when deleting other user, got %d", delUserRec.Code)
	}
}

func TestDashboard_NtfyTopicPrefix(t *testing.T) {
	ctx := context.Background()
	fix := setupFixture(t, false, true)

	server, err := adapterHTTP.NewServer(
		adapterHTTP.Config{
			Port:               8080,
			NtfyURL:            "https://ntfy.sh",
			NtfyTopicSeparator: "-",
			NtfyTopicPrefix:    "test-",
			CORSAllowedOrigins: "http://localhost:5173",
		},
		fix.setupUC,
		fix.authUC,
		fix.userAdminUC,
		fix.tgAuthUC,
		fix.channelUC,
		fix.interestUC,
		fix.offerUC,
	)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	userCookie := newSessionCookie(fix.regularToken)
	req := newJSONRequest(ctx, http.MethodGet, "/api/v1/dashboard", nil, userCookie, "")
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var resp adapterHTTP.DashboardResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal dashboard: %v", err)
	}

	if resp.UserNormalizedTopic != "test-promo-user-mateus" {
		t.Errorf("expected test-promo-user-mateus, got %s", resp.UserNormalizedTopic)
	}
	if resp.CouponsTopic != "test-promo/cupom" {
		t.Errorf("expected test-promo/cupom, got %s", resp.CouponsTopic)
	}
	if !strings.Contains(resp.UserPublicURL, "test-promo-user-mateus") {
		t.Errorf("expected test-promo-user-mateus in url, got %s", resp.UserPublicURL)
	}
}
