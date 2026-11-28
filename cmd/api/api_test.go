package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHTTPRouting(t *testing.T) {
	const origin = "http://localhost:5173"
	handler := newHandler(nil, origin, nil, nil, nil)

	t.Run("session APIs return JSON when signed out", func(t *testing.T) {
		for _, route := range []struct{ method, path string }{
			{http.MethodGet, "/api/v1/user"},
			{http.MethodPost, "/api/v1/refresh"},
			{http.MethodGet, "/api/v1/users/me"},
			{http.MethodPost, "/api/v1/auth/sessions/current/renewals"},
			{http.MethodDelete, "/api/v1/auth/sessions/current"},
		} {
			req := httptest.NewRequest(route.method, route.path, nil)
			res := httptest.NewRecorder()
			handler.ServeHTTP(res, req)
			checkErrorCode(t, res, "authentication_required")
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
		if res.Code != http.StatusMethodNotAllowed || res.Header().Get("Allow") != "POST" {
			t.Fatalf("expected 405 with Allow: POST, got %d %v", res.Code, res.Header())
		}
		checkErrorCode(t, res, "method_not_allowed")
	})

	t.Run("unknown API path returns JSON", func(t *testing.T) {
		res := httptest.NewRecorder()
		handler.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/api/v1/unknown", nil))
		if res.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d", res.Code)
		}
		checkErrorCode(t, res, "not_found")
	})

	t.Run("frontend preflight permits credentials", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodOptions, "/api/v1/admin/submissions/1", nil)
		req.Header.Set("Origin", origin)
		req.Header.Set("Access-Control-Request-Method", http.MethodPatch)
		req.Header.Set("Access-Control-Request-Headers", "Content-Type")
		res := httptest.NewRecorder()
		handler.ServeHTTP(res, req)
		if res.Code != http.StatusOK || res.Header().Get("Access-Control-Allow-Origin") != origin ||
			res.Header().Get("Access-Control-Allow-Credentials") != "true" ||
			res.Header().Get("Access-Control-Allow-Methods") != "PATCH" {
			t.Fatalf("unexpected preflight response: %d %v", res.Code, res.Header())
		}
	})
}

func checkErrorCode(t *testing.T, res *httptest.ResponseRecorder, code string) {
	t.Helper()
	var body struct {
		Code  string
		Error string
	}
	if err := json.Unmarshal(res.Body.Bytes(), &body); err != nil || body.Code != code || body.Error == "" {
		t.Fatalf("expected API error %q, got %s (%v)", code, res.Body, err)
	}
}
