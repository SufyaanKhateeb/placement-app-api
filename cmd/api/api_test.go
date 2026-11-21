package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHTTPRouting(t *testing.T) {
	const origin = "http://localhost:5173"
	handler := newHandler(nil, origin, nil)

	t.Run("protected route requires a session", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/user", nil)
		res := httptest.NewRecorder()
		handler.ServeHTTP(res, req)
		if res.Code != http.StatusFound || res.Header().Get("Location") != "/login" {
			t.Fatalf("expected login redirect, got %d to %q", res.Code, res.Header().Get("Location"))
		}
	})

	t.Run("wrong method is rejected", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/login", nil)
		res := httptest.NewRecorder()
		handler.ServeHTTP(res, req)
		if res.Code != http.StatusMethodNotAllowed {
			t.Fatalf("expected 405, got %d", res.Code)
		}
	})

	t.Run("frontend preflight permits credentials", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodOptions, "/api/v1/login", nil)
		req.Header.Set("Origin", origin)
		req.Header.Set("Access-Control-Request-Method", http.MethodPost)
		req.Header.Set("Access-Control-Request-Headers", "Content-Type")
		res := httptest.NewRecorder()
		handler.ServeHTTP(res, req)
		if res.Code != http.StatusOK || res.Header().Get("Access-Control-Allow-Origin") != origin ||
			res.Header().Get("Access-Control-Allow-Credentials") != "true" {
			t.Fatalf("unexpected preflight response: %d %v", res.Code, res.Header())
		}
	})
}
