package api

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/SufyaanKhateeb/college-placement-app-api/config"
	"github.com/SufyaanKhateeb/college-placement-app-api/service/auth"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestRoleManagementPostgres(t *testing.T) {
	database := os.Getenv("PLACEMENT_TEST_DATABASE_URL")
	if database == "" {
		t.Skip("requires a migrated test database in PLACEMENT_TEST_DATABASE_URL")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, database)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	previous := config.Env
	t.Cleanup(func() { config.Env = previous })
	config.Env.PrivateKey = key
	config.Env.PublicKey = &key.PublicKey
	config.Env.JWTExpirationTime = 300

	stamp := time.Now().UnixNano()
	managerID := int(1000000000 + stamp%100000000)
	ordinaryID := managerID + 1
	memberID := managerID
	managerEmail := fmt.Sprintf("rbac-manager-%d@example.invalid", stamp)
	ordinaryEmail := fmt.Sprintf("rbac-admin-%d@example.invalid", stamp)
	memberEmail := fmt.Sprintf("rbac-member-%d@example.invalid", stamp)
	const password = "FixturePassword!8"
	hash, err := auth.HashPassword(password)
	if err != nil {
		t.Fatal(err)
	}
	var roleID int64
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := pool.Exec(cleanup, "DELETE FROM access_roles WHERE id = $1", roleID); err != nil {
			t.Errorf("clean custom role: %v", err)
		}
		if _, err := pool.Exec(cleanup, "DELETE FROM admin_user WHERE id IN ($1, $2)", managerID, ordinaryID); err != nil {
			t.Errorf("clean admins: %v", err)
		}
		if _, err := pool.Exec(cleanup, "DELETE FROM app_user WHERE id = $1", memberID); err != nil {
			t.Errorf("clean member: %v", err)
		}
	}()
	if _, err := pool.Exec(ctx, `
		INSERT INTO admin_user (id, firstname, lastname, email, password)
		VALUES ($1, 'Role', 'Manager', $2, $5), ($3, 'Ordinary', 'Admin', $4, $5)
	`, managerID, managerEmail, ordinaryID, ordinaryEmail, hash); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO app_user (id, firstname, lastname, email, password)
		VALUES ($1, 'Regular', 'Member', $2, $3)
	`, memberID, memberEmail, hash); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO access_assignments
		SELECT 'admin', $1, id FROM access_roles WHERE key = 'role-manager'
		ON CONFLICT DO NOTHING
	`, managerID); err != nil {
		t.Fatal(err)
	}

	const origin = "http://localhost:5173"
	handler := newHandler(pool, origin, nil)
	request := func(method, path, body string, cookie *http.Cookie, status int) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, "/api/v1"+path, strings.NewReader(body)).WithContext(ctx)
		req.Header.Set("Origin", origin)
		req.Header.Set("Content-Type", "application/json")
		if cookie != nil {
			req.AddCookie(cookie)
		}
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, req)
		if recorder.Code != status {
			t.Fatalf("%s %s: got HTTP %d, want %d. %s", method, path, recorder.Code, status, recorder.Body.String())
		}
		return recorder
	}
	login := func(path, email string) *http.Cookie {
		t.Helper()
		body, err := json.Marshal(map[string]string{"email": email, "password": password})
		if err != nil {
			t.Fatal(err)
		}
		response := request(http.MethodPost, path, string(body), nil, http.StatusOK)
		for _, cookie := range response.Result().Cookies() {
			if cookie.Name == "ACCESS_TOKEN" {
				return cookie
			}
		}
		t.Fatal("login did not issue an access cookie")
		return nil
	}
	manager := login("/login-admin", managerEmail)
	ordinary := login("/login-admin", ordinaryEmail)
	member := login("/login", memberEmail)

	const accessPath = "/admin/access"
	request(http.MethodGet, accessPath, "", manager, http.StatusOK)
	request(http.MethodGet, accessPath, "", ordinary, http.StatusForbidden)
	request(http.MethodGet, accessPath, "", member, http.StatusForbidden)
	request(http.MethodPost, "/register-admin", "{}", manager, http.StatusBadRequest)
	request(http.MethodPost, "/register-admin", "{}", ordinary, http.StatusForbidden)
	for _, session := range []struct {
		cookie     *http.Cookie
		canPromote bool
	}{{manager, true}, {ordinary, false}, {member, false}} {
		body := request(http.MethodGet, "/user", "", session.cookie, http.StatusOK).Body.String()
		if strings.Contains(body, `"admins.promote"`) != session.canPromote {
			t.Fatal("profile did not return the expected admin promotion permission")
		}
	}
	request(http.MethodGet, "/admin/forms", "", ordinary, http.StatusOK)
	request(http.MethodGet, "/admin/submissions", "", ordinary, http.StatusOK)
	request(http.MethodGet, "/admin/forms", "", member, http.StatusForbidden)

	const roles = accessPath + "/roles"
	request(http.MethodPost, roles, `{"key":"bad","name":"Bad","approvalEligible":true,"permissions":["roles.manage"]}`,
		manager, http.StatusBadRequest)
	request(http.MethodPost, roles, `{"key":"bad","name":"Bad","approvalEligible":true,"permissions":["submissions.review"]}`,
		manager, http.StatusBadRequest)
	roleKey := fmt.Sprintf("form-helper-%d", stamp)
	create := request(http.MethodPost, roles, fmt.Sprintf(`{"key":%q,"name":"Form helper","permissions":["forms.manage"]}`, roleKey),
		manager, http.StatusCreated)
	var created struct{ ID int64 }
	if err := json.Unmarshal(create.Body.Bytes(), &created); err != nil || created.ID <= 0 {
		t.Fatalf("parse created role: %+v (%v)", created, err)
	}
	roleID = created.ID

	accountPath := fmt.Sprintf("%s/accounts/user/%d/roles", accessPath, memberID)
	assignment := fmt.Sprintf("%s/%d", accountPath, roleID)
	request(http.MethodPut, assignment, "", manager, http.StatusNoContent)
	if body := request(http.MethodGet, accountPath, "", manager, http.StatusOK).Body.String(); !strings.Contains(body, "Form helper") {
		t.Fatal("account assignments omitted the new role")
	}
	request(http.MethodGet, "/admin/forms", "", member, http.StatusOK)
	request(http.MethodGet, "/admin/submissions", "", member, http.StatusForbidden)

	rolePath := fmt.Sprintf("%s/%d", roles, roleID)
	request(http.MethodPut, rolePath, `{"name":"Review helper","permissions":["submissions.review"]}`, manager, http.StatusOK)
	request(http.MethodGet, "/admin/forms", "", member, http.StatusForbidden)
	request(http.MethodGet, "/admin/submissions", "", member, http.StatusOK)
	request(http.MethodDelete, assignment, "", manager, http.StatusNoContent)
	request(http.MethodGet, "/admin/submissions", "", member, http.StatusForbidden)
	request(http.MethodGet, "/admin/forms", "", ordinary, http.StatusOK)

	if body := request(http.MethodGet, accessPath+"/audit", "", manager, http.StatusOK).Body.String(); !strings.Contains(body, "role.revoked") ||
		!strings.Contains(body, "role.edited") || !strings.Contains(body, fmt.Sprintf(`"actorId":%d`, managerID)) {
		t.Fatalf("access audit omitted the changes or actor: %s", body)
	}
}
