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

	"github.com/AleksMa/quote_service/internal/config"
	"github.com/AleksMa/quote_service/internal/httpapi"
	"github.com/AleksMa/quote_service/internal/provider"
	"github.com/AleksMa/quote_service/internal/storage"
	"github.com/AleksMa/quote_service/internal/worker"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	cfg, err := config.Load()
	if err != nil {
		logger.Error("load config", "error", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	quoteStore, err := storage.Open(ctx, storage.Config{
		Driver: cfg.Database.Driver,
		URL:    cfg.Database.URL,
	})
	if err != nil {
		logger.Error("open storage", "error", err)
		os.Exit(1)
	}
	defer quoteStore.Close()

	rateProvider, err := provider.BuildChain(cfg.Providers, logger)
	if err != nil {
		logger.Error("build provider chain", "error", err)
		os.Exit(1)
	}
	backgroundWorker := worker.New(quoteStore, rateProvider, worker.Options{
		Interval:    cfg.Worker.Interval,
		Concurrency: cfg.Worker.Concurrency,
		Logger:      logger,
	})
	go backgroundWorker.Run(ctx)

	server := &http.Server{
		Addr: cfg.HTTP.Addr,
		Handler: httpapi.New(
			quoteStore,
			cfg.SupportedPairs,
			httpapi.Options{CORSAllowedOrigins: cfg.HTTP.CORSAllowedOrigins},
		),
		ReadHeaderTimeout: 5 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		logger.Info("http server started", "addr", cfg.HTTP.Addr)
		errCh <- server.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("http server failed", "error", err)
			os.Exit(1)
		}
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error("http server shutdown", "error", err)
		os.Exit(1)
	}
	logger.Info("http server stopped")
}
