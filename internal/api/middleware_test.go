package api

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestRequestIDMiddleware_GeneratesID(t *testing.T) {
	handler := RequestIDMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqID, ok := r.Context().Value(RequestIDKey).(string)
		if !ok || reqID == "" {
			t.Error("request ID should be set in context")
		}
		w.WriteHeader(200)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	// Should be in response header too
	if rec.Header().Get("X-Request-ID") == "" {
		t.Error("X-Request-ID should be in response header")
	}
}

func TestRequestIDMiddleware_UsesExisting(t *testing.T) {
	handler := RequestIDMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqID := r.Context().Value(RequestIDKey).(string)
		if reqID != "my-custom-id" {
			t.Errorf("request ID = %s, want my-custom-id", reqID)
		}
		w.WriteHeader(200)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("X-Request-ID", "my-custom-id")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Header().Get("X-Request-ID") != "my-custom-id" {
		t.Error("should preserve client-provided request ID")
	}
}

func TestRecoveryMiddleware_CatchesPanic(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	handler := RecoveryMiddleware(logger)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("something broke!")
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()

	// Should NOT panic — recovery middleware catches it
	handler.ServeHTTP(rec, req)

	if rec.Code != 500 {
		t.Errorf("status = %d, want 500", rec.Code)
	}
}
