package worker

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/AleksMa/quote_service/internal/domain"
	"github.com/AleksMa/quote_service/internal/provider"
	"github.com/AleksMa/quote_service/internal/storage"
)

type Options struct {
	Interval    time.Duration
	Concurrency int
	Logger      *slog.Logger
}

type Worker struct {
	store       storage.WorkerStore
	provider    provider.Client
	interval    time.Duration
	concurrency int
	logger      *slog.Logger
}

func New(store storage.WorkerStore, provider provider.Client, options Options) *Worker {
	logger := options.Logger
	if logger == nil {
		logger = slog.Default()
	}
	if options.Interval <= 0 {
		options.Interval = 2 * time.Second
	}
	if options.Concurrency <= 0 {
		options.Concurrency = 1
	}

	return &Worker{
		store:       store,
		provider:    provider,
		interval:    options.Interval,
		concurrency: options.Concurrency,
		logger:      logger,
	}
}

func (w *Worker) Run(ctx context.Context) {
	w.processBatch(ctx)

	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.processBatch(ctx)
		}
	}
}

func (w *Worker) processBatch(ctx context.Context) {
	requests, err := w.store.ClaimPending(ctx, w.concurrency)
	if err != nil {
		w.logger.Error("claim quote update requests", "error", err)
		return
	}
	if len(requests) == 0 {
		return
	}

	var wg sync.WaitGroup
	wg.Add(len(requests))
	for _, request := range requests {
		request := request
		go func() {
			defer wg.Done()
			w.processOne(ctx, request)
		}()
	}
	wg.Wait()
}

func (w *Worker) processOne(ctx context.Context, request domain.UpdateRequest) {
	rate, err := w.provider.FetchRate(ctx, request.Pair)
	if err != nil {
		if markErr := w.store.MarkFailed(ctx, request.ID, err.Error(), time.Now().UTC()); markErr != nil {
			w.logger.Error("mark quote update failed", "request_id", request.ID, "error", markErr)
		}
		return
	}

	if err := w.store.MarkSucceeded(ctx, request.ID, rate.Price, rate.Provider, rate.FetchedAt); err != nil {
		w.logger.Error("mark quote update succeeded", "request_id", request.ID, "error", err)
		return
	}
	w.logger.Info("quote update completed", "request_id", request.ID, "pair", request.Pair.Raw, "provider", rate.Provider)
}
