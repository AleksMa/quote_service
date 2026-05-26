package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/AleksMa/quote_service/internal/domain"
)

func TestCreateUpdateRequest(t *testing.T) {
	store := newFakeStore()
	handler := New(store, map[string]struct{}{"EUR/MXN": {}})

	body := bytes.NewBufferString(`{"pair":"EUR/MXN"}`)
	req := httptest.NewRequest(http.MethodPost, "/quote-updates", body)
	req.Header.Set("Idempotency-Key", "same-key")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", rec.Code, rec.Body.String())
	}
	if store.idempotencyKey != "same-key" {
		t.Fatalf("idempotency key was not passed to store")
	}
	var response quoteUpdateResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.RequestID == "" || response.Status != domain.StatusPending {
		t.Fatalf("unexpected response: %+v", response)
	}
}

func TestCreateUpdateRequestRejectsUnsupportedPair(t *testing.T) {
	handler := New(newFakeStore(), map[string]struct{}{"EUR/MXN": {}})
	req := httptest.NewRequest(http.MethodPost, "/quote-updates", bytes.NewBufferString(`{"pair":"USD/EUR"}`))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestCreateUpdateRequestIsIdempotent(t *testing.T) {
	store := newFakeStore()
	handler := New(store, map[string]struct{}{"EUR/MXN": {}})

	first := postQuoteUpdate(t, handler, "same-key")
	second := postQuoteUpdate(t, handler, "same-key")

	if first.RequestID != second.RequestID {
		t.Fatalf("expected same request id, got %s and %s", first.RequestID, second.RequestID)
	}
}

func TestGetUpdateRequestPendingReturnsAccepted(t *testing.T) {
	store := newFakeStore()
	store.update = domain.UpdateRequest{
		ID:     "3f90b1d3-e261-4ac6-b7ac-1a66dc67f747",
		Pair:   domain.Pair{Raw: "EUR/MXN", Base: "EUR", Quote: "MXN"},
		Status: domain.StatusProcessing,
	}
	handler := New(store, map[string]struct{}{"EUR/MXN": {}})
	req := httptest.NewRequest(http.MethodGet, "/quote-updates/3f90b1d3-e261-4ac6-b7ac-1a66dc67f747", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d", rec.Code)
	}
}

func TestGetUpdateRequestSucceededReturnsQuote(t *testing.T) {
	finishedAt := time.Date(2026, 5, 19, 12, 0, 0, 0, time.UTC)
	store := newFakeStore()
	store.update = domain.UpdateRequest{
		ID:         "3f90b1d3-e261-4ac6-b7ac-1a66dc67f747",
		Pair:       domain.Pair{Raw: "EUR/MXN", Base: "EUR", Quote: "MXN"},
		Status:     domain.StatusSucceeded,
		Price:      "20.1",
		Provider:   "frankfurter",
		FinishedAt: &finishedAt,
	}
	handler := New(store, map[string]struct{}{"EUR/MXN": {}})
	req := httptest.NewRequest(http.MethodGet, "/quote-updates/3f90b1d3-e261-4ac6-b7ac-1a66dc67f747", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var response quoteUpdateResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Price != "20.1" || response.UpdatedAt == nil {
		t.Fatalf("unexpected response: %+v", response)
	}
}

func TestGetLatestQuoteMissingReturnsNotFound(t *testing.T) {
	store := newFakeStore()
	store.latestErr = domain.ErrNotFound
	handler := New(store, map[string]struct{}{"EUR/MXN": {}})
	req := httptest.NewRequest(http.MethodGet, "/quotes/latest/EUR/MXN", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestCORSPreflightAllowedOrigin(t *testing.T) {
	handler := New(newFakeStore(), map[string]struct{}{"EUR/MXN": {}}, Options{
		CORSAllowedOrigins: []string{"http://localhost:*"},
	})
	req := httptest.NewRequest(http.MethodOptions, "/quote-updates", nil)
	req.Header.Set("Origin", "http://localhost:3000")
	req.Header.Set("Access-Control-Request-Method", http.MethodPost)
	req.Header.Set("Access-Control-Request-Headers", "Content-Type, Idempotency-Key")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", rec.Code)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "http://localhost:3000" {
		t.Fatalf("unexpected allowed origin: %q", got)
	}
	if got := rec.Header().Get("Access-Control-Allow-Headers"); got != corsAllowedHeaders {
		t.Fatalf("unexpected allowed headers: %q", got)
	}
}

func TestCORSAllowsAnyOrigin(t *testing.T) {
	handler := New(newFakeStore(), map[string]struct{}{"EUR/MXN": {}}, Options{
		CORSAllowedOrigins: []string{"*"},
	})
	req := httptest.NewRequest(http.MethodOptions, "/quote-updates", nil)
	req.Header.Set("Origin", "https://editor.swagger.io")
	req.Header.Set("Access-Control-Request-Method", http.MethodPost)
	req.Header.Set("Access-Control-Request-Private-Network", "true")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", rec.Code)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Fatalf("unexpected allowed origin: %q", got)
	}
	if got := rec.Header().Get("Access-Control-Allow-Private-Network"); got != "true" {
		t.Fatalf("unexpected private network header: %q", got)
	}
}

func TestCORSRejectsUnknownOrigin(t *testing.T) {
	handler := New(newFakeStore(), map[string]struct{}{"EUR/MXN": {}}, Options{
		CORSAllowedOrigins: []string{"http://localhost:*"},
	})
	req := httptest.NewRequest(http.MethodOptions, "/quote-updates", nil)
	req.Header.Set("Origin", "https://example.test")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("unexpected CORS header for rejected origin")
	}
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected request to fall through to handler, got %d", rec.Code)
	}
}

