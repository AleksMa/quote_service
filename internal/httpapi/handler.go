package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/AleksMa/quote_service/internal/domain"
	"github.com/AleksMa/quote_service/internal/storage"
)

type Handler struct {
	store          storage.APIStore
	supportedPairs map[string]struct{}
}

type Options struct {
	SwaggerUIOrigin string
}

func New(store storage.APIStore, supportedPairs map[string]struct{}, opts ...Options) http.Handler {
	h := &Handler{store: store, supportedPairs: supportedPairs}
	mux := http.NewServeMux()
	mux.HandleFunc("/quote-updates", h.quoteUpdates)
	mux.HandleFunc("/quote-updates/", h.quoteUpdateByID)
	mux.HandleFunc("/quotes/latest/", h.latestQuote)
	if len(opts) > 0 && opts[0].SwaggerUIOrigin != "" {
		return withSwaggerCORS(mux, opts[0].SwaggerUIOrigin)
	}
	return mux
}

func withSwaggerCORS(next http.Handler, swaggerUIOrigin string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Origin") == swaggerUIOrigin {
			w.Header().Set("Access-Control-Allow-Origin", swaggerUIOrigin)
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Idempotency-Key")
			w.Header().Add("Vary", "Origin")
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func (h *Handler) quoteUpdates(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/quote-updates" {
		notFound(w)
		return
	}
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}

	var req struct {
		Pair string `json:"pair"`
	}
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "request body must be a JSON object with a pair field")
		return
	}

	pair, err := domain.ParsePair(req.Pair, h.supportedPairs)
	if err != nil {
		writePairError(w, err)
		return
	}

	idempotencyKey := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if len(idempotencyKey) > 255 {
		writeError(w, http.StatusBadRequest, "invalid_idempotency_key", "Idempotency-Key must be at most 255 characters")
		return
	}

	update, idempotencyReplayed, err := h.store.CreateUpdateRequest(r.Context(), pair, idempotencyKey)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to create update request")
		return
	}

	response := updateResponse(update)
	response.IdempotencyReplayed = &idempotencyReplayed
	writeJSON(w, http.StatusAccepted, response)
}

func (h *Handler) quoteUpdateByID(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}

	id := strings.TrimPrefix(r.URL.Path, "/quote-updates/")
	if id == "" || strings.Contains(id, "/") {
		notFound(w)
		return
	}
	if !domain.IsUUID(id) {
		writeError(w, http.StatusBadRequest, "invalid_request_id", "request_id must be a UUID")
		return
	}

	update, err := h.store.GetUpdateRequest(r.Context(), id)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			notFound(w)
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to get update request")
		return
	}

	switch update.Status {
	case domain.StatusPending, domain.StatusProcessing:
		writeJSON(w, http.StatusAccepted, updateResponse(update))
	case domain.StatusFailed:
		writeJSON(w, http.StatusConflict, updateResponse(update))
	default:
		writeJSON(w, http.StatusOK, updateResponse(update))
	}
}

func (h *Handler) latestQuote(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}

	rawPair := strings.TrimPrefix(r.URL.Path, "/quotes/latest/")
	pair, err := domain.ParsePair(rawPair, h.supportedPairs)
	if err != nil {
		writePairError(w, err)
		return
	}

	quote, err := h.store.GetLatestQuote(r.Context(), pair)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			notFound(w)
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to get latest quote")
		return
	}

	writeJSON(w, http.StatusOK, latestQuoteResponse{
		Pair:      quote.Pair.Raw,
		Price:     json.Number(quote.Price.String()),
		Provider:  quote.Provider,
		UpdatedAt: quote.UpdatedAt,
		RequestID: quote.RequestID,
	})
}

func writePairError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, domain.ErrInvalidPair):
		writeError(w, http.StatusBadRequest, "invalid_pair", "pair must use BASE/QUOTE format, for example EUR/MXN")
	case errors.Is(err, domain.ErrUnsupportedPair):
		writeError(w, http.StatusBadRequest, "unsupported_pair", "currency pair is not supported")
	default:
		writeError(w, http.StatusBadRequest, "invalid_pair", "currency pair is invalid")
	}
}

func methodNotAllowed(w http.ResponseWriter, allowed string) {
	w.Header().Set("Allow", allowed)
	writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", fmt.Sprintf("method must be %s", allowed))
}

func notFound(w http.ResponseWriter) {
	writeError(w, http.StatusNotFound, "not_found", "resource was not found")
}

type errorResponse struct {
	Error apiError `json:"error"`
}

type apiError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type quoteUpdateResponse struct {
	RequestID           string        `json:"request_id"`
	Status              domain.Status `json:"status"`
	IdempotencyReplayed *bool         `json:"idempotency_replayed,omitempty"`
	Pair                string        `json:"pair,omitempty"`
	Price               json.Number   `json:"price,omitempty"`
	Provider            string        `json:"provider,omitempty"`
	UpdatedAt           *time.Time    `json:"updated_at,omitempty"`
	Error               string        `json:"error,omitempty"`
}

type latestQuoteResponse struct {
	Pair      string      `json:"pair"`
	Price     json.Number `json:"price"`
	Provider  string      `json:"provider"`
	UpdatedAt time.Time   `json:"updated_at"`
	RequestID string      `json:"request_id"`
}

func updateResponse(update domain.UpdateRequest) quoteUpdateResponse {
	response := quoteUpdateResponse{
		RequestID: update.ID,
		Status:    update.Status,
		Pair:      update.Pair.Raw,
		Provider:  update.Provider,
		Error:     update.Error,
	}
	if update.Price != nil {
		response.Price = json.Number(update.Price.String())
	}
	if update.FinishedAt != nil {
		response.UpdatedAt = update.FinishedAt
	}
	return response
}

func writeError(w http.ResponseWriter, status int, code string, message string) {
	writeJSON(w, status, errorResponse{Error: apiError{Code: code, Message: message}})
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
