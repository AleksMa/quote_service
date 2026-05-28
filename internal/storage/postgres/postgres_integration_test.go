package postgres

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/AleksMa/quote_service/internal/domain"
)

func TestStoreCreateUpdateRequestIsIdempotent(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	pair := uniquePair(t)
	idempotencyKey := "test-" + uniqueID(t)

	first, repeated, err := store.CreateUpdateRequest(ctx, pair, idempotencyKey)
	if err != nil {
		t.Fatalf("CreateUpdateRequest returned error: %v", err)
	}
	if repeated {
		t.Fatal("first request should not be marked as repeated")
	}
	t.Cleanup(cleanupPair(t, store, pair))

	second, repeated, err := store.CreateUpdateRequest(ctx, pair, idempotencyKey)
	if err != nil {
		t.Fatalf("CreateUpdateRequest returned error for repeated key: %v", err)
	}
	if !repeated {
		t.Fatal("second request should be marked as repeated")
	}
	if first.ID != second.ID {
		t.Fatalf("expected same request id, got %s and %s", first.ID, second.ID)
	}
	if first.JobID == "" || first.JobID != second.JobID {
		t.Fatalf("expected same job id, got %s and %s", first.JobID, second.JobID)
	}

	got, err := store.GetUpdateRequest(ctx, first.ID)
	if err != nil {
		t.Fatalf("GetUpdateRequest returned error: %v", err)
	}
	if got.Status != domain.StatusPending || got.Pair.Raw != pair.Raw {
		t.Fatalf("unexpected update request: %+v", got)
	}
}

func TestStoreCreateUpdateRequestReusesActiveJobForPair(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	pair := uniquePair(t)

	first, repeated, err := store.CreateUpdateRequest(ctx, pair, "test-"+uniqueID(t))
	if err != nil {
		t.Fatalf("CreateUpdateRequest returned error: %v", err)
	}
	if repeated {
		t.Fatal("first request should not be marked as repeated")
	}
	t.Cleanup(cleanupPair(t, store, pair))
	prioritizePendingJob(t, store, first.JobID)

	second, repeated, err := store.CreateUpdateRequest(ctx, pair, "test-"+uniqueID(t))
	if err != nil {
		t.Fatalf("CreateUpdateRequest returned error for same pair: %v", err)
	}
	if repeated {
		t.Fatal("different idempotency key should not be marked as repeated")
	}
	if first.ID == second.ID {
		t.Fatalf("expected different request ids, got %s", first.ID)
	}
	if first.JobID == "" || first.JobID != second.JobID {
		t.Fatalf("expected same active job id, got %s and %s", first.JobID, second.JobID)
	}

	claimed, err := store.ClaimPending(ctx, 10)
	if err != nil {
		t.Fatalf("ClaimPending returned error: %v", err)
	}
	var matchingJobs int
	for _, job := range claimed {
		if job.Pair.Raw == pair.Raw {
			matchingJobs++
		}
	}
	if matchingJobs != 1 {
		t.Fatalf("expected one claimed job for pair %s, got %d in %+v", pair.Raw, matchingJobs, claimed)
	}
}

func TestStoreClaimMarkSucceededAndLatestQuote(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	pair := uniquePair(t)

	created, _, err := store.CreateUpdateRequest(ctx, pair, "test-"+uniqueID(t))
	if err != nil {
		t.Fatalf("CreateUpdateRequest returned error: %v", err)
	}
	t.Cleanup(cleanupPair(t, store, pair))
	prioritizePendingJob(t, store, created.JobID)

	claimed, err := store.ClaimPending(ctx, 1)
	if err != nil {
		t.Fatalf("ClaimPending returned error: %v", err)
	}
	if len(claimed) != 1 || claimed[0].ID != created.JobID {
		t.Fatalf("unexpected claimed jobs: %+v", claimed)
	}
	if claimed[0].Status != domain.StatusProcessing {
		t.Fatalf("expected processing status, got %s", claimed[0].Status)
	}

	updatedAt := time.Now().UTC().Truncate(time.Microsecond)
	if err := store.MarkSucceeded(ctx, created.JobID, "1.2345", "test-provider", updatedAt); err != nil {
		t.Fatalf("MarkSucceeded returned error: %v", err)
	}

	updated, err := store.GetUpdateRequest(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetUpdateRequest returned error: %v", err)
	}
	if updated.Status != domain.StatusSucceeded || updated.Price == "" || updated.Provider != "test-provider" {
		t.Fatalf("unexpected succeeded request: %+v", updated)
	}

	latest, err := store.GetLatestQuote(ctx, pair)
	if err != nil {
		t.Fatalf("GetLatestQuote returned error: %v", err)
	}
	if latest.Pair.Raw != pair.Raw || latest.Price == "" || latest.Provider != "test-provider" || latest.RequestID != created.ID {
		t.Fatalf("unexpected latest quote: %+v", latest)
	}
}

