package forms

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/SufyaanKhateeb/college-placement-app-api/types"
	"github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type submissionTestAuth struct {
	types.AuthService
	owner int
	other int
	admin int
}

func (a submissionTestAuth) VerifyToken(cookie string) (*jwt.Token, error) {
	id, role := a.owner, types.RoleUser
	switch cookie {
	case "other":
		id = a.other
	case "admin":
		id, role = a.admin, types.RoleAdmin
	}
	return &jwt.Token{Valid: true, Claims: &types.CustomClaims{Id: id, Type: role}}, nil
}

func testReviewer(t *testing.T, ctx context.Context, pool *pgxpool.Pool) int {
	t.Helper()
	var id int
	email := fmt.Sprintf("review-test-%d@example.invalid", time.Now().UnixNano())
	if err := pool.QueryRow(ctx, `
		INSERT INTO admin_user (firstname, lastname, email, password) VALUES ('Test', 'Reviewer', $1, 'unused') RETURNING id
	`, email).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func deleteTestReviewer(t *testing.T, pool *pgxpool.Pool, id int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := pool.Exec(ctx, "DELETE FROM admin_user WHERE id = $1", id); err != nil {
		t.Errorf("clean reviewer: %v", err)
	}
}

func TestSubmissionFlowPostgres(t *testing.T) {
	database := os.Getenv("PLACEMENT_TEST_DATABASE_URL")
	if database == "" {
		t.Skip("requires a migrated test database in PLACEMENT_TEST_DATABASE_URL")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, database)
	if err != nil {
		t.Fatal("could not configure test database")
	}
	defer pool.Close()

	definition := Definition{Title: "Submission test", Fields: []Field{
		{ID: "code", Label: "Code", Type: "text", Required: true, Rules: &FieldRules{Pattern: "^[A-Z]{3}$"}},
	}}
	var owner, other int
	for index, id := range []*int{&owner, &other} {
		email := fmt.Sprintf("submission-test-%d-%d@example.invalid", time.Now().UnixNano(), index)
		err := pool.QueryRow(ctx, `
            INSERT INTO app_user (firstname, lastname, email, password) VALUES ('Test', 'User', $1, 'unused') RETURNING id
        `, email).Scan(id)
		if err != nil {
			t.Fatal(err)
		}
	}
	s := store{db: pool}
	form, err := s.create(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := pool.Exec(cleanup, `
            DELETE FROM form_verifications WHERE submission_id IN (SELECT id FROM form_submissions WHERE form_id = $1);
            DELETE FROM form_submissions WHERE form_id = $1;
            UPDATE forms SET published_version = NULL, draft = '{}'::jsonb WHERE id = $1;
            DELETE FROM form_versions WHERE form_id = $1;
            DELETE FROM forms WHERE id = $1;
        `, pgx.QueryExecModeSimpleProtocol, form.ID); err != nil {
			t.Errorf("clean submitted form: %v", err)
		}
		if _, err := pool.Exec(cleanup, "DELETE FROM app_user WHERE id IN ($1, $2)", owner, other); err != nil {
			t.Errorf("clean test accounts: %v", err)
		}
	}()
	revision, err := s.save(ctx, form.ID, form.Revision, definition)
	if err != nil {
		t.Fatal(err)
	}
	form, err = s.publish(ctx, form.ID, revision)
	if err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	admin := testReviewer(t, ctx, pool)
	defer deleteTestReviewer(t, pool, admin)
	RegisterRoutes(mux, pool, submissionTestAuth{owner: owner, other: other, admin: admin}, "http://localhost:5173")
	path := fmt.Sprintf("/api/v1/forms/%d/submission", form.ID)
	requestAt := func(path, method, cookie, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body)).WithContext(ctx)
		if cookie != "" {
			req.AddCookie(&http.Cookie{Name: "ACCESS_TOKEN", Value: cookie})
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", "http://localhost:5173")
		result := httptest.NewRecorder()
		mux.ServeHTTP(result, req)
		return result
	}
	request := func(method, cookie, body string) *httptest.ResponseRecorder {
		return requestAt(path, method, cookie, body)
	}
	check := func(response *httptest.ResponseRecorder, status int, contains string) {
		t.Helper()
		if response.Code != status || !strings.Contains(response.Body.String(), contains) {
			t.Fatalf("expected %d / %q, got %d / %s", status, contains, response.Code, response.Body.String())
		}
	}

	const formsPath = "/api/v1/forms"
	check(requestAt(formsPath, http.MethodGet, "owner", ""), http.StatusOK, `"submissionStatus":"not_submitted"`)
	if body := requestAt(formsPath, http.MethodGet, "admin", "").Body.String(); strings.Contains(body, "submissionStatus") {
		t.Fatalf("admin browsing received a personal status: %s", body)
	}
	valid := `{"version":1,"values":{"code":"ABC","unknown":"discard"}}`
	check(request(http.MethodPost, "", valid), http.StatusUnauthorized, "")
	check(request(http.MethodPost, "admin", valid), http.StatusForbidden, "")
	check(request(http.MethodPost, "owner", `{"version":1,"values":{"code":"invalid"}}`), http.StatusUnprocessableEntity, `"code":`)
	check(request(http.MethodPost, "owner", `{"version":1,"values":{}}`), http.StatusUnprocessableEntity, "This field is required")

	start := make(chan struct{})
	attempts := make(chan *httptest.ResponseRecorder, 2)
	for i := 0; i < 2; i++ {
		go func() {
			<-start
			attempts <- request(http.MethodPost, "owner", valid)
		}()
	}
	close(start)
	success, duplicate := <-attempts, <-attempts
	if success.Code != http.StatusCreated {
		success, duplicate = duplicate, success
	}
	check(success, http.StatusCreated, `"status":"submitted"`)
	check(duplicate, http.StatusConflict, "already_submitted")
	check(requestAt(formsPath, http.MethodGet, "owner", ""), http.StatusOK, `"submissionStatus":"submitted"`)
	check(requestAt(formsPath, http.MethodGet, "other", ""), http.StatusOK, `"submissionStatus":"not_submitted"`)
	var original submission
	if err := json.Unmarshal(success.Body.Bytes(), &original); err != nil {
		t.Fatal(err)
	}
	if _, ok := original.Values["unknown"]; ok {
		t.Fatal("unknown answer was saved")
	}
	check(request(http.MethodGet, "other", ""), http.StatusOK, "null")
	check(request(http.MethodGet, "owner", ""), http.StatusOK, "ABC")
	check(request(http.MethodPut, "owner", `{"version":1,"revision":1,"values":{"code":"XYZ"}}`), http.StatusConflict, "submission_changed")

	const queuePath = "/api/v1/admin/submissions"
	detailPath := fmt.Sprintf("%s/%d", queuePath, original.ID)
	decisionPath := detailPath + "/review"
	check(requestAt(queuePath, http.MethodGet, "owner", ""), http.StatusForbidden, "")
	check(requestAt(detailPath, http.MethodGet, "other", ""), http.StatusForbidden, "")
	check(requestAt(decisionPath, http.MethodPut, "owner", `{"revision":1,"decision":"approved"}`), http.StatusForbidden, "")
	check(requestAt(queuePath, http.MethodGet, "admin", ""), http.StatusOK, "Submission test")
	check(requestAt(detailPath, http.MethodGet, "admin", ""), http.StatusOK, "ABC")
	check(requestAt(decisionPath, http.MethodPut, "admin", `{"revision":1,"decision":"changes_requested","comment":[{"fieldId":"unknown"},{"text":" try again"}]}`), http.StatusBadRequest, "submitted version")
	check(requestAt(decisionPath, http.MethodPut, "admin", `{"revision":1,"decision":"changes_requested","comment":[{"text":"Use a different code for "},{"fieldId":"code"}]}`), http.StatusOK, "changes_requested")
	check(requestAt(decisionPath, http.MethodPut, "admin", `{"revision":1,"decision":"approved"}`), http.StatusConflict, "reload")
	check(request(http.MethodGet, "owner", ""), http.StatusOK, `"fieldId":"code"`)
	check(request(http.MethodGet, "owner", ""), http.StatusOK, "Use a different code")
	check(requestAt(formsPath, http.MethodGet, "owner", ""), http.StatusOK, `"submissionStatus":"changes_requested"`)
	draft, err := s.editPublished(ctx, form.ID, form.Revision)
	if err != nil {
		t.Fatal(err)
	}
	definition.Fields[0].Rules = &FieldRules{Pattern: "^[0-9]{3}$"}
	revision, err = s.save(ctx, form.ID, draft.Revision, definition)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.publish(ctx, form.ID, revision); err != nil {
		t.Fatal(err)
	}

	check(request(http.MethodPost, "other", valid), http.StatusConflict, "version_changed")
	check(request(http.MethodPost, "owner", `{"version":2,"values":{"code":"123"}}`), http.StatusConflict, "already_submitted")
	result := request(http.MethodPut, "owner", `{"version":1,"revision":2,"values":{"code":"XYZ"}}`)
	check(result, http.StatusOK, `"status":"submitted"`)
	var updated submission
	if err := json.Unmarshal(result.Body.Bytes(), &updated); err != nil {
		t.Fatal(err)
	}
	if updated.ID != original.ID || updated.Version != original.Version || updated.Revision != 3 {
		t.Fatalf("resubmission did not retain the original record and version: %+v", updated)
	}
	check(request(http.MethodPut, "owner", `{"version":1,"revision":2,"values":{"code":"XYZ"}}`), http.StatusConflict, "submission_changed")
	check(requestAt(decisionPath, http.MethodPut, "admin", `{"revision":3,"decision":"approved"}`), http.StatusOK, `"status":"approved"`)
	check(request(http.MethodGet, "owner", ""), http.StatusOK, `"status":"approved"`)
	check(requestAt(formsPath, http.MethodGet, "owner", ""), http.StatusOK, `"submissionStatus":"approved"`)
	check(request(http.MethodPut, "owner", `{"version":1,"revision":4,"values":{"code":"XYZ"}}`), http.StatusConflict, "submission_changed")
}
