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
	"github.com/SufyaanKhateeb/college-placement-app-api/service/auth"
	"github.com/SufyaanKhateeb/college-placement-app-api/service/user"
	"github.com/SufyaanKhateeb/college-placement-app-api/types"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPasswordEmailVerificationAndGoogleLinkPostgres(t *testing.T) {
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

	const origin = "http://localhost:5173"
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	previous := config.Env
	t.Cleanup(func() { config.Env = previous })
	config.Env.PrivateKey, config.Env.PublicKey = key, &key.PublicKey
	config.Env.JWTExpirationTime, config.Env.FrontendOrigin = 300, origin

	prefix := fmt.Sprintf("pla17-%d-", time.Now().UnixNano())
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := db.Exec(ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	defer func() {
		for _, table := range []string{"email_verifications", "app_user", "admin_user"} {
			if _, err := db.Exec(context.Background(), "DELETE FROM "+table+" WHERE lower(email) LIKE $1", prefix+"%"); err != nil {
				t.Errorf("cleanup %s: %v", table, err)
			}
		}
	}()

	const password = "FixturePassword!8"
	hash, err := auth.HashPassword(password)
	if err != nil {
		t.Fatal(err)
	}
	var mail = map[string]string{}
	var deliveries int
	handler := newHandler(db, origin, nil, nil, func(_ context.Context, email, code string) error {
		deliveries++
		mail[email] = code
		return nil
	})
	request := func(method, path string, payload any, cookie *http.Cookie, want int) *httptest.ResponseRecorder {
		t.Helper()
		body, err := json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(method, "/api/v1"+path, strings.NewReader(string(body)))
		req.Header.Set("Origin", origin)
		if cookie != nil {
			req.AddCookie(cookie)
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		if response.Code != want {
			t.Fatalf("%s %s returned %d, want %d: %s", method, path, response.Code, want, response.Body)
		}
		return response
	}
	cookie := func(response *httptest.ResponseRecorder, name string) *http.Cookie {
		for _, cookie := range response.Result().Cookies() {
			if cookie.Name == name {
				return cookie
			}
		}
		return nil
	}
	pending := func(response *httptest.ResponseRecorder) *http.Cookie {
		t.Helper()
		grant := cookie(response, "placement_email_verification")
		if grant == nil || !grant.HttpOnly || cookie(response, "ACCESS_TOKEN") != nil || cookie(response, "REFRESH_TOKEN") != nil {
			t.Fatal("pending verification should issue only an HttpOnly challenge cookie")
		}
		return grant
	}
	profileID := func(response *httptest.ResponseRecorder) int {
		t.Helper()
		access := cookie(response, "ACCESS_TOKEN")
		if access == nil || cookie(response, "REFRESH_TOKEN") == nil {
			t.Fatal("missing session cookies")
		}
		profile := request("GET", "/user", nil, access, http.StatusOK)
		var data types.UserDto
		if err := json.Unmarshal(profile.Body.Bytes(), &data); err != nil || data.Id <= 0 || data.Role != types.RoleUser {
			t.Fatalf("expected a regular session: %s (%v)", profile.Body, err)
		}
		return data.Id
	}
	const confirm = "/auth/email-verification/confirm"
	const resend = "/auth/email-verification/resend"
	confirmWith := func(grant *http.Cookie, code string, status int) *httptest.ResponseRecorder {
		t.Helper()
		return request("POST", confirm, map[string]string{"code": code}, grant, status)
	}
	login := func(email, password string, status int) *httptest.ResponseRecorder {
		t.Helper()
		return request("POST", "/login", map[string]string{"email": email, "password": password}, nil, status)
	}
	store := user.NewStore(db)
	google := func(email, name string) auth.GoogleIdentity {
		return auth.GoogleIdentity{Issuer: "https://accounts.google.com", Subject: prefix + name, Email: email}
	}

	newEmail := prefix + "new@gmail.com"
	registration := map[string]string{"firstName": "New", "lastName": "Member", "email": newEmail, "password": password}
	grant := pending(request("POST", "/register", registration, nil, http.StatusAccepted))
	code := mail[newEmail]
	if len(code) != 6 {
		t.Fatal("registration should send a six-digit code to the requested address")
	}
	if _, err := store.GetUserByEmail(newEmail); err == nil {
		t.Fatal("unverified registration created a password account")
	}
	confirmWith(nil, code, http.StatusGone)
	newID := profileID(confirmWith(grant, code, http.StatusOK))
	confirmWith(grant, code, http.StatusGone)
	if got := profileID(login(newEmail, password, http.StatusOK)); got != newID {
		t.Fatalf("verified registration changed ID: got %d, want %d", got, newID)
	}
	for range 2 {
		linked, err := store.ResolveGoogleAccount(ctx, google(newEmail, "new"))
		if err != nil || linked.Id != newID || linked.IsAdmin() {
			t.Fatalf("Gmail link should resolve to original account: %+v, %v", linked, err)
		}
	}
	if _, err := store.ResolveGoogleAccount(ctx, google(newEmail, "other-subject")); !errors.Is(err, auth.ErrGoogleAccountExists) {
		t.Fatalf("existing link should block a different Google subject: %v", err)
	}

	existing := prefix + "legacy@gmail.com"
	var existingID int
	if err := db.QueryRow(ctx, `
		INSERT INTO app_user (firstname, lastname, email, password) VALUES ('Existing','Member',$1,$2) RETURNING id
	`, existing, hash).Scan(&existingID); err != nil {
		t.Fatal(err)
	}
	existingGoogle := google(existing, "legacy")
	if _, err := store.ResolveGoogleAccount(ctx, existingGoogle); !errors.Is(err, auth.ErrGoogleAccountExists) {
		t.Fatalf("unverified password account should block Google linking: %v", err)
	}
	login(existing, "incorrect", http.StatusBadRequest)
	if deliveries != 1 {
		t.Fatal("an incorrect password should not send a verification email")
	}
	grant = pending(login(existing, password, http.StatusAccepted))
	request("POST", resend, nil, grant, http.StatusTooManyRequests)
	expiredCode := mail[existing]
	exec("UPDATE email_verification_challenges SET expires_at = clock_timestamp() WHERE email = $1", existing)
	confirmWith(grant, expiredCode, http.StatusGone)
	exec("UPDATE email_verifications SET sent_at = ARRAY[clock_timestamp() - interval '2 minutes'] WHERE email = $1", existing)
	request("POST", resend, nil, grant, http.StatusAccepted)
	code = mail[existing]
	wrong := "000000"
	if code == wrong {
		wrong = "000001"
	}
	for range 5 {
		confirmWith(grant, wrong, http.StatusBadRequest)
	}
	exec("UPDATE email_verifications SET sent_at = ARRAY[clock_timestamp() - interval '4 minutes', clock_timestamp() - interval '2 minutes'] WHERE email = $1", existing)
	request("POST", resend, nil, grant, http.StatusAccepted)
	confirmWith(grant, mail[existing], http.StatusTooManyRequests)
	exec(`UPDATE email_verifications SET sent_at = ARRAY(SELECT sent - interval '2 minutes' FROM unnest(sent_at) AS sent) WHERE email = $1`, existing)
	request("POST", resend, nil, grant, http.StatusTooManyRequests)
	exec("UPDATE email_verifications SET failed_at = '{}' WHERE email = $1", existing)
	if got := profileID(confirmWith(grant, mail[existing], http.StatusOK)); got != existingID {
		t.Fatalf("verified existing login changed ID: got %d, want %d", got, existingID)
	}
	if linked, err := store.ResolveGoogleAccount(ctx, existingGoogle); err != nil || linked.Id != existingID {
		t.Fatalf("verified existing account should link: %+v, %v", linked, err)
	}

	for _, account := range []struct{ name, domain, hostedDomain string }{
		{"workspace", "example.invalid", "example.invalid"},
		{"external", "example.invalid", ""},
		{"promoted", "gmail.com", ""},
		{"ambiguous", "gmail.com", ""},
		{"admin", "gmail.com", ""},
	} {
		email := prefix + account.name + "@" + account.domain
		if account.name == "admin" {
			exec("INSERT INTO admin_user (firstname, lastname, email, password) VALUES ('Legacy','Admin',$1,$2)", email, hash)
		} else {
			exec("INSERT INTO app_user (firstname, lastname, email, password) VALUES ('Other','Member',$1,$2)", email, hash)
			exec("INSERT INTO verified_user_emails SELECT id, lower(email) FROM app_user WHERE email = $1", email)
		}
		if account.name == "promoted" {
			exec("INSERT INTO admin_memberships (user_id) SELECT id FROM app_user WHERE email = $1", email)
		}
		if account.name == "ambiguous" {
			exec("INSERT INTO app_user (firstname, lastname, email, password) VALUES ('Other','Member',$1,$2)", strings.ToUpper(email), hash)
		}
		identity := google(email, account.name)
		identity.HostedDomain = account.hostedDomain
		if _, err := store.ResolveGoogleAccount(ctx, identity); !errors.Is(err, auth.ErrGoogleAccountExists) {
			t.Fatalf("account should require an explicit connection: %v", err)
		}
		if account.name == "external" {
			member, err := store.GetUserByEmail(email)
			if err != nil {
				t.Fatal(err)
			}
			if err := store.CheckGoogleUserPassword(ctx, member.Id, password); err != nil {
				t.Fatal(err)
			}
			if err := store.LinkGoogleUser(ctx, member.Id, identity); err != nil {
				t.Fatal(err)
			}
			if linked, err := store.ResolveGoogleAccount(ctx, identity); err != nil || linked.Id != member.Id {
				t.Fatalf("explicit link should resolve to original account: %+v, %v", linked, err)
			}
		}
	}
}
