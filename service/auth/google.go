package auth

import (
	"context"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strconv"

	"strings"
	"time"

	"github.com/SufyaanKhateeb/college-placement-app-api/config"
	"github.com/SufyaanKhateeb/college-placement-app-api/service/access"
	"github.com/SufyaanKhateeb/college-placement-app-api/types"
	"github.com/SufyaanKhateeb/college-placement-app-api/utils"
	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

const googleIssuer = "https://accounts.google.com"
const googleFlowLifetime = 10 * time.Minute

const (
	googleUserFlow      = "user"
	googleUserLinkFlow  = "user-link"
	googleAdminFlow     = "admin"
	googleAdminLinkFlow = "admin-link"
	googleInviteFlow    = "admin-invite"
)

type GoogleIdentity struct {
	Issuer       string
	Subject      string
	Email        string
	FirstName    string
	LastName     string
	HostedDomain string
}

func (identity GoogleIdentity) IsPersonalGmail() bool {
	local, domain, ok := strings.Cut(identity.Email, "@")
	return ok && local != "" && strings.EqualFold(domain, "gmail.com") && identity.HostedDomain == ""
}

var ErrGoogleAccountExists = errors.New("existing account requires linking")

type GoogleAccountStore interface {
	ResolveGoogleAccount(context.Context, GoogleIdentity) (types.TokenInput, error)
	ResolveInvitedGoogleAccount(context.Context, GoogleIdentity, []byte) (types.TokenInput, error)
	FindGoogleAdmin(context.Context, GoogleIdentity) (types.TokenInput, error)
	CheckGoogleAdminPassword(context.Context, types.TokenInput, string) error
	LinkGoogleAdmin(context.Context, types.TokenInput, GoogleIdentity) error
	GoogleAdminLinked(context.Context, types.TokenInput, string) (bool, error)
	CheckGoogleUserPassword(context.Context, int, string) error
	LinkGoogleUser(context.Context, int, GoogleIdentity) error
	GoogleUserLinkStatus(context.Context, int, string) (bool, bool, error)
}

type GoogleSignIn struct {
	client   oauth2.Config
	verifier *oidc.IDTokenVerifier
	accounts GoogleAccountStore
	sessions types.AuthService
	frontend string
	secure   bool
}

func NewGoogleSignIn(ctx context.Context, accounts GoogleAccountStore, sessions types.AuthService, frontend string) (*GoogleSignIn, error) {
	clientID := os.Getenv("GOOGLE_OIDC_CLIENT_ID")
	secret := os.Getenv("GOOGLE_OIDC_CLIENT_SECRET")
	redirect := os.Getenv("GOOGLE_OIDC_REDIRECT_URL")
	if clientID == "" && secret == "" && redirect == "" {
		return nil, nil
	}
	if clientID == "" || secret == "" || redirect == "" {
		return nil, errors.New("Google sign-in requires client ID, client secret, and redirect URL")
	}
	callback, err := url.Parse(redirect)
	if err != nil || !validGoogleURL(callback) || callback.Path != "/api/v1/auth/google/callback" {
		return nil, errors.New("invalid Google sign-in callback URL")
	}
	origin, err := url.Parse(frontend)
	if err != nil || !validGoogleURL(origin) || (origin.Path != "" && origin.Path != "/") || origin.Scheme != callback.Scheme {
		return nil, errors.New("Google sign-in requires a valid frontend origin with the callback scheme")
	}
	keySet := oidc.NewRemoteKeySet(ctx, "https://www.googleapis.com/oauth2/v3/certs")
	return &GoogleSignIn{
		client: oauth2.Config{
			ClientID:     clientID,
			ClientSecret: secret,
			RedirectURL:  redirect,
			Endpoint: oauth2.Endpoint{
				AuthURL:   googleIssuer + "/o/oauth2/v2/auth",
				TokenURL:  "https://oauth2.googleapis.com/token",
				AuthStyle: oauth2.AuthStyleInParams,
			},
			Scopes: []string{oidc.ScopeOpenID, "email", "profile"},
		},
		verifier: oidc.NewVerifier(googleIssuer, keySet, &oidc.Config{ClientID: clientID}),
		accounts: accounts,
		sessions: sessions,
		frontend: strings.TrimSuffix(origin.String(), "/"),
		secure:   callback.Scheme == "https",
	}, nil
}

func validGoogleURL(address *url.URL) bool {
	if address.Hostname() == "" || address.User != nil || address.RawQuery != "" || address.ForceQuery || address.Fragment != "" {
		return false
	}
	return address.Scheme == "https" || (address.Scheme == "http" && address.Hostname() == "localhost")
}

func (g *GoogleSignIn) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/auth/providers", func(w http.ResponseWriter, _ *http.Request) {
		utils.WriteJson(w, http.StatusOK, map[string]bool{"google": g != nil})
	})
	if g != nil {
		mux.HandleFunc("GET /api/v1/auth/google", g.start)
		mux.HandleFunc("GET /api/v1/auth/google/admin", g.startAdmin)
		mux.HandleFunc("GET /api/v1/auth/google/invitation", g.startInvitation)
		mux.HandleFunc("POST /api/v1/auth/google/admin/link", g.startAdminLink)
		mux.HandleFunc("GET /api/v1/auth/google/admin/link", g.adminLinkStatus)
		mux.HandleFunc("POST /api/v1/auth/google/link", g.startUserLink)
		mux.HandleFunc("GET /api/v1/auth/google/link", g.userLinkStatus)
		mux.HandleFunc("GET /api/v1/auth/google/callback", g.callback)
	}
}

