// Package search is the HTTP handler for flight search.
package search

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/jeremyvw/fareway/internal/model"
	usecase "github.com/jeremyvw/fareway/internal/usecase/search"
)

type Searcher interface {
	Search(ctx context.Context, req model.SearchRequest) (model.SearchResponse, error)
}

type Handler struct {
	usecase Searcher
	log     *slog.Logger
}

func New(service Searcher, log *slog.Logger) *Handler {
	return &Handler{usecase: service, log: log}
}

func (h *Handler) Search(w http.ResponseWriter, r *http.Request) {
	var req model.SearchRequest
	decoder := json.NewDecoder(r.Body)
	// Reject unknown fields rather than silently ignoring a misspelled filter, which would
	// otherwise look to a caller like the filter had been applied.
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, "malformed request body: "+err.Error())
		return
	}

	response, err := h.usecase.Search(r.Context(), req)
	switch {
	case errors.Is(err, usecase.ErrInvalidRequest):
		h.writeError(w, http.StatusBadRequest, err.Error())
		return
	case errors.Is(err, usecase.ErrAllProvidersFailed):
		h.writeError(w, http.StatusBadGateway, "no provider could be reached")
		return
	case err != nil:
		h.log.Error("search failed", slog.String("error", err.Error()))
		h.writeError(w, http.StatusInternalServerError, "search failed")
		return
	}

	h.writeJSON(w, http.StatusOK, response)
}

func (h *Handler) Health(w http.ResponseWriter, _ *http.Request) {
	h.writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

type errorResponse struct {
	Error string `json:"error"`
}

func (h *Handler) writeError(w http.ResponseWriter, status int, message string) {
	h.writeJSON(w, status, errorResponse{Error: message})
}

func (h *Handler) writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		h.log.Error("encode response", slog.String("error", err.Error()))
	}
}
