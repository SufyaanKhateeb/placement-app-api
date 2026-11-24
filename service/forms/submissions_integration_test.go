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
}

func (a submissionTestAuth) VerifyToken(cookie string) (*jwt.Token, error) {
	id, role := a.owner, types.RoleUser
	switch cookie {
	case "other":
		id = a.other
	case "admin":
		role = types.RoleAdmin
	}
	return &jwt.Token{Valid: true, Claims: &types.CustomClaims{Id: id, Type: role}}, nil
}

func TestSubmissionFlowPostgres(t *testing.T) {
	database := os.Getenv("PLACEMENT_TEST_DATABASE_URL")
	if database == "" {
		t.Skip("requires a migrated test database in PLACEMENT_TEST_DATABASE_URL")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
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
	RegisterRoutes(mux, pool, submissionTestAuth{owner: owner, other: other}, "http://localhost:5173")
	path := fmt.Sprintf("/api/v1/forms/%d/submission", form.ID)
	request := func(method, cookie, body string) *httptest.ResponseRecorder {
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

	valid := `{"version":1,"values":{"code":"ABC","unknown":"discard"}}`
	check(request(http.MethodPost, "", valid), http.StatusFound, "")
	check(request(http.MethodPost, "admin", valid), http.StatusFound, "")
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

	if _, err := pool.Exec(ctx, "UPDATE form_submissions SET status = 'changes_requested' WHERE id = $1", original.ID); err != nil {
		t.Fatal(err)
	}
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
	result := request(http.MethodPut, "owner", `{"version":1,"revision":1,"values":{"code":"XYZ"}}`)
	check(result, http.StatusOK, `"status":"submitted"`)
	var updated submission
	if err := json.Unmarshal(result.Body.Bytes(), &updated); err != nil {
		t.Fatal(err)
	}
	if updated.ID != original.ID || updated.Version != original.Version || updated.Revision != 2 {
		t.Fatalf("resubmission did not retain the original record and version: %+v", updated)
	}
	check(request(http.MethodPut, "owner", `{"version":1,"revision":1,"values":{"code":"XYZ"}}`), http.StatusConflict, "submission_changed")
}
