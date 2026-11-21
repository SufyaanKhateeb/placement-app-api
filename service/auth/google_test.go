package auth

import (
	"context"
	"crypto"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/SufyaanKhateeb/college-placement-app-api/types"
	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/oauth2"
)

type googleTestStore struct {
	GoogleAccountStore
	identity GoogleIdentity
	err      error
}

func (s *googleTestStore) ResolveGoogleAccount(_ context.Context, identity GoogleIdentity) (types.TokenInput, error) {
	s.identity = identity
	return types.TokenInput{Id: 7, Type: types.RoleUser}, s.err
}

type googleTestSessions struct{ types.AuthService }

func (googleTestSessions) CreateTokens(*types.TokenInput) (string, string, error) {
	return "access", "refresh", nil
}

func TestGoogleSignInCallback(t *testing.T) {
	privateKey, publicKey, err := getMockKeys()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name          string
		callbackState string
		tokenNonce    string
		emailVerified bool
		storeError    error
		wantRedirect  string
		wantAccount   bool
	}{
		{"valid identity", "state", "nonce", true, nil, "/", true},
		{"legacy account requires linking", "state", "nonce", true, ErrGoogleAccountExists, "/login?error=google_link_required", true},
		{"state mismatch", "wrong", "nonce", true, nil, "/login?error=google_failed", false},
		{"nonce mismatch", "state", "wrong", true, nil, "/login?error=google_failed", false},
		{"unverified email", "state", "nonce", false, nil, "/login?error=google_failed", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw, err := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
				"iss": googleIssuer, "aud": "client", "sub": "google-user",
				"exp": time.Now().Add(time.Minute).Unix(), "nonce": tc.tokenNonce,
				"email": "user@example.com", "email_verified": tc.emailVerified,
			}).SignedString(privateKey)
			if err != nil {
				t.Fatal(err)
			}

			provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if err := r.ParseForm(); err != nil || r.FormValue("code_verifier") != "verifier" {
					http.Error(w, "invalid verifier", http.StatusBadRequest)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(map[string]string{"access_token": "provider-token", "id_token": raw})
			}))
			defer provider.Close()
			store := &googleTestStore{err: tc.storeError}
			signIn := &GoogleSignIn{
				client: oauth2.Config{ClientID: "client", Endpoint: oauth2.Endpoint{
					TokenURL: provider.URL, AuthStyle: oauth2.AuthStyleInParams,
				}},
				verifier: oidc.NewVerifier(googleIssuer, &oidc.StaticKeySet{
					PublicKeys: []crypto.PublicKey{publicKey},
				}, &oidc.Config{ClientID: "client"}),
				accounts: store, sessions: googleTestSessions{}, frontend: "https://app.example.com",
				secure: true,
			}

			request := httptest.NewRequest(http.MethodGet, "/api/v1/auth/google/callback?code=auth-code&state="+url.QueryEscape(tc.callbackState), nil)
			request.AddCookie(signIn.flowCookie(signIn.signedFlow("state", "verifier", "nonce", googleUserFlow, 0), 60))
			response := httptest.NewRecorder()
			signIn.callback(response, request)
			if response.Code != http.StatusSeeOther || response.Header().Get("Location") != signIn.frontend+tc.wantRedirect {
				t.Fatalf("unexpected callback response: %d %s", response.Code, response.Header().Get("Location"))
			}
			if (store.identity.Subject == "google-user") != tc.wantAccount {
				t.Fatalf("unexpected account resolution: %+v", store.identity)
			}
			cookies := strings.Join(response.Header().Values("Set-Cookie"), "\n")
			if strings.Contains(cookies, "ACCESS_TOKEN=") != (tc.wantRedirect == "/") {
				t.Fatalf("unexpected session cookie issuance")
			}
		})
	}
}
