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
	ClaimLimit  int
	Concurrency int
	Logger      *slog.Logger
}

type Worker struct {
	store       storage.WorkerStore
	provider    provider.Client
	interval    time.Duration
	claimLimit  int
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
	if options.ClaimLimit <= 0 {
		options.ClaimLimit = 1
	}
	if options.Concurrency <= 0 {
		options.Concurrency = 1
	}

	return &Worker{
		store:       store,
		provider:    provider,
		interval:    options.Interval,
		claimLimit:  options.ClaimLimit,
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
	jobs, err := w.store.ClaimPending(ctx, w.claimLimit)
	if err != nil {
		w.logger.Error("claim quote update jobs", "error", err)
		return
	}
	if len(jobs) == 0 {
		return
	}

	workerCount := min(len(jobs), w.concurrency)

	jobCh := make(chan domain.UpdateJob)
	var wg sync.WaitGroup
	wg.Add(workerCount)
	for range workerCount {
		go func() {
			defer wg.Done()
			for job := range jobCh {
				w.processOne(ctx, job)
			}
		}()
	}

	for _, job := range jobs {
		select {
		case jobCh <- job:
		case <-ctx.Done():
			close(jobCh)
			wg.Wait()
			return
		}
	}
	close(jobCh)
	wg.Wait()
}

func (w *Worker) processOne(ctx context.Context, job domain.UpdateJob) {
	rate, err := w.provider.FetchRate(ctx, job.Pair)
	if err != nil {
		if markErr := w.store.MarkFailed(ctx, job.ID, err.Error(), time.Now().UTC()); markErr != nil {
			w.logger.Error("mark quote update job failed", "job_id", job.ID, "error", markErr)
		}
		return
	}

	if err := w.store.MarkSucceeded(ctx, job.ID, rate.Price, rate.Provider, rate.FetchedAt); err != nil {
		w.logger.Error("mark quote update job succeeded", "job_id", job.ID, "error", err)
		return
	}
	w.logger.Info("quote update job completed", "job_id", job.ID, "pair", job.Pair.Raw, "provider", rate.Provider)
}
