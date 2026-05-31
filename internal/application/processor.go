package application

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/AleksMa/quote_service/internal/domain"
	"github.com/shopspring/decimal"
)

type JobStore interface {
	RequeueProcessingJobs(ctx context.Context) error
	ClaimPending(ctx context.Context, limit int) ([]domain.UpdateJob, error)
	MarkSucceeded(ctx context.Context, jobID string, price decimal.Decimal, provider string, updatedAt time.Time) error
	MarkFailed(ctx context.Context, jobID string, message string, finishedAt time.Time) error
}

type RateProvider interface {
	FetchRate(ctx context.Context, pair domain.Pair) (domain.FetchedRate, error)
}

type Processor struct {
	store    JobStore
	provider RateProvider
	logger   *slog.Logger
	now      func() time.Time
}

func NewProcessor(store JobStore, provider RateProvider, logger *slog.Logger) *Processor {
	if logger == nil {
		logger = slog.Default()
	}
	return &Processor{
		store:    store,
		provider: provider,
		logger:   logger,
		now:      time.Now,
	}
}

func (p *Processor) RequeueProcessing(ctx context.Context) error {
	if err := p.store.RequeueProcessingJobs(ctx); err != nil {
		return fmt.Errorf("requeue processing quote update jobs: %w", err)
	}
	return nil
}

func (p *Processor) ProcessPending(ctx context.Context, limit int, concurrency int) error {
	if limit < 1 {
		return fmt.Errorf("claim limit must be greater than zero")
	}
	if concurrency < 1 {
		return fmt.Errorf("concurrency must be greater than zero")
	}

	jobs, err := p.store.ClaimPending(ctx, limit)
	if err != nil {
		return fmt.Errorf("claim pending quote update jobs: %w", err)
	}
	if len(jobs) == 0 {
		return nil
	}

	workerCount := min(len(jobs), concurrency)
	jobCh := make(chan domain.UpdateJob)
	errCh := make(chan error, len(jobs))
	var wg sync.WaitGroup
	wg.Add(workerCount)
	for range workerCount {
		go func() {
			defer wg.Done()
			for job := range jobCh {
				if err := p.processOne(ctx, job); err != nil {
					errCh <- err
				}
			}
		}()
	}

	for _, job := range jobs {
		select {
		case jobCh <- job:
		case <-ctx.Done():
			close(jobCh)
			wg.Wait()
			close(errCh)
			return errors.Join(append([]error{ctx.Err()}, collectErrors(errCh)...)...)
		}
	}
	close(jobCh)
	wg.Wait()
	close(errCh)
	return errors.Join(collectErrors(errCh)...)
}

func (p *Processor) processOne(ctx context.Context, job domain.UpdateJob) error {
	rate, err := p.provider.FetchRate(ctx, job.Pair)
	if err != nil {
		if markErr := p.store.MarkFailed(ctx, job.ID, err.Error(), p.now().UTC()); markErr != nil {
			return fmt.Errorf("mark quote update job %s failed: %w", job.ID, markErr)
		}
		return nil
	}

	if err := p.store.MarkSucceeded(ctx, job.ID, rate.Price, rate.Provider, rate.FetchedAt); err != nil {
		return fmt.Errorf("mark quote update job %s succeeded: %w", job.ID, err)
	}
	p.logger.Info("quote update job completed", "job_id", job.ID, "pair", job.Pair.Raw, "provider", rate.Provider)
	return nil
}

func collectErrors(errCh <-chan error) []error {
	var errs []error
	for err := range errCh {
		errs = append(errs, err)
	}
	return errs
}
