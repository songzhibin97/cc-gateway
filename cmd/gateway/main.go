package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/songzhibin97/cc-gateway/internal/accounting"
	"github.com/songzhibin97/cc-gateway/internal/admin"
	"github.com/songzhibin97/cc-gateway/internal/config"
	"github.com/songzhibin97/cc-gateway/internal/domain"
	"github.com/songzhibin97/cc-gateway/internal/proxy"
	"github.com/songzhibin97/cc-gateway/internal/router"
	"github.com/songzhibin97/cc-gateway/internal/store"
)

func main() {
	configPath := flag.String("config", "config.yaml", "path to config file")
	flag.Parse()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	cfg, err := config.Load(*configPath)
	if err != nil {
		logger.Error("failed to load config", slog.String("path", *configPath), slog.String("error", err.Error()))
		os.Exit(1)
	}

	db, err := store.Open(cfg.Database.Path)
	if err != nil {
		logger.Error("failed to open database", slog.String("path", cfg.Database.Path), slog.String("error", err.Error()))
		os.Exit(1)
	}

	logRepo := store.NewLogRepo(db)
	accountRepo := store.NewAccountRepo(db)
	groupRepo := store.NewGroupRepo(db)
	keyRepo := store.NewAPIKeyRepo(db)
	recorder := accounting.NewRecorder(logRepo, logger)
	cleanupInterval, err := time.ParseDuration(cfg.Log.CleanupInterval)
	if err != nil || cleanupInterval <= 0 {
		cleanupInterval = time.Hour
	}
	cleaner := accounting.NewCleaner(logRepo, cfg.Log.PayloadRetentionDays, cleanupInterval, logger)

	costCalc := accounting.NewCostCalculator(cfg.Pricing)

	startupCtx := context.Background()
	accounts, err := accountRepo.List(startupCtx)
	if err != nil {
		logger.Error("failed to load accounts", slog.String("error", err.Error()))
		os.Exit(1)
	}
	groups, err := groupRepo.List(startupCtx)
	if err != nil {
		logger.Error("failed to load groups", slog.String("error", err.Error()))
		os.Exit(1)
	}
	keys, err := keyRepo.List(startupCtx)
	if err != nil {
		logger.Error("failed to load api keys", slog.String("error", err.Error()))
		os.Exit(1)
	}

	rtr := router.New(accounts, groups, logger)
	keyStore := proxy.NewKeyStoreFromData(keys, groups, accounts, keyRepo)
	handler := proxy.NewHandler(rtr, keyStore, recorder, costCalc, logger)

	reloadFn := func() error {
		ctx := context.Background()

		accounts, err := accountRepo.List(ctx)
		if err != nil {
			return fmt.Errorf("list accounts: %w", err)
		}
		groups, err := groupRepo.List(ctx)
		if err != nil {
			return fmt.Errorf("list groups: %w", err)
		}
		keys, err := keyRepo.List(ctx)
		if err != nil {
			return fmt.Errorf("list api keys: %w", err)
		}

		rtr.Reload(accounts, groups)
		keyStore.Reload(keys, groups, accounts)
		logger.Info("configuration reloaded",
			slog.Int("accounts", len(accounts)),
			slog.Int("groups", len(groups)),
			slog.Int("keys", len(keys)),
		)
		return nil
	}

	router := chi.NewRouter()
	router.Use(middleware.RequestID)
	router.Use(middleware.RealIP)
	router.Use(middleware.Recoverer)

	router.Get("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	router.Post("/v1/messages", handler.ServeHTTP)

	server := &http.Server{
		Addr:         cfg.Server.Listen,
		Handler:      router,
		ReadTimeout:  mustParseDuration(cfg.Server.ReadTimeout),
		WriteTimeout: mustParseDuration(cfg.Server.WriteTimeout),
		IdleTimeout:  mustParseDuration(cfg.Server.IdleTimeout),
	}

	adminSrv := admin.NewServer(admin.Config{
		Router:      rtr,
		KeyStore:    keyStore,
		AccountRepo: accountRepo,
		GroupRepo:   groupRepo,
		KeyRepo:     keyRepo,
		LogRepo:     logRepo,
		Cleaner:     cleaner,
		ReloadFn:    reloadFn,
		AdminToken:  os.Getenv("ADMIN_TOKEN"),
		Logger:      logger,
	})

	adminHTTP := &http.Server{
		Addr:    cfg.Server.AdminListen,
		Handler: adminSrv.Handler(),
	}

	logger.Info("starting cc-gateway",
		slog.String("listen", cfg.Server.Listen),
		slog.String("admin_listen", cfg.Server.AdminListen),
		slog.Int("accounts", len(accounts)),
		slog.Int("groups", len(groups)),
		slog.Int("api_keys", len(keys)),
	)
	if len(accounts) == 0 && len(groups) == 0 && len(keys) == 0 {
		logger.Info("database is empty; create accounts, groups, and api keys via the Admin API")
	}

	var (
		cfgMu        sync.Mutex
		currentCfg   = cfg
		shutdownOnce sync.Once
	)

	go func() {
		sighup := make(chan os.Signal, 1)
		signal.Notify(sighup, syscall.SIGHUP)
		defer signal.Stop(sighup)

		for range sighup {
			logger.Info("received SIGHUP, reloading config")

			newCfg, err := config.Load(*configPath)
			if err != nil {
				logger.Error("config reload failed", slog.String("error", err.Error()))
				continue
			}

			cfgMu.Lock()
			oldCfg := currentCfg
			currentCfg = newCfg
			cfgMu.Unlock()

			costCalc.UpdatePricing(newCfg.Pricing)
			logger.Info("config reloaded",
				slog.Int("pricing_rules", len(newCfg.Pricing)),
				slog.Any("pricing_added", pricingPatternsAdded(oldCfg.Pricing, newCfg.Pricing)),
				slog.Any("pricing_removed", pricingPatternsAdded(newCfg.Pricing, oldCfg.Pricing)),
				slog.Any("pricing_updated", pricingPatternsUpdated(oldCfg.Pricing, newCfg.Pricing)),
			)
		}
	}()

	serverErr := make(chan error, 2)
	shutdown := func(reason string, attrs ...any) {
		shutdownOnce.Do(func() {
			started := time.Now()
			logAttrs := append([]any{slog.String("reason", reason)}, attrs...)
			logger.Info("shutting down", logAttrs...)

			shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			if err := server.Shutdown(shutdownCtx); err != nil {
				logger.Error("shutdown failed", slog.String("error", err.Error()))
			}
			if err := adminHTTP.Shutdown(shutdownCtx); err != nil {
				logger.Error("admin shutdown failed", slog.String("error", err.Error()))
			}

			recorder.Close()
			cleaner.Stop()
			if err := db.Close(); err != nil {
				logger.Error("database close failed", slog.String("error", err.Error()))
			}

			logger.Info("shutdown complete", slog.Duration("duration", time.Since(started)))
		})
	}

	go func() {
		logger.Info("admin server starting", slog.String("addr", cfg.Server.AdminListen))
		if err := adminHTTP.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			serverErr <- err
			return
		}
		serverErr <- nil
	}()

	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			serverErr <- err
			return
		}
		serverErr <- nil
	}()

	stopSignals := make(chan os.Signal, 1)
	signal.Notify(stopSignals, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(stopSignals)

	var exitCode int
	receivedServerResults := 0

	select {
	case sig := <-stopSignals:
		shutdown("signal", slog.String("signal", sig.String()))
	case err := <-serverErr:
		receivedServerResults++
		if err != nil {
			logger.Error("server failed", slog.String("error", err.Error()))
			exitCode = 1
			shutdown("server_error", slog.String("error", err.Error()))
		}
	}

	for ; receivedServerResults < 2; receivedServerResults++ {
		if err := <-serverErr; err != nil {
			logger.Error("server failed", slog.String("error", err.Error()))
			exitCode = 1
			shutdown("server_error", slog.String("error", err.Error()))
		}
	}

	logger.Info("server stopped")
	if exitCode != 0 {
		os.Exit(exitCode)
	}
}

