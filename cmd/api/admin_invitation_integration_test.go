package api

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/SufyaanKhateeb/college-placement-app-api/config"
	"github.com/SufyaanKhateeb/college-placement-app-api/service/access"
	"github.com/SufyaanKhateeb/college-placement-app-api/service/auth"
	"github.com/SufyaanKhateeb/college-placement-app-api/service/user"
	"github.com/SufyaanKhateeb/college-placement-app-api/types"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestAdminInvitationAndPromotionPostgres(t *testing.T) {
	database := os.Getenv("PLACEMENT_TEST_DATABASE_URL")
	if database == "" {
		t.Skip("requires a migrated test database in PLACEMENT_TEST_DATABASE_URL")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	db, err := pgxpool.New(ctx, database)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	const origin = "http://localhost:5173"
	previous := config.Env
	t.Cleanup(func() { config.Env = previous })
	config.Env.PrivateKey, config.Env.PublicKey = key, &key.PublicKey
	config.Env.JWTExpirationTime, config.Env.FrontendOrigin = 300, origin

	stamp := time.Now().UnixNano()
	fixturePrefix := fmt.Sprintf("pla19-%d-", stamp)
	address := func(name string) string { return fixturePrefix + name + "@example.invalid" }
	managerEmail, ordinaryEmail, memberEmail, inviteEmail := address("manager"), address("admin"), address("member"), address("invite")
	const password = "FixturePassword!8"
	hash, err := auth.HashPassword(password)
	if err != nil {
		t.Fatal(err)
	}
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := db.Exec(ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	id := func(query string, args ...any) int {
		t.Helper()
		var value int
		if err := db.QueryRow(ctx, query, args...).Scan(&value); err != nil {
			t.Fatal(err)
		}
		return value
	}

	var formID, subID int
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		for _, entry := range []struct {
			query string
			arg   any
		}{
			{"DELETE FROM form_verifications WHERE submission_id = $1", subID},
			{"DELETE FROM form_submissions WHERE id = $1", subID},
			{"DELETE FROM form_versions WHERE form_id = $1", formID},
			{"DELETE FROM forms WHERE id = $1", formID},
			{"DELETE FROM admin_invitations WHERE email LIKE $1", fixturePrefix + "%"},
			{"DELETE FROM app_user WHERE email LIKE $1", fixturePrefix + "%"},
			{"DELETE FROM admin_user WHERE email LIKE $1", fixturePrefix + "%"},
		} {
			if _, err := db.Exec(cleanup, entry.query, entry.arg); err != nil {
				t.Errorf("fixture cleanup: %v", err)
			}
		}
	}()

	managerID := int(500000000 + stamp%100000000)
	exec("INSERT INTO admin_user (id, firstname, lastname, email, password) VALUES ($1,'Role','Manager',$2,$3)", managerID, managerEmail, hash)
	exec("INSERT INTO admin_user (firstname, lastname, email, password) VALUES ('Ordinary','Admin',$1,$2)", ordinaryEmail, hash)
	managerRole := id("SELECT id FROM access_roles WHERE key = 'role-manager'")
	exec("INSERT INTO access_assignments VALUES ('admin', $1, $2) ON CONFLICT DO NOTHING", managerID, managerRole)
	// The member and legacy admin can have identical numeric IDs.
	memberID := id("INSERT INTO app_user (id, firstname, lastname, email, password) VALUES ($1,'Existing','Member',$2,$3) RETURNING id", managerID, memberEmail, hash)
	identity := auth.GoogleIdentity{Issuer: "https://accounts.google.com", Subject: fixturePrefix + "subject", Email: memberEmail}
	exec("INSERT INTO oidc_identity (issuer, subject, user_id) VALUES ($1,$2,$3)", identity.Issuer, identity.Subject, memberID)
	definition := `{"title":"PLA-19 fixture","fields":[]}`
	formID = id("INSERT INTO forms (draft) VALUES ($1::jsonb) RETURNING id", definition)
	exec("INSERT INTO form_versions (form_id, version, definition) VALUES ($1,1,$2::jsonb)", formID, definition)
	subID = id("INSERT INTO form_submissions (form_id, user_id, form_version, answers, status) VALUES ($1,$2,1,'{}','approved') RETURNING id", formID, memberID)
	exec("INSERT INTO form_verifications (submission_id) VALUES ($1)", subID)

	type capturedMail struct{ email, link string }
	var mail []capturedMail
	handler := newHandler(db, origin, nil, func(_ context.Context, email, link string) error {
		mail = append(mail, capturedMail{email, link})
		if email == address("delivery-failure") {
			return errors.New("fixture delivery failure")
		}
		return nil
	})
	send := func(method, path, body string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, "/api/v1"+path, strings.NewReader(body)).WithContext(ctx)
		req.Header.Set("Origin", origin)
		req.Header.Set("Content-Type", "application/json")
		for _, cookie := range cookies {
			req.AddCookie(cookie)
		}
		res := httptest.NewRecorder()
		handler.ServeHTTP(res, req)
		return res
	}
	check := func(status int, res *httptest.ResponseRecorder) *httptest.ResponseRecorder {
		t.Helper()
		if res.Code != status {
			t.Fatalf("HTTP %d, want %d: %s", res.Code, status, res.Body.String())
		}
		return res
	}
	cookie := func(res *httptest.ResponseRecorder, name string) *http.Cookie {
		t.Helper()
		for _, c := range res.Result().Cookies() {
			if c.Name == name {
				return c
			}
		}
		t.Fatalf("missing %s cookie", name)
		return nil
	}
	login := func(path, email string, status int) *httptest.ResponseRecorder {
		t.Helper()
		return check(status, send("POST", path, fmt.Sprintf(`{"email":%q,"password":%q}`, email, password)))
	}
	profile := func(session *http.Cookie, wantID int, wantRole string) types.UserDto {
		t.Helper()
		res := check(http.StatusOK, send("GET", "/user", "", session))
		var result types.UserDto
		if err := json.Unmarshal(res.Body.Bytes(), &result); err != nil || result.Id != wantID || result.Role != wantRole {
			t.Fatalf("wrong profile: %s (%v)", res.Body.String(), err)
		}
		return result
	}

	manager := cookie(login("/login-admin", managerEmail, http.StatusOK), "ACCESS_TOKEN")
	ordinary := cookie(login("/login-admin", ordinaryEmail, http.StatusOK), "ACCESS_TOKEN")
	memberLogin := login("/login", memberEmail, http.StatusOK)
	member := cookie(memberLogin, "ACCESS_TOKEN")
	oldRefresh := cookie(memberLogin, "REFRESH_TOKEN")
	const invites = "/admin/access/invitations"
	const accept = "/auth/admin-invitation/accept"
	const open = "/auth/admin-invitation/open"
	const register = "/auth/admin-invitation/register"
	promote := fmt.Sprintf("/admin/access/accounts/user/%d/promote", memberID)
	memberBody := fmt.Sprintf(`{"email":%q}`, memberEmail)
	inviteBody := fmt.Sprintf(`{"email":%q}`, strings.ToUpper(inviteEmail))

	check(http.StatusUnauthorized, send("POST", invites, inviteBody))
	for _, actor := range []*http.Cookie{ordinary, member} {
		check(http.StatusForbidden, send("POST", invites, inviteBody, actor))
		check(http.StatusForbidden, send("POST", promote, memberBody, actor))
	}
	if len(mail) != 0 {
		t.Fatal("an unauthorized invitation called the mailer")
	}
	check(http.StatusConflict, send("POST", invites, memberBody, manager))
	check(http.StatusConflict, send("POST", promote, fmt.Sprintf(`{"email":%q}`, inviteEmail), manager))
	check(http.StatusGone, send("POST", "/register-admin", "{}", manager))
	login("/login-admin", memberEmail, http.StatusBadRequest)

	check(http.StatusNoContent, send("POST", promote, memberBody, manager))
	check(http.StatusConflict, send("POST", promote, memberBody, manager))
	profile(member, memberID, "user")
	profile(oldRefresh, memberID, "user")
	check(http.StatusForbidden, send("GET", "/admin/forms", "", oldRefresh))
	promoted := cookie(login("/login-admin", memberEmail, http.StatusOK), "ACCESS_TOKEN")
	if result := profile(promoted, memberID, "admin"); result.AccountType != "user" {
		t.Fatal("admin mode changed the account type used for submission ownership")
	}
	check(http.StatusOK, send("GET", fmt.Sprintf("/forms/%d/submission", formID), "", promoted))
	check(http.StatusOK, send("GET", "/admin/forms", "", promoted))
	check(http.StatusOK, send("GET", "/admin/submissions", "", promoted))
	check(http.StatusForbidden, send("POST", invites, inviteBody, promoted))
	if saved := id("SELECT s.user_id FROM form_submissions s JOIN form_verifications v ON v.submission_id = s.id WHERE s.id = $1", subID); saved != memberID {
		t.Fatalf("submission changed owner: %d", saved)
	}

	users := user.NewStore(db)
	memberGoogle, err := users.ResolveGoogleAccount(ctx, identity)
	if err != nil || memberGoogle.Id != memberID || memberGoogle.IsAdmin() {
		t.Fatalf("regular Google link changed: %+v, %v", memberGoogle, err)
	}
	adminGoogle, err := users.FindGoogleAdmin(ctx, identity)
	if err != nil || adminGoogle.Id != memberID || adminGoogle.Type != types.RoleUser || !adminGoogle.IsAdmin() {
		t.Fatalf("admin Google sign-in did not reuse the account: %+v, %v", adminGoogle, err)
	}
	check(http.StatusNoContent, send("PUT", fmt.Sprintf("/admin/access/accounts/user/%d/roles/%d", memberID, managerRole), "", manager))
	check(http.StatusOK, send("GET", "/admin/access", "", promoted))
	check(http.StatusForbidden, send("POST", invites, inviteBody, oldRefresh))

	issue := func(body string) string {
		t.Helper()
		check(http.StatusAccepted, send("POST", invites, body, manager))
		_, token, ok := strings.Cut(mail[len(mail)-1].link, "/admin/invitation#")
		if !ok || token == "" {
			t.Fatal("captured invitation did not include a link")
		}
		return token
	}
	openInvite := func(token string, status int) *httptest.ResponseRecorder {
		t.Helper()
		return check(status, send("POST", open, fmt.Sprintf(`{"token":%q}`, token)))
	}
	firstToken := issue(inviteBody)
	secondToken := issue(inviteBody)
	if firstToken == secondToken || mail[len(mail)-1].email != inviteEmail {
		t.Fatal("resend did not rotate the token or normalize the email")
	}
	openInvite(firstToken, http.StatusGone)
	inviteCookie := cookie(openInvite(secondToken, http.StatusOK), "placement_admin_invitation")

	registration := fmt.Sprintf(`{"email":%q,"firstName":"Fixture","lastName":"Invite","password":%q}`, inviteEmail, password)
	check(http.StatusConflict, send("POST", "/register", registration))
	check(http.StatusUnauthorized, send("POST", accept, "{}", inviteCookie))
	check(http.StatusConflict, send("POST", accept, "{}", member, inviteCookie))
	invited := cookie(check(http.StatusCreated, send("POST", register, registration, inviteCookie)), "ACCESS_TOKEN")
	check(http.StatusForbidden, send("GET", "/admin/forms", "", invited))
	check(http.StatusConflict, send("POST", accept, "{}", member, inviteCookie))

	results := make(chan int, 2)
	for range 2 {
		go func() { results <- send("POST", accept, "{}", invited, inviteCookie).Code }()
	}
	first, second := <-results, <-results
	if !((first == http.StatusNoContent && second == http.StatusGone) || (second == http.StatusNoContent && first == http.StatusGone)) {
		t.Fatalf("concurrent acceptance returned %d and %d", first, second)
	}
	check(http.StatusForbidden, send("GET", "/admin/forms", "", invited))
	invitedAdmin := cookie(login("/login-admin", inviteEmail, http.StatusOK), "ACCESS_TOKEN")
	invitedID := id("SELECT id FROM app_user WHERE email = $1", inviteEmail)
	profile(invitedAdmin, invitedID, "admin")
	check(http.StatusOK, send("GET", "/admin/forms", "", invitedAdmin))

	googleEmail := address("google-invite")
	googleToken := issue(fmt.Sprintf(`{"email":%q}`, googleEmail))
	googleHash, err := access.InvitationHash(googleToken)
	if err != nil {
		t.Fatal(err)
	}
	googleInvite := auth.GoogleIdentity{Issuer: identity.Issuer, Subject: fixturePrefix + "invited-google", Email: address("wrong"), FirstName: "Google", LastName: "Fixture"}
	if _, err := users.ResolveInvitedGoogleAccount(ctx, googleInvite, googleHash); !errors.Is(err, access.ErrInvitationAccount) {
		t.Fatalf("wrong Google address was accepted: %v", err)
	}
	googleInvite.Email = googleEmail
	if _, err := users.ResolveGoogleAccount(ctx, googleInvite); !errors.Is(err, access.ErrInvitationRequired) {
		t.Fatalf("normal Google enrollment bypassed the invitation: %v", err)
	}
	googleMember, err := users.ResolveInvitedGoogleAccount(ctx, googleInvite, googleHash)
	if err != nil || googleMember.Id == 0 || googleMember.IsAdmin() {
		t.Fatalf("Google enrollment should create a regular session: %+v, %v", googleMember, err)
	}
	if _, err := users.FindGoogleAdmin(ctx, googleInvite); !errors.Is(err, auth.ErrGoogleAdminUnlinked) {
		t.Fatalf("admin Google sign-in worked before acceptance: %v", err)
	}
	signed, _, err := (&auth.AuthService{}).CreateTokens(&googleMember)
	if err != nil {
		t.Fatal(err)
	}
	googleCookie := cookie(openInvite(googleToken, http.StatusOK), "placement_admin_invitation")
	check(http.StatusNoContent, send("POST", accept, "{}", googleCookie, &http.Cookie{Name: "ACCESS_TOKEN", Value: signed}))
	googleAdmin, err := users.FindGoogleAdmin(ctx, googleInvite)
	if err != nil || !googleAdmin.IsAdmin() || googleAdmin.Id != googleMember.Id {
		t.Fatalf("invite changed Google account identity: %+v, %v", googleAdmin, err)
	}

	expiredEmail := address("expired")
	expired := issue(fmt.Sprintf(`{"email":%q}`, expiredEmail))
	exec("UPDATE admin_invitations SET expires_at = clock_timestamp() WHERE email = $1", expiredEmail)
	openInvite(expired, http.StatusGone)
	revokedEmail := address("revoked-inviter")
	check(http.StatusAccepted, send("POST", invites, fmt.Sprintf(`{"email":%q}`, revokedEmail), promoted))
	_, revokedToken, _ := strings.Cut(mail[len(mail)-1].link, "/admin/invitation#")
	revokedCookie := cookie(openInvite(revokedToken, http.StatusOK), "placement_admin_invitation")
	beforeRevocation := cookie(check(http.StatusCreated, send("POST", register, registration, revokedCookie)), "ACCESS_TOKEN")
	check(http.StatusNoContent, send("DELETE", fmt.Sprintf("/admin/access/accounts/user/%d/roles/%d", memberID, managerRole), "", manager))
	check(http.StatusForbidden, send("POST", invites, inviteBody, promoted))
	check(http.StatusGone, send("POST", accept, "{}", beforeRevocation, revokedCookie))
	check(http.StatusBadGateway, send("POST", invites, fmt.Sprintf(`{"email":%q}`, address("delivery-failure")), manager))
	if audit := check(http.StatusOK, send("GET", "/admin/access/audit", "", manager)).Body.String(); !strings.Contains(audit, "admin.promoted") ||
		!strings.Contains(audit, "admin.invitation_accepted") || !strings.Contains(audit, "admin.invited") || !strings.Contains(audit, inviteEmail) {
		t.Fatalf("missing invitation or promotion history: %s", audit)
	}
}
