package api

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/SufyaanKhateeb/college-placement-app-api/config"
	"github.com/SufyaanKhateeb/college-placement-app-api/service/auth"
	"github.com/SufyaanKhateeb/college-placement-app-api/types"
)

func TestRegularSessionCannotUseAdminRoutes(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	previousConfig := config.Env
	t.Cleanup(func() { config.Env = previousConfig })
	config.Env.PrivateKey = key
	config.Env.PublicKey = &key.PublicKey
	config.Env.JWTExpirationTime = 300

	const origin = "http://localhost:5173"
	t.Setenv("GOOGLE_OIDC_CLIENT_ID", "test-client")
	t.Setenv("GOOGLE_OIDC_CLIENT_SECRET", "test-secret")
	t.Setenv("GOOGLE_OIDC_REDIRECT_URL", "http://localhost:8091/api/v1/auth/google/callback")
	sessions := &auth.AuthService{}
	google, err := auth.NewGoogleSignIn(context.Background(), nil, sessions, origin)
	if err != nil {
		t.Fatal(err)
	}
	access, refresh, err := sessions.CreateTokens(&types.TokenInput{Id: 7, Type: types.RoleUser})
	if err != nil {
		t.Fatal(err)
	}
	handler := newHandler(nil, origin, google, nil)
	for _, tc := range []struct {
		name, method, path, cookieName, token string
		status                                int
	}{
		{"retired admin registration", http.MethodPost, "/register-admin", "ACCESS_TOKEN", access, http.StatusGone},
		{"invite with access token", http.MethodPost, "/admin/access/invitations", "ACCESS_TOKEN", access, http.StatusForbidden},
		{"promote with refresh token", http.MethodPost, "/admin/access/accounts/user/7/promote", "REFRESH_TOKEN", refresh, http.StatusForbidden},
		{"read Google admin link", http.MethodGet, "/auth/google/admin/link", "ACCESS_TOKEN", access, http.StatusUnauthorized},
		{"start Google admin link", http.MethodPost, "/auth/google/admin/link", "ACCESS_TOKEN", access, http.StatusUnauthorized},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, "/api/v1"+tc.path, strings.NewReader("{}"))
			req.Header.Set("Origin", origin)
			req.Header.Set("Content-Type", "application/json")
			req.AddCookie(&http.Cookie{Name: tc.cookieName, Value: tc.token})
			res := httptest.NewRecorder()
			handler.ServeHTTP(res, req)
			if res.Code != tc.status {
				t.Fatalf("expected HTTP %d, got %d", tc.status, res.Code)
			}
			if len(res.Header().Values("Set-Cookie")) != 0 {
				t.Fatal("denied request issued cookies")
			}
		})
	}
}
