package search

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jeremyvw/fareway/internal/model"
	usecase "github.com/jeremyvw/fareway/internal/usecase/search"
)

type fakeUsecase struct {
	response model.SearchResponse
	err      error
	gotReq   model.SearchRequest
}

func (f *fakeUsecase) Search(_ context.Context, req model.SearchRequest) (model.SearchResponse, error) {
	f.gotReq = req
	return f.response, f.err
}

func quiet() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

const validBody = `{"origin":"CGK","destination":"DPS","departureDate":"2025-12-15","returnDate":null,"passengers":1,"cabinClass":"economy"}`

func post(t *testing.T, h *Handler, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/flights/search", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.Search(rec, req)
	return rec
}

func TestSearchReturnsTheUsecaseResponse(t *testing.T) {
	fake := &fakeUsecase{response: model.SearchResponse{
		SearchCriteria: model.Criteria{Origin: "CGK", Destination: "DPS"},
		Metadata:       model.Metadata{TotalResults: 1, ProvidersQueried: 4, ProvidersSucceded: 4},
		Flights:        []model.FlightView{{ID: "QZ7250_AirAsia"}},
	}}

	rec := post(t, New(fake, quiet()), validBody)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, "application/json") {
		t.Errorf("content-type = %q", got)
	}

	var body model.SearchResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Metadata.TotalResults != 1 || len(body.Flights) != 1 {
		t.Errorf("body = %+v", body)
	}

	// The camelCase request shape from the assignment must decode.
	if fake.gotReq.Origin != "CGK" || fake.gotReq.DepartureDate != "2025-12-15" || fake.gotReq.Passengers != 1 {
		t.Errorf("usecase received %+v", fake.gotReq)
	}
}

func TestPartialFailureIsStill200(t *testing.T) {
	fake := &fakeUsecase{response: model.SearchResponse{
		Metadata: model.Metadata{
			TotalResults: 9, ProvidersQueried: 4, ProvidersSucceded: 3, ProvidersFailed: 1,
			ProviderStatus: []model.ProviderStatus{{Provider: "AirAsia", OK: false, Error: "unavailable"}},
		},
	}}

	rec := post(t, New(fake, quiet()), validBody)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 for a partial success", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "providers_failed") {
		t.Error("response does not report providers_failed")
	}
}

func TestStatusMapping(t *testing.T) {
	for name, tc := range map[string]struct {
		body string
		err  error
		want int
	}{
		"malformed json": {
			body: `{"origin":`,
			want: http.StatusBadRequest,
		},
		"unknown field": {
			body: `{"origin":"CGK","destination":"DPS","departureDate":"2025-12-15","cabinKlass":"economy"}`,
			want: http.StatusBadRequest,
		},
		"invalid request": {
			body: validBody,
			err:  usecase.ErrInvalidRequest,
			want: http.StatusBadRequest,
		},
		"all providers failed": {
			body: validBody,
			err:  usecase.ErrAllProvidersFailed,
			want: http.StatusBadGateway,
		},
		"unexpected error": {
			body: validBody,
			err:  errors.New("boom"),
			want: http.StatusInternalServerError,
		},
	} {
		t.Run(name, func(t *testing.T) {
			rec := post(t, New(&fakeUsecase{err: tc.err}, quiet()), tc.body)

			if rec.Code != tc.want {
				t.Errorf("status = %d, want %d: %s", rec.Code, tc.want, rec.Body.String())
			}
			var body errorResponse
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("error reply is not JSON: %v", err)
			}
			if body.Error == "" {
				t.Error("error reply carries no message")
			}
		})
	}
}

func TestAllProvidersFailedIsNot500(t *testing.T) {
	rec := post(t, New(&fakeUsecase{err: usecase.ErrAllProvidersFailed}, quiet()), validBody)
	if rec.Code == http.StatusInternalServerError {
		t.Error("status = 500; an unreachable upstream is 502, not our own failure")
	}
}

func TestHealth(t *testing.T) {
	rec := httptest.NewRecorder()
	New(&fakeUsecase{}, quiet()).Health(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "ok") {
		t.Errorf("body = %q", rec.Body.String())
	}
}
