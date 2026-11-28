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
	var form Form
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
	mux := http.NewServeMux()
	admin := testReviewer(t, ctx, pool)
	defer deleteTestReviewer(t, pool, admin)
	RegisterRoutes(mux, pool, submissionTestAuth{owner: owner, other: other, admin: admin}, "http://localhost:5173")
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
	check := func(response *httptest.ResponseRecorder, status int, contains string) {
		t.Helper()
		if response.Code != status || !strings.Contains(response.Body.String(), contains) {
			t.Fatalf("expected %d / %q, got %d / %s", status, contains, response.Code, response.Body.String())
		}
	}

	readForm := func(response *httptest.ResponseRecorder) Form {
		t.Helper()
		var value Form
		if err := json.Unmarshal(response.Body.Bytes(), &value); err != nil || value.ID <= 0 {
			t.Fatalf("invalid form: %s (%v)", response.Body, err)
		}
		return value
	}
	save := func(path string, revision int64, status int) {
		t.Helper()
		body, err := json.Marshal(map[string]any{"revision": revision, "draft": definition})
		if err != nil {
			t.Fatal(err)
		}
		check(requestAt(path+"/draft", http.MethodPut, "admin", string(body)), status, "")
	}
	const adminForms = "/api/v1/admin/forms"
	created := requestAt(adminForms, http.MethodPost, "admin", "{}")
	check(created, http.StatusCreated, "Untitled form")
	form = readForm(created)
	formPath := fmt.Sprintf("%s/%d", adminForms, form.ID)
	if got := created.Header().Get("Location"); got != formPath {
		t.Fatalf("unexpected form location: %q", got)
	}
	check(requestAt(formPath, http.MethodGet, "admin", ""), http.StatusOK, "Untitled form")
	save(formPath, form.Revision, http.StatusOK)
	save(formPath, form.Revision, http.StatusConflict)
	draft := requestAt(formPath+"/draft", http.MethodGet, "admin", "")
	check(draft, http.StatusOK, "Submission test")
	publish := func(revision int64) Form {
		t.Helper()
		version := requestAt(formPath+"/versions", http.MethodPost, "admin", fmt.Sprintf(`{"revision":%d}`, revision))
		check(version, http.StatusCreated, "Submission test")
		published := readForm(version)
		location := fmt.Sprintf("%s/versions/%d", formPath, published.PublishedVersion)
		if version.Header().Get("Location") != location {
			t.Fatalf("unexpected version location: %q", version.Header().Get("Location"))
		}
		check(requestAt(location, http.MethodGet, "admin", ""), http.StatusOK, "Submission test")
		check(requestAt(location, http.MethodGet, "owner", ""), http.StatusForbidden, "forbidden")
		return published
	}
	form = publish(form.Revision + 1)
	check(requestAt(formPath+"/draft", http.MethodGet, "admin", ""), http.StatusNotFound, "not_found")

	const formsPath = "/api/v1/forms"
	submissions := fmt.Sprintf("%s/%d/submissions", formsPath, form.ID)
	receipt := submissions + "/mine"
	request := func(method, cookie, body string) *httptest.ResponseRecorder {
		path := receipt
		if method == http.MethodPost {
			path = submissions
		}
		return requestAt(path, method, cookie, body)
	}
	check(requestAt(formsPath, http.MethodGet, "owner", ""), http.StatusOK, `"submissionStatus":"not_submitted"`)
	if body := requestAt(formsPath, http.MethodGet, "admin", "").Body.String(); strings.Contains(body, "submissionStatus") {
		t.Fatalf("admin browsing received a personal status: %s", body)
	}
	valid := `{"version":1,"values":{"code":"ABC","unknown":"discard"}}`
	check(request(http.MethodPost, "", valid), http.StatusUnauthorized, "")
	check(request(http.MethodPost, "admin", valid), http.StatusForbidden, "")
	check(request(http.MethodPost, "owner", `{"version":1,"values":{"code":"invalid"}}`), http.StatusUnprocessableEntity, `"code":"invalid_answers"`)
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
	if got := success.Header().Get("Location"); got != receipt {
		t.Fatalf("unexpected submission location: %q", got)
	}
	check(requestAt(strings.TrimSuffix(submissions, "s"), http.MethodGet, "owner", ""), http.StatusOK, "ABC")
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
	check(requestAt(queuePath, http.MethodGet, "owner", ""), http.StatusForbidden, "")
	check(requestAt(detailPath, http.MethodGet, "other", ""), http.StatusForbidden, "")
	check(requestAt(detailPath, http.MethodPatch, "owner", `{"revision":1,"decision":"approved"}`), http.StatusForbidden, "")
	check(requestAt(queuePath, http.MethodGet, "admin", ""), http.StatusOK, "Submission test")
	check(requestAt(detailPath, http.MethodGet, "admin", ""), http.StatusOK, "ABC")
	check(requestAt(detailPath, http.MethodPatch, "admin", `{"revision":1,"decision":"changes_requested","comment":[{"fieldId":"unknown"},{"text":" try again"}]}`), http.StatusBadRequest, "submitted version")
	check(requestAt(detailPath, http.MethodPatch, "admin", `{"revision":1,"decision":"changes_requested","comment":[{"text":"Use a different code for "},{"fieldId":"code"}]}`), http.StatusOK, "changes_requested")
	check(requestAt(detailPath, http.MethodPatch, "admin", `{"revision":1,"decision":"approved"}`), http.StatusConflict, "reload")
	check(request(http.MethodGet, "owner", ""), http.StatusOK, `"fieldId":"code"`)
	check(request(http.MethodGet, "owner", ""), http.StatusOK, "Use a different code")
	check(requestAt(formsPath, http.MethodGet, "owner", ""), http.StatusOK, `"submissionStatus":"changes_requested"`)
	copyDraft := requestAt(formPath+"/drafts", http.MethodPost, "admin", fmt.Sprintf(`{"revision":%d}`, form.Revision))
	check(copyDraft, http.StatusCreated, "Submission test")
	if got := copyDraft.Header().Get("Location"); got != formPath+"/draft" {
		t.Fatalf("unexpected draft location: %q", got)
	}
	check(requestAt(formPath+"/draft", http.MethodGet, "admin", ""), http.StatusOK, "Submission test")
	definition.Fields[0].Rules = &FieldRules{Pattern: "^[0-9]{3}$"}
	draftRevision := readForm(copyDraft).Revision
	save(formPath, draftRevision, http.StatusOK)
	publish(draftRevision + 1)
	check(requestAt(formPath+"/versions/1", http.MethodGet, "admin", ""), http.StatusOK, "[A-Z]")

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
	check(requestAt(detailPath, http.MethodPatch, "admin", `{"revision":3,"decision":"approved"}`), http.StatusOK, `"status":"approved"`)
	check(request(http.MethodGet, "owner", ""), http.StatusOK, `"status":"approved"`)
	check(requestAt(formsPath, http.MethodGet, "owner", ""), http.StatusOK, `"submissionStatus":"approved"`)
	check(request(http.MethodPut, "owner", `{"version":1,"revision":4,"values":{"code":"XYZ"}}`), http.StatusConflict, "submission_changed")
}
