package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHTTPRouting(t *testing.T) {
	const origin = "http://localhost:5173"
	handler := newHandler(nil, origin, nil, nil)

	t.Run("session APIs return JSON when signed out", func(t *testing.T) {
		for _, route := range []struct{ method, path string }{
			{http.MethodGet, "/api/v1/user"},
			{http.MethodPost, "/api/v1/refresh"},
		} {
			req := httptest.NewRequest(route.method, route.path, nil)
			res := httptest.NewRecorder()
			handler.ServeHTTP(res, req)
			if res.Code != http.StatusUnauthorized || res.Header().Get("Location") != "" ||
				!strings.HasPrefix(res.Header().Get("Content-Type"), "application/json") {
				t.Fatalf("%s %s: expected a JSON 401 without a redirect, got %d %v", route.method, route.path, res.Code, res.Header())
			}
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