func (g *GoogleSignIn) flowCookie(value string, maxAge int) *http.Cookie {
	name := "placement_google_flow"
	if g.secure {
		name = "__Host-" + name
	}
	return &http.Cookie{
		Name: name, Value: value, Path: "/", MaxAge: maxAge,
		HttpOnly: true, Secure: g.secure, SameSite: http.SameSiteLaxMode,
	}
}

func (g *GoogleSignIn) start(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, g.begin(w, googleUserFlow, "0"), http.StatusFound)
}

func (g *GoogleSignIn) startInvitation(w http.ResponseWriter, r *http.Request) {
	ref := g.invitationReference(r)
	if ref == "" {
		g.redirectError(w, r, googleInviteFlow, "google_failed")
		return
	}
	http.Redirect(w, r, g.begin(w, googleInviteFlow, ref), http.StatusFound)
}

func (g *GoogleSignIn) invitationReference(r *http.Request) string {
	cookie, err := r.Cookie(utils.AdminInvitationCookie(g.frontend, "", 0).Name)
	if err != nil {
		return ""
	}
	hash, err := access.InvitationHash(cookie.Value)
	if err != nil {
		return ""
	}
	return hex.EncodeToString(hash)
}

func (g *GoogleSignIn) begin(w http.ResponseWriter, mode, reference string) string {
	state := oauth2.GenerateVerifier()
	verifier := oauth2.GenerateVerifier()
	nonce := oauth2.GenerateVerifier()
	w.Header().Set("Cache-Control", "no-store")
	http.SetCookie(w, g.flowCookie(g.signedFlow(state, verifier, nonce, mode, reference), int(googleFlowLifetime.Seconds())))
	return g.client.AuthCodeURL(
		state,
		oauth2.S256ChallengeOption(verifier),
		oidc.Nonce(nonce),
		oauth2.SetAuthURLParam("prompt", "select_account"),
	)
}

