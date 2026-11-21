package api

import (
	"crypto/rand"
	"crypto/rsa"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/SufyaanKhateeb/college-placement-app-api/config"
	"github.com/SufyaanKhateeb/college-placement-app-api/middlewares"
	"github.com/SufyaanKhateeb/college-placement-app-api/service/auth"
	"github.com/SufyaanKhateeb/college-placement-app-api/types"
)

func TestLegacyRegularUserSessions(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	previousConfig := config.Env
	t.Cleanup(func() { config.Env = previousConfig })
	config.Env.PrivateKey = key
	config.Env.PublicKey = &key.PublicKey
	config.Env.JWTExpirationTime = 300
	sessions := &auth.AuthService{}
	legacy, err := sessions.SignJwt(time.Minute, types.CustomClaims{Id: 7, Type: types.LegacyRoleStudent})
	if err != nil {
		t.Fatal(err)
	}

	handler := middlewares.Authenticate(sessions, "")(func(w http.ResponseWriter, r *http.Request) {
		session, ok := middlewares.Session(r.Context())
		if !ok || session.Type != types.RoleUser || session.Id != 7 {
			t.Errorf("expected the original user ID and canonical user role, got %+v", session)
		}
		w.WriteHeader(http.StatusNoContent)
	})
	for _, name := range []string{"ACCESS_TOKEN", "REFRESH_TOKEN"} {
		t.Run(name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/v1/user", nil)
			req.AddCookie(&http.Cookie{Name: name, Value: legacy})
			res := httptest.NewRecorder()
			handler.ServeHTTP(res, req)
			if res.Code != http.StatusNoContent {
				t.Fatalf("legacy session was denied: HTTP %d", res.Code)
			}
			if name == "REFRESH_TOKEN" {
				cookies := res.Result().Cookies()
				if len(cookies) != 1 || cookies[0].Name != "ACCESS_TOKEN" {
					t.Fatal("expected a refreshed access token")
				}
				token, err := sessions.VerifyToken(cookies[0].Value)
				if err != nil || token.Claims.(*types.CustomClaims).Type != types.RoleUser {
					t.Fatal("expected the refreshed token to use the user role")
				}
			}
		})
	}

	adminOnly := middlewares.Authenticate(sessions, types.RoleAdmin)(func(http.ResponseWriter, *http.Request) {
		t.Fatal("legacy user session reached an admin endpoint")
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/register-admin", nil)
	req.AddCookie(&http.Cookie{Name: "ACCESS_TOKEN", Value: legacy})
	res := httptest.NewRecorder()
	adminOnly.ServeHTTP(res, req)
	if res.Code != http.StatusFound {
		t.Fatalf("expected the admin access check to deny the session, got HTTP %d", res.Code)
	}
}
