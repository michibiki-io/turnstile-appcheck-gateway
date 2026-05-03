package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
	_ "time/tzdata"

	firebase "firebase.google.com/go/v4"
	"google.golang.org/api/option"

	"github.com/michibiki-io/turnstile-appcheck-gateway/internal/audit"
	"github.com/michibiki-io/turnstile-appcheck-gateway/internal/auth"
	"github.com/michibiki-io/turnstile-appcheck-gateway/internal/config"
	"github.com/michibiki-io/turnstile-appcheck-gateway/internal/firebaseappcheck"
	"github.com/michibiki-io/turnstile-appcheck-gateway/internal/handlers"
	"github.com/michibiki-io/turnstile-appcheck-gateway/internal/health"
	"github.com/michibiki-io/turnstile-appcheck-gateway/internal/httpserver"
	"github.com/michibiki-io/turnstile-appcheck-gateway/internal/logging"
	"github.com/michibiki-io/turnstile-appcheck-gateway/internal/turnstile"
)

func main() {
	if err := run(); err != nil {
		slog.Error("appcheck service exited with error", "error", err.Error())
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	logger := logging.New(cfg.LogLevel)
	slog.SetDefault(logger)

	var auditRecorder audit.Recorder = audit.NoopRecorder{}
	var auditStore *audit.Store
	if cfg.Audit.Enabled {
		auditStore, err = audit.Open(context.Background(), cfg.Audit.SQLitePath)
		if err != nil {
			return fmt.Errorf("open audit store: %w", err)
		}
		defer func() { _ = auditStore.Close() }()
		if err := auditStore.PruneRetention(context.Background(), cfg.Audit.RetentionDays); err != nil {
			return fmt.Errorf("prune audit retention: %w", err)
		}
		auditRecorder = auditStore
		_ = auditRecorder.Record(context.Background(), audit.Event{
			Actor:       "system",
			ActorSource: "system",
			Action:      "system.startup",
			Result:      audit.ResultSuccess,
			Message:     "Application startup",
		})
	}

	normalizedCredsJSON, serviceAccount, err := auth.NormalizeJSON(cfg.ServiceAccountJSON)
	if err != nil {
		return fmt.Errorf("normalize service account json: %w", err)
	}
	privateKey, err := auth.ParseRSAPrivateKey(serviceAccount)
	if err != nil {
		return fmt.Errorf("parse service account key: %w", err)
	}

	ctx := context.Background()
	tokenSource, err := auth.NewTokenSource(ctx, normalizedCredsJSON,
		"https://www.googleapis.com/auth/firebase",
		"https://www.googleapis.com/auth/cloud-platform",
	)
	if err != nil {
		return fmt.Errorf("create oauth token source: %w", err)
	}

	outboundHTTPClient := &http.Client{Timeout: cfg.RequestTimeout}
	turnstileClient, err := turnstile.NewClient(outboundHTTPClient, cfg.TurnstileSecretKey, cfg.TurnstileSiteVerifyURL)
	if err != nil {
		return fmt.Errorf("create turnstile client: %w", err)
	}

	appCheckExchangeClient, err := firebaseappcheck.NewClient(
		outboundHTTPClient,
		tokenSource,
		privateKey,
		serviceAccount.ClientEmail,
		cfg.FirebaseAppResource,
		cfg.FirebaseAppID,
		firebaseappcheck.CustomTokenOptions{
			TTLMillis: ptrInt64(cfg.AppCheckTokenTTL.Milliseconds()),
		},
		logger,
	)
	if err != nil {
		return fmt.Errorf("create firebase appcheck exchanger: %w", err)
	}

	adminApp, err := firebase.NewApp(ctx, &firebase.Config{ProjectID: cfg.FirebaseProjectID}, option.WithCredentialsJSON(normalizedCredsJSON))
	if err != nil {
		return fmt.Errorf("initialize firebase admin app: %w", err)
	}

	appCheckVerifier, err := adminApp.AppCheck(ctx)
	if err != nil {
		return fmt.Errorf("initialize firebase app check verifier: %w", err)
	}

	exchangeHandler := &handlers.ExchangeHandler{
		Logger: logger,
		Timeout: func(parent context.Context) (context.Context, context.CancelFunc) {
			return context.WithTimeout(parent, cfg.RequestTimeout)
		},
		TurnstileVerifier: turnstileClient,
		Exchanger:         appCheckExchangeClient,
		TrustProxyHeaders: cfg.TrustProxyHeaders,
		OriginAllowed:     cfg.IsExchangeOriginAllowed,
		Audit:             auditRecorder,
	}
	verifyHandler := &handlers.VerifyHandler{
		Logger:        logger,
		Verifier:      appCheckVerifier,
		HeaderName:    cfg.VerifyHeaderName,
		SuccessStatus: cfg.VerifySuccessStatus,
		FailureStatus: cfg.VerifyFailureStatus,
		Audit:         auditRecorder,
	}

	router, err := httpserver.NewRouter(cfg, logger, exchangeHandler, verifyHandler, health.NewHandler(), auditRecorder)
	if err != nil {
		return fmt.Errorf("create router: %w", err)
	}

	httpSrv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           router,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		logger.Info("starting appcheck service", "addr", cfg.HTTPAddr, "subpath", cfg.AppCheckSubpath)
		errCh <- httpSrv.ListenAndServe()
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-sigCh:
		logger.Info("shutdown signal received", "signal", sig.String())
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("http server failed: %w", err)
		}
		return nil
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := httpSrv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("http shutdown failed: %w", err)
	}

	logger.Info("server shutdown complete")
	return nil
}

func ptrInt64(v int64) *int64 {
	return &v
}