func (g *GoogleSignIn) callback(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	flow := g.flowCookie("", -1)
	http.SetCookie(w, flow)
	cookie, err := r.Cookie(flow.Name)
	mode := googleUserFlow
	var params []string
	if err == nil {
		params = g.verifyFlow(cookie.Value)
		if len(params) == 6 {
			mode = params[3]
		}
	}
	code := r.URL.Query().Get("code")
	if err != nil || code == "" {
		slog.Warn("Google sign-in callback missing flow cookie or code", "flow_cookie_present", err == nil, "code_present", code != "", "provider_error", googleErrorCode(r.URL.Query().Get("error")))
		g.redirectError(w, r, mode, "google_failed")
		return
	}
	if len(params) != 6 || params[0] == "" || subtle.ConstantTimeCompare([]byte(params[0]), []byte(r.URL.Query().Get("state"))) != 1 {
		slog.Warn("Google sign-in state mismatch")
		g.redirectError(w, r, mode, "google_failed")
		return
	}

	var admin types.TokenInput
	var user types.TokenInput
	if mode == googleUserLinkFlow {
		var ok bool
		user, ok = g.activeUser(r)
		if !ok || params[4] != strconv.Itoa(user.Id) {
			g.redirectError(w, r, mode, "google_user_session_expired")
			return
		}
	}
	if mode == googleAdminLinkFlow {
		var ok bool
		admin, ok = g.adminLinkAccount(r, params[4])
		if !ok {
			g.redirectError(w, r, googleAdminFlow, "google_admin_session_expired")
			return
		}
	}
	if mode == googleInviteFlow && (params[4] == "" || params[4] != g.invitationReference(r)) {
		g.redirectError(w, r, mode, "google_failed")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	identity, err := g.exchange(ctx, code, params[1], params[2])
	if err != nil {
		g.redirectError(w, r, mode, "google_failed")
		return
	}
	if mode == googleUserLinkFlow {
		g.finishUserLink(w, r, ctx, user.Id, identity)
		return
	}
	if mode == googleAdminLinkFlow {
		err = g.accounts.LinkGoogleAdmin(ctx, admin, identity)
		switch {
		case err == nil:
			http.Redirect(w, r, g.frontend+"/admin?status=google_connected", http.StatusSeeOther)
		case errors.Is(err, ErrGoogleAdminEmailMismatch):
			g.redirectError(w, r, mode, "google_admin_email_mismatch")
		case errors.Is(err, ErrGoogleAdminLinkConflict):
			g.redirectError(w, r, mode, "google_admin_link_conflict")
		default:
			slog.Warn("Google admin connection failed")
			g.redirectError(w, r, mode, "google_failed")
		}
		return
	}

	var account types.TokenInput
	switch mode {
	case googleAdminFlow:
		account, err = g.accounts.FindGoogleAdmin(ctx, identity)
		if errors.Is(err, ErrGoogleAdminUnlinked) {
			g.redirectError(w, r, mode, "google_admin_unlinked")
			return
		}
	case googleInviteFlow:
		hash, _ := hex.DecodeString(params[4])
		account, err = g.accounts.ResolveInvitedGoogleAccount(ctx, identity, hash)
	default:
		account, err = g.accounts.ResolveGoogleAccount(ctx, identity)
	}
	if errors.Is(err, access.ErrInvitationRequired) {
		g.redirectError(w, r, mode, "google_invitation_required")
		return
	}
	if errors.Is(err, access.ErrInvitationAccount) || errors.Is(err, access.ErrInvalidInvitation) {
		g.redirectError(w, r, mode, "google_invitation_mismatch")
		return
	}
	if errors.Is(err, ErrGoogleAccountExists) {
		g.redirectError(w, r, mode, "google_link_required")
		return
	}
	if err != nil {
		var databaseError interface{ SQLState() string }
		if errors.As(err, &databaseError) {
			slog.Warn("Google sign-in account lookup failed", "sqlstate", databaseError.SQLState())
		} else {
			slog.Warn("Google sign-in account lookup failed", "timed_out", errors.Is(err, context.DeadlineExceeded))
		}
		g.redirectError(w, r, mode, "google_failed")
		return
	}

	access, refresh, err := g.sessions.CreateTokens(&account)
	if err != nil {
		slog.Warn("Google sign-in session creation failed")
		g.redirectError(w, r, mode, "google_failed")
		return
	}
	g.sessionCookie(w, "ACCESS_TOKEN", access, time.Duration(config.Env.JWTExpirationTime)*time.Second)
	g.sessionCookie(w, "REFRESH_TOKEN", refresh, 30*24*time.Hour)
	destination := "/"
	if mode == googleInviteFlow {
		destination = "/admin/invitation"
	} else if account.IsAdmin() {
		destination = "/admin"
	}
	http.Redirect(w, r, g.frontend+destination, http.StatusSeeOther)
}

func (g *GoogleSignIn) exchange(ctx context.Context, code, verifier, nonce string) (GoogleIdentity, error) {
	token, err := g.client.Exchange(ctx, code, oauth2.VerifierOption(verifier))
	if err != nil {
		var providerError *oauth2.RetrieveError
		if errors.As(err, &providerError) {
			slog.Warn("Google sign-in token exchange rejected", "provider_error", googleErrorCode(providerError.ErrorCode))
		} else {
			slog.Warn("Google sign-in token exchange failed", "timed_out", errors.Is(err, context.DeadlineExceeded))
		}
		return GoogleIdentity{}, err
	}
	raw, ok := token.Extra("id_token").(string)
	if !ok {
		slog.Warn("Google sign-in response missing ID token")
		return GoogleIdentity{}, errors.New("missing Google ID token")
	}
	verified, err := g.verifier.Verify(ctx, raw)
	if err != nil {
		slog.Warn("Google sign-in ID token verification failed", "timed_out", errors.Is(err, context.DeadlineExceeded))
		return GoogleIdentity{}, err
	}
	var claims struct {
		Email         string `json:"email"`
		EmailVerified bool   `json:"email_verified"`
		HostedDomain  string `json:"hd"`
		FirstName     string `json:"given_name"`
		LastName      string `json:"family_name"`
	}
	if err := verified.Claims(&claims); err != nil {
		slog.Warn("Google sign-in could not decode identity claims")
		return GoogleIdentity{}, err
	}
	if verified.Subject == "" || nonce == "" || verified.Nonce != nonce || !claims.EmailVerified || claims.Email == "" {
		slog.Warn("Google sign-in identity claims rejected", "subject_present", verified.Subject != "", "nonce_matches", nonce != "" && verified.Nonce == nonce, "email_verified", claims.EmailVerified, "email_present", claims.Email != "")
		return GoogleIdentity{}, errors.New("Google identity is missing required verified claims")
	}
	return GoogleIdentity{Issuer: googleIssuer, Subject: verified.Subject, Email: claims.Email, FirstName: claims.FirstName, LastName: claims.LastName, HostedDomain: claims.HostedDomain}, nil
}

func (g *GoogleSignIn) sessionCookie(w http.ResponseWriter, name, value string, lifetime time.Duration) {
	http.SetCookie(w, &http.Cookie{
		Name: name, Value: value, Path: "/", MaxAge: int(lifetime.Seconds()),
		HttpOnly: true, Secure: g.secure, SameSite: http.SameSiteLaxMode,
	})
}

func (g *GoogleSignIn) redirectError(w http.ResponseWriter, r *http.Request, mode, code string) {
	destination := "/login"
	switch mode {
	case googleUserLinkFlow:
		destination = "/account"
	case googleAdminFlow:
		destination = "/admin/login"
	case googleAdminLinkFlow:
		destination = "/admin"
	case googleInviteFlow:
		destination = "/admin/invitation"
	}
	http.Redirect(w, r, g.frontend+destination+"?error="+url.QueryEscape(code), http.StatusSeeOther)
}

func googleErrorCode(code string) string {
	switch code {
	case "", "access_denied", "invalid_client", "invalid_grant", "invalid_request", "redirect_uri_mismatch", "unauthorized_client", "server_error", "temporarily_unavailable":
		return code
	default:
		return "other"
	}
}
