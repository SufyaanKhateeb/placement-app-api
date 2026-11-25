package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/SufyaanKhateeb/college-placement-app-api/types"
	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/oauth2"
)

type googleAdminTestSessions struct {
	types.AuthService
}

func (googleAdminTestSessions) VerifyToken(value string) (*jwt.Token, error) {
	return &jwt.Token{
		Valid:  true,
		Claims: &types.CustomClaims{Id: 7, Type: value},
	}, nil
}

type googleAdminTestStore struct {
	GoogleAccountStore
}

func (googleAdminTestStore) CheckGoogleAdminPassword(_ context.Context, admin types.TokenInput, password string) error {
	if admin.Id != 7 || password != "test-password" {
		return ErrGoogleAdminPassword
	}
	return nil
}

func TestStartAdminGoogleLinkRequiresPasswordAndSession(t *testing.T) {
	g := &GoogleSignIn{
		frontend: "https://app.example.com", secure: true,
		client: oauth2.Config{
			ClientSecret: "test-flow-key",
			Endpoint:     oauth2.Endpoint{AuthURL: "https://accounts.google.com/o/oauth2/v2/auth"},
		},
		accounts: googleAdminTestStore{}, sessions: googleAdminTestSessions{},
	}
	for _, tc := range []struct {
		name, origin, session, password string
		status                          int
	}{
		{"cross-origin request", "https://other.example.com", "admin", "test-password", http.StatusForbidden},
		{"regular user session", g.frontend, types.RoleUser, "test-password", http.StatusUnauthorized},
		{"wrong password", g.frontend, "admin", "wrong", http.StatusForbidden},
		{"confirmed admin password", g.frontend, "admin", "test-password", http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/google/admin/link", strings.NewReader(`{"password":"`+tc.password+`"}`))
			req.Header.Set("Origin", tc.origin)
			req.AddCookie(&http.Cookie{Name: "ACCESS_TOKEN", Value: tc.session})
			res := httptest.NewRecorder()
			g.startAdminLink(res, req)
			if res.Code != tc.status {
				t.Fatalf("expected HTTP %d, got %d", tc.status, res.Code)
			}
			cookies := res.Result().Cookies()
			if tc.status != http.StatusOK {
				if len(cookies) != 0 {
					t.Fatal("link flow began without authorization")
				}
				return
			}
			if len(cookies) != 1 {
				t.Fatal("expected a signed flow cookie")
			}
			fields := g.verifyFlow(cookies[0].Value)
			if len(fields) != 6 || fields[3] != googleAdminLinkFlow || fields[4] != "7" {
				t.Fatal("link flow was not bound to the authenticated admin")
			}
		})
	}
}

func TestAdminGoogleCallbackRejectsTamperedFlowOrChangedAccount(t *testing.T) {
	g := &GoogleSignIn{
		frontend: "https://app.example.com", sessions: googleAdminTestSessions{},
		client: oauth2.Config{ClientSecret: "test-flow-key"},
	}
	valid := g.signedFlow("state", "verifier", "nonce", googleAdminLinkFlow, "7")
	t.Run("tampered flow", func(t *testing.T) {
		tampered := strings.Replace(valid, ".admin-link.7.", ".admin-link.9.", 1)
		if g.verifyFlow(tampered) != nil {
			t.Fatal("tampered flow was accepted")
		}
	})
	t.Run("callback with a regular user session", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/google/callback?code=code&state=state", nil)
		req.AddCookie(g.flowCookie(valid, 60))
		req.AddCookie(&http.Cookie{Name: "ACCESS_TOKEN", Value: types.RoleUser})
		res := httptest.NewRecorder()
		g.callback(res, req)
		if res.Header().Get("Location") != g.frontend+"/admin/login?error=google_admin_session_expired" {
			t.Fatalf("unexpected redirect: %s", res.Header().Get("Location"))
		}
	})
}
