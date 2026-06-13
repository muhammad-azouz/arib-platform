// Command api runs the Arib license management HTTP server.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/aribpos/license-api/internal/admin"
	"github.com/aribpos/license-api/internal/auth"
	"github.com/aribpos/license-api/internal/config"
	"github.com/aribpos/license-api/internal/device"
	"github.com/aribpos/license-api/internal/httpapi"
	"github.com/aribpos/license-api/internal/license"
	"github.com/aribpos/license-api/internal/mail"
	"github.com/aribpos/license-api/internal/rollout"
	mongostore "github.com/aribpos/license-api/internal/store/mongo"
	"github.com/aribpos/license-api/internal/tenant"
	"github.com/aribpos/license-api/pkg/licensetoken"
	"github.com/joho/godotenv"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	// Load .env for local development (ignored if absent).
	_ = godotenv.Load()

	cfg, err := config.Load()
	if err != nil {
		log.Error("config", "err", err)
		os.Exit(1)
	}

	signer, err := licensetoken.NewSigner(cfg.PrivateKeyXML)
	if err != nil {
		log.Error("load signing key", "err", err)
		os.Exit(1)
	}

	ctx := context.Background()
	store, err := mongostore.Connect(ctx, cfg.MongoURI, cfg.MongoDB)
	if err != nil {
		log.Error("mongo connect", "err", err)
		os.Exit(1)
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = store.Close(shutdownCtx)
	}()
	if err := store.EnsureIndexes(ctx); err != nil {
		log.Error("ensure indexes", "err", err)
		os.Exit(1)
	}

	// --- services ---
	licenseSvc := license.New(store, signer, license.Clocks{
		RevalidateAfter: cfg.RevalidateAfter,
		HardExpireAfter: cfg.HardExpireAfter,
		TrialDuration:   cfg.TrialDuration,
	})
	deviceSvc := device.New(store, licenseSvc, device.CooldownPolicy{
		MinInterval: cfg.ReleaseCooldown,
		MaxPerMonth: cfg.ReleaseMaxPerMonth,
	})
	adminSvc := admin.New(store, licenseSvc)
	syncKey, err := licensetoken.ParsePrivateKeyXML(cfg.PrivateKeyXML)
	if err != nil {
		log.Error("parse sync signing key", "err", err)
		os.Exit(1)
	}
	tenantSvc := tenant.New(store, syncKey, cfg.SyncTokenTTL)
	rolloutSvc := rollout.New(store, tenantSvc, nil)

	mailer := mail.New(mail.Config{
		Host: cfg.SMTPHost, Port: cfg.SMTPPort,
		Username: cfg.SMTPUsername, Password: cfg.SMTPPassword, From: cfg.SMTPFrom,
	}, log)

	tokenMgr := auth.NewTokenManager(cfg.JWTSecret, cfg.AccessTokenTTL, cfg.RefreshTokenTTL)
	oauth := auth.NewOAuth(cfg.PublicBaseURL, cfg.JWTSecret,
		cfg.GoogleClientID, cfg.GoogleClientSecret,
		cfg.FacebookClientID, cfg.FacebookClientSecret)

	authSvc := auth.NewService(auth.Deps{
		Store: store, Tokens: tokenMgr, OAuth: oauth, Mail: mailer,
		Trials: licenseSvc, IsAdmin: cfg.IsAdmin,
		OTPTTL: cfg.OTPTTL, OTPMaxAttempts: cfg.OTPMaxAttempts,
	})

	srv := httpapi.New(authSvc, deviceSvc, adminSvc, tenantSvc, rolloutSvc, cfg.DashboardOrigins, log)

	httpServer := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           srv.Router(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	// --- run with graceful shutdown ---
	go func() {
		log.Info("listening", "addr", cfg.HTTPAddr, "public", cfg.PublicBaseURL)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("http server", "err", err)
			os.Exit(1)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	log.Info("shutting down")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		log.Error("shutdown", "err", err)
	}
}
