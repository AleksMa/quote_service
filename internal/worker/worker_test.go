package worker

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
)

func TestWorkerPassesProcessingOptions(t *testing.T) {
	processor := &fakeProcessor{}
	w := New(processor, Options{ClaimLimit: 3, Concurrency: 2, Logger: discardLogger()})

	w.processBatch(context.Background())

	if processor.limit != 3 || processor.concurrency != 2 {
		t.Fatalf("unexpected processing options: %+v", processor)
	}
}

func TestWorkerLogsProcessorError(t *testing.T) {
	var logs strings.Builder
	processor := &fakeProcessor{err: errors.New("database unavailable")}
	w := New(processor, Options{Logger: slog.New(slog.NewTextHandler(&logs, nil))})

	w.processBatch(context.Background())

	if !strings.Contains(logs.String(), "database unavailable") {
		t.Fatalf("expected processing error in logs, got %q", logs.String())
	}
}

type fakeProcessor struct {
	limit       int
	concurrency int
	err         error
}

func (p *fakeProcessor) ProcessPending(ctx context.Context, limit int, concurrency int) error {
	p.limit = limit
	p.concurrency = concurrency
	return p.err
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