func mustParseDuration(raw string) time.Duration {
	d, err := time.ParseDuration(raw)
	if err != nil {
		panic(err)
	}
	return d
}

func pricingPatternsAdded(oldPricing, newPricing []domain.ModelPricing) []string {
	oldPatterns := make(map[string]struct{}, len(oldPricing))
	for _, item := range oldPricing {
		oldPatterns[item.ModelPattern] = struct{}{}
	}

	added := make([]string, 0)
	for _, item := range newPricing {
		if _, ok := oldPatterns[item.ModelPattern]; ok {
			continue
		}
		added = append(added, item.ModelPattern)
	}
	return added
}

func pricingPatternsUpdated(oldPricing, newPricing []domain.ModelPricing) []string {
	oldByPattern := make(map[string]domain.ModelPricing, len(oldPricing))
	for _, item := range oldPricing {
		oldByPattern[item.ModelPattern] = item
	}

	updated := make([]string, 0)
	for _, item := range newPricing {
		oldItem, ok := oldByPattern[item.ModelPattern]
		if !ok {
			continue
		}
		if oldItem.InputPricePerM == item.InputPricePerM &&
			oldItem.OutputPricePerM == item.OutputPricePerM &&
			oldItem.ThinkingPricePerM == item.ThinkingPricePerM {
			continue
		}
		updated = append(updated, item.ModelPattern)
	}
	return updated
}