func TestStoreMarkFailed(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	pair := uniquePair(t)

	created, _, err := store.CreateUpdateRequest(ctx, pair, "test-"+uniqueID(t))
	if err != nil {
		t.Fatalf("CreateUpdateRequest returned error: %v", err)
	}
	t.Cleanup(cleanupPair(t, store, pair))
	prioritizePendingJob(t, store, created.JobID)

	if _, err := store.ClaimPending(ctx, 1); err != nil {
		t.Fatalf("ClaimPending returned error: %v", err)
	}
	if err := store.MarkFailed(ctx, created.JobID, "provider failed", time.Now().UTC()); err != nil {
		t.Fatalf("MarkFailed returned error: %v", err)
	}

	got, err := store.GetUpdateRequest(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetUpdateRequest returned error: %v", err)
	}
	if got.Status != domain.StatusFailed || got.Error != "provider failed" {
		t.Fatalf("unexpected failed request: %+v", got)
	}
}

func openTestStore(t *testing.T) *Store {
	t.Helper()

	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}

	store, err := Open(context.Background(), databaseURL)
	if err != nil {
		t.Fatalf("open test store: %v", err)
	}
	t.Cleanup(store.Close)
	return store
}

func cleanupPair(t *testing.T, store *Store, pair domain.Pair) func() {
	t.Helper()

	return func() {
		ctx := context.Background()
		if _, err := store.pool.Exec(ctx, `DELETE FROM latest_quotes WHERE pair = $1`, pair.Raw); err != nil {
			t.Fatalf("delete latest quote for pair %s: %v", pair.Raw, err)
		}
		if _, err := store.pool.Exec(ctx, `DELETE FROM quote_update_requests WHERE pair = $1`, pair.Raw); err != nil {
			t.Fatalf("delete update requests for pair %s: %v", pair.Raw, err)
		}
		if _, err := store.pool.Exec(ctx, `DELETE FROM quote_update_jobs WHERE pair = $1`, pair.Raw); err != nil {
			t.Fatalf("delete update jobs for pair %s: %v", pair.Raw, err)
		}
	}
}

func prioritizePendingJob(t *testing.T, store *Store, id string) {
	t.Helper()

	_, err := store.pool.Exec(context.Background(), `
		UPDATE quote_update_jobs
		SET created_at = '1970-01-01 00:00:00+00'
		WHERE id = $1::uuid
	`, id)
	if err != nil {
		t.Fatalf("prioritize update job %s: %v", id, err)
	}
}

func uniqueID(t *testing.T) string {
	t.Helper()

	id, err := domain.NewUUID()
	if err != nil {
		t.Fatalf("generate UUID: %v", err)
	}
	return id
}

func uniquePair(t *testing.T) domain.Pair {
	t.Helper()

	id := uniqueID(t)
	letters := make([]byte, 0, 6)
	for _, ch := range id {
		switch {
		case ch >= '0' && ch <= '9':
			letters = append(letters, byte('A'+ch-'0'))
		case ch >= 'a' && ch <= 'f':
			letters = append(letters, byte('K'+ch-'a'))
		}
		if len(letters) == 6 {
			break
		}
	}
	base := string(letters[:3])
	quote := string(letters[3:6])
	return domain.Pair{Raw: base + "/" + quote, Base: base, Quote: quote}
}

func TestStoreGetMissingLatestQuote(t *testing.T) {
	store := openTestStore(t)
	_, err := store.GetLatestQuote(context.Background(), uniquePair(t))
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}
