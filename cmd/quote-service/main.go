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

	"github.com/AleksMa/quote_service/internal/application"
	"github.com/AleksMa/quote_service/internal/config"
	"github.com/AleksMa/quote_service/internal/httpapi"
	"github.com/AleksMa/quote_service/internal/provider"
	"github.com/AleksMa/quote_service/internal/storage/postgres"
	"github.com/AleksMa/quote_service/internal/worker"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	quoteStore, err := postgres.Open(ctx, cfg.Database.URL)
	if err != nil {
		return fmt.Errorf("open storage: %w", err)
	}
	defer quoteStore.Close()

	rateProvider, err := provider.BuildChain(cfg.Providers, logger)
	if err != nil {
		return fmt.Errorf("build provider chain: %w", err)
	}
	quoteService := application.NewQuoteService(quoteStore, cfg.SupportedPairs)
	quoteProcessor := application.NewProcessor(quoteStore, rateProvider, logger)
	if err := quoteProcessor.RequeueProcessing(ctx); err != nil {
		return err
	}
	backgroundWorker := worker.New(quoteProcessor, worker.Options{
		Interval:    cfg.Worker.Interval,
		ClaimLimit:  cfg.Worker.ClaimLimit,
		Concurrency: cfg.Worker.Concurrency,
		Logger:      logger,
	})
	go backgroundWorker.Run(ctx)

	server := &http.Server{
		Addr: cfg.HTTP.Addr,
		Handler: httpapi.New(
			quoteService,
			httpapi.Options{SwaggerUIOrigin: cfg.HTTP.SwaggerUIOrigin},
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
			return fmt.Errorf("http server failed: %w", err)
		}
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("http server shutdown: %w", err)
	}
	logger.Info("http server stopped")
	return nil
}
