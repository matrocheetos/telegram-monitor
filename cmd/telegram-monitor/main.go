package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	httpAdapter "github.com/matrocheetos/telegram-monitor/internal/adapter/primary/http"
	"github.com/matrocheetos/telegram-monitor/internal/adapter/secondary/crypto"
	"github.com/matrocheetos/telegram-monitor/internal/adapter/secondary/gotd"
	"github.com/matrocheetos/telegram-monitor/internal/adapter/secondary/ntfy"
	"github.com/matrocheetos/telegram-monitor/internal/adapter/secondary/sqlite"
	"github.com/matrocheetos/telegram-monitor/internal/service"
)

// @title           Telegram Monitor REST API
// @version         1.0
// @description     Headless JSON REST API for Telegram Monitor, providing MTProto userbot connection, multi-rule channel product and coupon monitoring, and user management.
// @host            localhost:8080
// @BasePath        /api/v1
// @schemes         http https

// @securityDefinitions.apikey CookieAuth
// @in cookie
// @name session_id
// @description Session authentication cookie.

// @securityDefinitions.apikey CSRFToken
// @in header
// @name X-CSRF-Token
// @description Double-submit CSRF token header for mutating requests.

func main() {
	if err := run(); err != nil {
		slog.Error("daemon terminated with error", "error", err)
		os.Exit(1)
	}
}

