package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	httpAdapter "github.com/matrocheetos/telegram-monitor/internal/adapter/primary/http"
	"github.com/matrocheetos/telegram-monitor/internal/adapter/secondary/apprise"
	"github.com/matrocheetos/telegram-monitor/internal/adapter/secondary/crypto"
	"github.com/matrocheetos/telegram-monitor/internal/adapter/secondary/gotd"
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
	debugFlag := flag.Bool("debug", false, "Enable debug structured logging")
	flag.Parse()

	logLevel := slog.LevelInfo
	if *debugFlag || os.Getenv("DEBUG") == "1" || os.Getenv("DEBUG") == "true" {
		logLevel = slog.LevelDebug
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: logLevel,
	}))
	slog.SetDefault(logger)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

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

	appriseURL := os.Getenv("APPRISE_URL")
	if appriseURL == "" {
		appriseURL = "http://localhost:8000"
	}
	ntfyURL := os.Getenv("NTFY_URL")
	if ntfyURL == "" {
		ntfyURL = "https://ntfy.sh"
	}
	ntfyTopicSep := os.Getenv("NTFY_TOPIC_SEPARATOR")
	if ntfyTopicSep == "" {
		ntfyTopicSep = "-"
	}

	appriseClient := apprise.NewClient(apprise.Config{
		AppriseURL:         appriseURL,
		AppriseKey:         os.Getenv("APPRISE_KEY"),
		NtfyURL:            ntfyURL,
		NtfyTopicSeparator: ntfyTopicSep,
	})

	monitorSvc := service.NewMonitorService(
		userRepo,
		interestRepo,
		channelRepo,
		matchRepo,
		couponRepo,
		appriseClient,
		service.MonitorConfig{
			WorkerCount: 4,
			QueueSize:   1024,
		},
	)
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