type fakeStore struct {
	idempotencyKey string
	update         domain.UpdateRequest
	latest         domain.LatestQuote
	latestErr      error
	idempotent     map[string]domain.UpdateRequest
}

func newFakeStore() *fakeStore {
	return &fakeStore{idempotent: make(map[string]domain.UpdateRequest)}
}

func (s *fakeStore) CreateUpdateRequest(ctx context.Context, pair domain.Pair, idempotencyKey string) (domain.UpdateRequest, bool, error) {
	s.idempotencyKey = idempotencyKey
	if idempotencyKey != "" {
		key := pair.Raw + ":" + idempotencyKey
		if update, ok := s.idempotent[key]; ok {
			return update, true, nil
		}
		id, _ := domain.NewUUID()
		update := domain.UpdateRequest{ID: id, Pair: pair, Status: domain.StatusPending}
		s.idempotent[key] = update
		return update, false, nil
	}
	id, _ := domain.NewUUID()
	return domain.UpdateRequest{ID: id, Pair: pair, Status: domain.StatusPending}, false, nil
}

func (s *fakeStore) GetUpdateRequest(ctx context.Context, id string) (domain.UpdateRequest, error) {
	if s.update.ID == "" {
		return domain.UpdateRequest{}, domain.ErrNotFound
	}
	return s.update, nil
}

func (s *fakeStore) GetLatestQuote(ctx context.Context, pair domain.Pair) (domain.LatestQuote, error) {
	if s.latestErr != nil {
		return domain.LatestQuote{}, s.latestErr
	}
	if s.latest.Pair.Raw == "" {
		return domain.LatestQuote{}, errors.New("unexpected latest quote call")
	}
	return s.latest, nil
}

func postQuoteUpdate(t *testing.T, handler http.Handler, idempotencyKey string) quoteUpdateResponse {
	t.Helper()

	req := httptest.NewRequest(http.MethodPost, "/quote-updates", bytes.NewBufferString(`{"pair":"EUR/MXN"}`))
	req.Header.Set("Idempotency-Key", idempotencyKey)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", rec.Code, rec.Body.String())
	}
	var response quoteUpdateResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return response
}