func run() error {
	dotEnvLoaded, dotEnvErr := loadDotEnv()

	debugFlag := flag.Bool("debug", false, "Enable debug structured logging and local message collection")
	flag.Parse()

	debugMode := *debugFlag || os.Getenv("DEBUG") == "1" || os.Getenv("DEBUG") == "true" || os.Getenv("DEBUG_COLLECT_MESSAGES") == "true"

	logLevel := slog.LevelInfo
	if debugMode {
		logLevel = slog.LevelDebug
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: logLevel,
	}))
	slog.SetDefault(logger)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if dotEnvErr != nil {
		slog.WarnContext(ctx, "failed to load .env file", "error", dotEnvErr)
	} else if dotEnvLoaded {
		slog.DebugContext(ctx, "loaded environment variables from .env")
	}

	slog.InfoContext(ctx, "starting telegram-monitor daemon")

	secretKey := os.Getenv("APP_SECRET_KEY")
	if secretKey == "" {
		secretKey = "telegram-monitor-dev-insecure-secret-key-change-in-prod"
		slog.WarnContext(ctx, "APP_SECRET_KEY environment variable is not set; using development fallback key")
	}

	dbPath := os.Getenv("DB_PATH")
	if dbPath == "" {
		dbPath = "./data/telegram-monitor.db"
	}

	if dir := filepath.Dir(dbPath); dir != "" && dir != "." {
		cleanDir := filepath.Clean(dir)
		//nolint:gosec // database directory path is from trusted configuration
		if err := os.MkdirAll(cleanDir, 0750); err != nil {
			return fmt.Errorf("create database directory %s: %w", cleanDir, err)
		}
	}

	db, err := sqlite.OpenDB(ctx, dbPath)
	if err != nil {
		return fmt.Errorf("open sqlite database: %w", err)
	}
	defer func() {
		_ = db.Close()
	}()

	cryptoService, err := crypto.NewAESGCMCrypto(secretKey)
	if err != nil {
		return fmt.Errorf("initialize crypto service: %w", err)
	}
	hasher := crypto.NewArgon2idHasher()

	userRepo := sqlite.NewUserRepository(db)
	sessionRepo := sqlite.NewSessionRepository(db)
	credRepo := sqlite.NewCredentialRepository(db)
	channelRepo := sqlite.NewChannelRepository(db)
	interestRepo := sqlite.NewInterestRepository(db)
	matchRepo := sqlite.NewMatchHistoryRepository(db)
	couponRepo := sqlite.NewCouponRepository(db)

	ntfyURL := os.Getenv("NTFY_URL")
	if ntfyURL == "" {
		ntfyURL = "https://ntfy.sh"
	}
	ntfyToken := os.Getenv("NTFY_TOKEN")
	ntfyUsername := os.Getenv("NTFY_USERNAME")
	ntfyPassword := os.Getenv("NTFY_PASSWORD")
	ntfyTopicSep := os.Getenv("NTFY_TOPIC_SEPARATOR")
	if ntfyTopicSep == "" {
		ntfyTopicSep = "-"
	}
	ntfyTopicPrefix := os.Getenv("NTFY_TOPIC_PREFIX")
	appEnv := strings.ToLower(os.Getenv("APP_ENV"))
	if ntfyTopicPrefix == "" && (appEnv == "development" || appEnv == "local" || appEnv == "dev" || appEnv == "test") {
		ntfyTopicPrefix = "test-"
	}

	ntfyClient := ntfy.NewClient(ntfy.Config{
		BaseURL:        ntfyURL,
		Token:          ntfyToken,
		Username:       ntfyUsername,
		Password:       ntfyPassword,
		TopicSeparator: ntfyTopicSep,
		TopicPrefix:    ntfyTopicPrefix,
	})

	monitorSvc := service.NewMonitorService(
		userRepo,
		interestRepo,
		channelRepo,
		matchRepo,
		couponRepo,
		ntfyClient,
		service.MonitorConfig{
			WorkerCount: 4,
			QueueSize:   1024,
		},
	)
	if isLocalDebugCollectionEnabled(appEnv, debugMode) {
		debugRepo := sqlite.NewDebugMessageRepository(db)
		monitorSvc.SetDebugRepository(debugRepo)
		slog.InfoContext(ctx, "local telegram message debug collector enabled", "table", "debug_analyzed_messages")
	} else if debugMode {
		slog.WarnContext(ctx, "telegram message debug collection is blocked outside local development environment", "app_env", appEnv)
	}
	monitorSvc.Start(ctx)
	defer monitorSvc.Stop()

	if err := monitorSvc.RebuildSnapshot(ctx); err != nil {
		slog.WarnContext(ctx, "failed to build initial in-memory matcher snapshot", "error", err)
	}

	gotdManager := gotd.NewClientManager(credRepo, cryptoService, monitorSvc)

	if creds, err := credRepo.GetTelegramCredentials(ctx); err == nil && creds != nil && len(creds.EncryptedSession) > 0 {
		slog.InfoContext(ctx, "saved telegram session found; starting listener in background", "phone", creds.PhoneMasked)
		go func() {
			if err := gotdManager.StartListener(ctx); err != nil && !errors.Is(err, context.Canceled) {
				slog.ErrorContext(ctx, "telegram background listener failed", "error", err)
			}
		}()
	} else {
		slog.InfoContext(ctx, "no active telegram session found; awaiting web setup in /admin/telegram")
	}
	defer func() {
		_ = gotdManager.StopListener(context.Background())
	}()

	authSvc := service.NewAuthService(userRepo, sessionRepo, hasher, monitorSvc)
	tgAuthSvc := service.NewTelegramAuthService(gotdManager, credRepo)
	channelSvc := service.NewChannelService(channelRepo, gotdManager, monitorSvc)
	interestSvc := service.NewInterestService(interestRepo, monitorSvc)
	offerSvc := service.NewOfferService(matchRepo, couponRepo, service.OfferServiceConfig{
		RetentionDays: 90,
		MaxKeep:       100000,
		BatchSize:     1000,
	})
	offerSvc.StartRetentionJob(ctx, 1*time.Hour)
	defer offerSvc.Stop()

	port := 8080
	if pStr := os.Getenv("HTTP_PORT"); pStr != "" {
		if pVal, err := strconv.Atoi(pStr); err == nil && pVal > 0 {
			port = pVal
		}
	}

	server, err := httpAdapter.NewServer(
		httpAdapter.Config{
			Port:               port,
			NtfyURL:            ntfyURL,
			NtfyTopicSeparator: ntfyTopicSep,
			NtfyTopicPrefix:    ntfyTopicPrefix,
			CORSAllowedOrigins: os.Getenv("CORS_ALLOWED_ORIGINS"),
		},
		authSvc,
		authSvc,
		authSvc,
		tgAuthSvc,
		channelSvc,
		interestSvc,
		offerSvc,
	)
	if err != nil {
		return fmt.Errorf("create http adapter server: %w", err)
	}

	httpSrv := &http.Server{
		Addr:              fmt.Sprintf(":%d", server.Port()),
		Handler:           server,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	go func() {
		slog.InfoContext(ctx, "web server listening", "port", server.Port(), "url", fmt.Sprintf("http://localhost:%d", server.Port()))
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.ErrorContext(ctx, "http server fatal error", "error", err)
		}
	}()

	<-ctx.Done()
	slog.InfoContext(ctx, "shutting down telegram-monitor daemon...")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()

	if err := httpSrv.Shutdown(shutdownCtx); err != nil {
		slog.ErrorContext(ctx, "error shutting down http server", "error", err)
	}

	slog.InfoContext(ctx, "daemon stopped cleanly")
	return nil
}

func isLocalDebugCollectionEnabled(appEnv string, debugMode bool) bool {
	if !debugMode {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(appEnv)) {
	case "", "local", "development", "dev":
		return true
	default:
		return false
	}
}

func loadDotEnv() (bool, error) {
	f, err := os.Open(".env")
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	defer func() {
		_ = f.Close()
	}()

	if err := loadDotEnvFromReader(f); err != nil {
		return false, err
	}
	return true, nil
}

func loadDotEnvFromReader(r io.Reader) error {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		line = strings.TrimPrefix(line, "\xef\xbb\xbf")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")

		idx := strings.IndexByte(line, '=')
		if idx <= 0 {
			continue
		}

		key := strings.TrimSpace(line[:idx])
		if key == "" {
			continue
		}
		val := strings.TrimSpace(line[idx+1:])
		if len(val) >= 2 {
			if (val[0] == '"' && val[len(val)-1] == '"') || (val[0] == '\'' && val[len(val)-1] == '\'') {
				val = val[1 : len(val)-1]
			}
		}

		if _, exists := os.LookupEnv(key); exists {
			continue
		}
		if err := os.Setenv(key, val); err != nil {
			return fmt.Errorf("set env %s: %w", key, err)
		}
	}
	return scanner.Err()
}
