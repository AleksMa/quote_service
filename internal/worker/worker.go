package worker

import (
	"context"
	"log/slog"
	"time"
)

type Processor interface {
	ProcessPending(ctx context.Context, limit int, concurrency int) error
}

type Options struct {
	Interval    time.Duration
	ClaimLimit  int
	Concurrency int
	Logger      *slog.Logger
}

type Worker struct {
	processor   Processor
	interval    time.Duration
	claimLimit  int
	concurrency int
	logger      *slog.Logger
}

func New(processor Processor, options Options) *Worker {
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
		processor:   processor,
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
	if err := w.processor.ProcessPending(ctx, w.claimLimit, w.concurrency); err != nil {
		w.logger.Error("process pending quote update jobs", "error", err)
	}
}
