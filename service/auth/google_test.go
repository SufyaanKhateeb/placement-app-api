package auth

import (
	"bytes"
	"context"
	"crypto"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/SufyaanKhateeb/college-placement-app-api/service/access"
	"github.com/SufyaanKhateeb/college-placement-app-api/types"
	"github.com/SufyaanKhateeb/college-placement-app-api/utils"
	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/oauth2"
)

type googleTestStore struct {
	GoogleAccountStore
	identity       GoogleIdentity
	err            error
	invitationHash []byte
	linkedUser     int
}

func (s *googleTestStore) ResolveGoogleAccount(_ context.Context, identity GoogleIdentity) (types.TokenInput, error) {
	s.identity = identity
	return types.TokenInput{Id: 7, Type: types.RoleUser}, s.err
}

func (s *googleTestStore) ResolveInvitedGoogleAccount(ctx context.Context, identity GoogleIdentity, hash []byte) (types.TokenInput, error) {
	s.invitationHash = hash
	return s.ResolveGoogleAccount(ctx, identity)
}

func (s *googleTestStore) LinkGoogleUser(_ context.Context, id int, identity GoogleIdentity) error {
	s.linkedUser = id
	s.identity = identity
	return s.err
}

type googleTestSessions struct{ googleAdminTestSessions }

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
		invitation    bool
		changeCookie  bool
		userLink      bool
	}{
		{"valid identity", "state", "nonce", true, nil, "/", true, false, false, false},
		{"legacy account requires linking", "state", "nonce", true, ErrGoogleAccountExists, "/login?error=google_link_required", true, false, false, false},
		{"state mismatch", "wrong", "nonce", true, nil, "/login?error=google_failed", false, false, false, false},
		{"nonce mismatch", "state", "wrong", true, nil, "/login?error=google_failed", false, false, false, false},
		{"unverified email", "state", "nonce", false, nil, "/login?error=google_failed", false, false, false, false},
		{"invited Google identity", "state", "nonce", true, nil, "/admin/invitation", true, true, false, false},
		{"invitation cookie changed during OAuth", "state", "nonce", true, nil, "/admin/invitation?error=google_failed", false, true, true, false},
		{"invited but unverified", "state", "nonce", false, nil, "/admin/invitation?error=google_failed", false, true, false, false},
		{"explicit user connection", "state", "nonce", true, nil, "/account", true, false, false, true},
		{"explicit user connection with a different session", "state", "nonce", true, nil, "/account?error=google_user_session_expired", false, false, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw, err := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
				"iss": googleIssuer, "aud": "client", "sub": "google-user",
				"exp": time.Now().Add(time.Minute).Unix(), "nonce": tc.tokenNonce,
				"email": "user@example.com", "email_verified": tc.emailVerified, "hd": "example.com",
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
			mode, ref := googleUserFlow, "0"
			inviteToken := strings.Repeat("A", 43)
			if tc.invitation {
				mode = googleInviteFlow
				inviteRequest := httptest.NewRequest(http.MethodGet, "/", nil)
				inviteRequest.AddCookie(utils.AdminInvitationCookie(signIn.frontend, inviteToken, time.Hour))
				ref = signIn.invitationReference(inviteRequest)
				cookieToken := inviteToken
				if tc.changeCookie {
					cookieToken = "B" + inviteToken[1:]
				}
				request.AddCookie(utils.AdminInvitationCookie(signIn.frontend, cookieToken, time.Hour))
			}
			if tc.userLink {
				mode, ref = googleUserLinkFlow, "7"
				if tc.changeCookie {
					ref = "9"
				}
				request.AddCookie(&http.Cookie{Name: "ACCESS_TOKEN", Value: types.RoleUser})
			}
			request.AddCookie(signIn.flowCookie(signIn.signedFlow("state", "verifier", "nonce", mode, ref), 60))
			response := httptest.NewRecorder()
			signIn.callback(response, request)
			if response.Code != http.StatusSeeOther || response.Header().Get("Location") != signIn.frontend+tc.wantRedirect {
				t.Fatalf("unexpected callback response: %d %s", response.Code, response.Header().Get("Location"))
			}
			if (store.identity.Subject == "google-user") != tc.wantAccount {
				t.Fatalf("unexpected account resolution: %+v", store.identity)
			}
			if tc.wantAccount && store.identity.HostedDomain != "example.com" {
				t.Fatal("verified Google hosted domain did not reach account resolution")
			}
			if tc.userLink && tc.wantAccount && store.linkedUser != 7 {
				t.Fatal("callback connected Google to the wrong user")
			}
			cookies := strings.Join(response.Header().Values("Set-Cookie"), "\n")
			if strings.Contains(cookies, "ACCESS_TOKEN=") != (tc.wantRedirect == "/" || tc.wantRedirect == "/admin/invitation") {
				t.Fatalf("unexpected session cookie issuance")
			}
			if tc.invitation && tc.wantAccount {
				want, _ := access.InvitationHash(inviteToken)
				if !bytes.Equal(store.invitationHash, want) {
					t.Fatal("callback did not use the invitation bound to the OAuth state")
				}
			}
		})
	}
}
