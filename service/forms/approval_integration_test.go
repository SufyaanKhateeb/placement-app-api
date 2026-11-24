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

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestApprovalGrantsPostgres(t *testing.T) {
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
	var users []int
	var forms []int64
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		for _, id := range forms {
			if _, err := pool.Exec(cleanup, `
                DELETE FROM form_verifications WHERE submission_id IN (SELECT id FROM form_submissions WHERE form_id = $1) OR follow_up_form_id = $1;
                DELETE FROM form_submissions WHERE form_id = $1;
                UPDATE forms SET published_version = NULL, draft = '{}'::jsonb WHERE id = $1;
                DELETE FROM form_versions WHERE form_id = $1;
                DELETE FROM forms WHERE id = $1;
            `, pgx.QueryExecModeSimpleProtocol, id); err != nil {
				t.Errorf("cleanup forms: %v", err)
			}
		}
		for _, id := range users {
			if _, err := pool.Exec(cleanup, "DELETE FROM app_user WHERE id = $1", id); err != nil {
				t.Errorf("cleanup users: %v", err)
			}
		}
	}()
	for i := 0; i < 2; i++ {
		var id int
		email := fmt.Sprintf("approval-test-%d-%d@example.invalid", time.Now().UnixNano(), i)
		if err := pool.QueryRow(ctx, `
            INSERT INTO app_user (firstname, lastname, email, password) VALUES ('Test', 'User', $1, 'unused') RETURNING id
        `, email).Scan(&id); err != nil {
			t.Fatal(err)
		}
		users = append(users, id)
	}
	s := store{db: pool}
	def := Definition{Title: "Approval access test", Fields: []Field{{ID: "name", Label: "Name", Type: "text", Required: true}}}
	create := func(definition Definition) Form {
		t.Helper()
		form, err := s.create(ctx)
		if err != nil {
			t.Fatal(err)
		}
		forms = append(forms, form.ID)
		revision, err := s.save(ctx, form.ID, form.Revision, definition)
		if err != nil {
			t.Fatal(err)
		}
		result, err := s.publish(ctx, form.ID, revision)
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	followUp := create(Definition{Title: "Restricted follow-up test", Fields: def.Fields, RequiresApprovalGrant: true})
	def.ApprovalAction = &ApprovalAction{Kind: "follow_up_form", FormID: followUp.ID}
	source := create(def)

	mux := http.NewServeMux()
	RegisterRoutes(mux, pool, submissionTestAuth{owner: users[0], other: users[1]}, "http://localhost:5173")
	request := func(method, path, cookie, body string, status int) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(body)).WithContext(ctx)
		req.AddCookie(&http.Cookie{Name: "ACCESS_TOKEN", Value: cookie})
		req.Header.Set("Content-Type", "application/json")
		result := httptest.NewRecorder()
		mux.ServeHTTP(result, req)
		if result.Code != status {
			t.Fatalf("%s %s: got %d, want %d. %s", method, path, result.Code, status, result.Body.String())
		}
		return result
	}
	targetPath := fmt.Sprintf("/api/v1/forms/%d", followUp.ID)
	sourcePath := fmt.Sprintf("/api/v1/forms/%d/submission", source.ID)
	const initial = `{"version":1,"values":{"name":"Test"}}`
	request(http.MethodGet, targetPath, "owner", "", http.StatusNotFound)
	request(http.MethodPost, targetPath+"/submission", "owner", initial, http.StatusNotFound)
	if list := request(http.MethodGet, "/api/v1/forms", "owner", "", http.StatusOK).Body.String(); strings.Contains(list, "Restricted follow-up test") {
		t.Fatal("locked form appeared in owner's list before approval")
	}
	beforeEdit := request(http.MethodPost, sourcePath, "owner", initial, http.StatusCreated)

	draft, err := s.editPublished(ctx, source.ID, source.Revision)
	if err != nil {
		t.Fatal(err)
	}
	def.ApprovalAction = &ApprovalAction{Kind: "assign_role", Role: "verified-member"}
	revision, err := s.save(ctx, source.ID, draft.Revision, def)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.publish(ctx, source.ID, revision); err != nil {
		t.Fatal(err)
	}
	afterEdit := request(http.MethodPost, sourcePath, "other", `{"version":2,"values":{"name":"Test"}}`, http.StatusCreated)

	approve := func(response *httptest.ResponseRecorder) int64 {
		t.Helper()
		var result submission
		if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		path := fmt.Sprintf("/api/v1/admin/submissions/%d/review", result.ID)
		const input = `{"revision":1,"decision":"approved"}`
		request(http.MethodPut, path, "admin", input, http.StatusOK)
		request(http.MethodPut, path, "admin", input, http.StatusConflict)
		return result.ID
	}
	beforeID, afterID := approve(beforeEdit), approve(afterEdit)
	request(http.MethodGet, targetPath, "owner", "", http.StatusOK)
	request(http.MethodGet, targetPath, "other", "", http.StatusNotFound)
	request(http.MethodPost, targetPath+"/submission", "owner", initial, http.StatusCreated)
	if list := request(http.MethodGet, "/api/v1/forms", "owner", "", http.StatusOK).Body.String(); !strings.Contains(list, "Restricted follow-up test") {
		t.Fatal("approved follow-up did not appear in the owner's list")
	}
	var followUpID int64
	if err := pool.QueryRow(ctx, "SELECT follow_up_form_id FROM form_verifications WHERE submission_id=$1", beforeID).Scan(&followUpID); err != nil || followUpID != followUp.ID {
		t.Fatalf("missing pinned follow-up grant: %d (%v)", followUpID, err)
	}
	var role string
	if err := pool.QueryRow(ctx, "SELECT role_key FROM form_verifications WHERE submission_id=$1", afterID).Scan(&role); err != nil || role != "verified-member" {
		t.Fatalf("missing assigned role: %q (%v)", role, err)
	}
	request(http.MethodGet, "/api/v1/admin/forms", "other", "", http.StatusFound)
}

func TestApprovalActionRejectsBuiltInRole(t *testing.T) {
	if err := (&ApprovalAction{Kind: "assign_role", Role: " Admin "}).validate(); err == nil {
		t.Fatal("the built-in admin role should not be assigned by a form")
	}
}
