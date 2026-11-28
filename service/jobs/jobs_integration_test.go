package jobs

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
	"github.com/jackc/pgx/v5/pgxpool"
)

type fixtureSessions struct {
	types.AuthService
	claims map[string]types.CustomClaims
}

func (s fixtureSessions) VerifyToken(cookie string) (*jwt.Token, error) {
	claims, ok := s.claims[cookie]
	return &jwt.Token{Valid: ok, Claims: &claims}, nil
}

func TestJobPublicationPostgres(t *testing.T) {
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
	t.Cleanup(pool.Close)

	fixture := fmt.Sprintf("jobs-%d", time.Now().UnixNano())
	sessions := fixtureSessions{claims: make(map[string]types.CustomClaims)}
	var jobs, roles []int64
	var formID, submissionID int64
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		run := func(query string, args ...any) {
			if _, err := pool.Exec(cleanup, query, args...); err != nil {
				t.Errorf("clean job fixture: %v", err)
			}
		}
		run("DELETE FROM access_audit WHERE action='job.published' AND (detail->>'jobId')::bigint=ANY($1::bigint[])", jobs)
		run("DELETE FROM job_opportunities WHERE id=ANY($1::bigint[])", jobs)
		run("DELETE FROM form_verifications WHERE submission_id=$1", submissionID)
		run("DELETE FROM form_submissions WHERE id=$1", submissionID)
		run("DELETE FROM form_versions WHERE form_id=$1", formID)
		run("DELETE FROM forms WHERE id=$1", formID)
		run("DELETE FROM app_user WHERE email LIKE $1", fixture+"-%@example.invalid")
		run("DELETE FROM access_roles WHERE id=ANY($1::bigint[])", roles)
	})
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	account := func(name string, verified, admin bool) int {
		t.Helper()
		var id int
		err := pool.QueryRow(ctx, `INSERT INTO app_user (firstname, lastname, email, password, verified)
			VALUES ('Job', 'Fixture', $1, 'unused', $2) RETURNING id`,
			fixture+"-"+name+"@example.invalid", verified).Scan(&id)
		if err != nil {
			t.Fatal(err)
		}
		if admin {
			exec("INSERT INTO admin_memberships (user_id) VALUES ($1)", id)
		}
		sessions.claims[name] = types.CustomClaims{Id: id, Type: types.RoleUser, AdminMode: admin}
		return id
	}
	role := func(name string, eligible bool, permission string) int64 {
		t.Helper()
		var id int64
		if err := pool.QueryRow(ctx, `INSERT INTO access_roles (key,name,approval_eligible)
			VALUES ($1,$2,$3) RETURNING id`, fixture+"-"+name, name, eligible).Scan(&id); err != nil {
			t.Fatal(err)
		}
		roles = append(roles, id)
		exec("INSERT INTO access_role_permissions VALUES ($1,$2)", id, permission)
		return id
	}
	publisher := account("publisher", false, true)
	account("denied-admin", false, true)
	account("verified", true, false)
	roleOnly := account("role-only", false, false)
	overlap := account("overlap", true, false)
	approval := account("approval", false, false)
	outsider := account("outsider", false, false)
	publisherRole := role("publisher", false, "jobs.publish")
	firstRole := role("audience-one", true, "forms.browse")
	secondRole := role("audience-two", true, "forms.browse")
	exec("INSERT INTO admin_membership_roles VALUES ($1,$2)", publisher, publisherRole)
	exec("INSERT INTO access_assignments VALUES ('user',$1,$2)", outsider, publisherRole)
	exec("INSERT INTO access_assignments VALUES ('user',$1,$3), ('user',$2,$3), ('user',$2,$4)",
		roleOnly, overlap, firstRole, secondRole)
	if err := pool.QueryRow(ctx, "INSERT INTO forms (draft) VALUES ('{}') RETURNING id").Scan(&formID); err != nil {
		t.Fatal(err)
	}
	exec("INSERT INTO form_versions (form_id,version,definition) VALUES ($1,1,'{}')", formID)
	if err := pool.QueryRow(ctx, `INSERT INTO form_submissions (form_id,user_id,form_version,answers,status)
		VALUES ($1,$2,1,'{}','approved') RETURNING id`, formID, approval).Scan(&submissionID); err != nil {
		t.Fatal(err)
	}
	exec("INSERT INTO form_verifications (submission_id, access_role_id) VALUES ($1,$2)", submissionID, firstRole)

	mux := http.NewServeMux()
	RegisterRoutes(mux, pool, sessions, "https://example.invalid")
	request := func(method, path, who, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body)).WithContext(ctx)
		r.Header.Set("Content-Type", "application/json")
		if who != "" {
			r.AddCookie(&http.Cookie{Name: "ACCESS_TOKEN", Value: who})
		}
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, r)
		return response
	}
	check := func(response *httptest.ResponseRecorder, status int) {
		t.Helper()
		if response.Code != status {
			t.Fatalf("got HTTP %d, want %d: %s", response.Code, status, response.Body)
		}
	}
	readJob := func(response *httptest.ResponseRecorder, status int) adminOpportunity {
		t.Helper()
		check(response, status)
		var job adminOpportunity
		if err := json.Unmarshal(response.Body.Bytes(), &job); err != nil {
			t.Fatal(err)
		}
		return job
	}
	newJob := func(body string) adminOpportunity {
		t.Helper()
		response := request(http.MethodPost, adminPath, "publisher", body)
		job := readJob(response, http.StatusCreated)
		jobs = append(jobs, job.ID)
		if response.Header().Get("Location") != fmt.Sprintf("%s/%d", adminPath, job.ID) {
			t.Fatal("unexpected job creation location")
		}
		return job
	}
	check(request(http.MethodPost, adminPath, "", "{}"), http.StatusUnauthorized)
	check(request(http.MethodPost, adminPath, "denied-admin", "{}"), http.StatusForbidden)
	check(request(http.MethodPost, adminPath, "outsider", "{}"), http.StatusForbidden)
	check(request(http.MethodGet, adminPath+"/audience-roles", "publisher", ""), http.StatusOK)

	const details = `{"company":"Example Company","title":"Summer internship","description":"Work with the team.","deadline":"2030-08-01T12:00:00Z","applicationInstructions":"Contact placement office"}`
	job := newJob("{}")
	adminDetail := fmt.Sprintf("%s/%d", adminPath, job.ID)
	userDetail := fmt.Sprintf("/api/v1/jobs/%d", job.ID)
	publication := adminDetail + "/publication"
	combined := fmt.Sprintf(`{"revision":2,"audience":{"verifiedUsers":true,"roleIds":[%d,%d,%d]}}`, secondRole, firstRole, firstRole)
	check(request(http.MethodPut, publication, "denied-admin", combined), http.StatusForbidden)
	check(request(http.MethodPut, publication, "publisher", `{"revision":1,"audience":{"verifiedUsers":true}}`), http.StatusBadRequest)
	check(request(http.MethodGet, userDetail, "verified", ""), http.StatusNotFound)
	updated := readJob(request(http.MethodPut, adminDetail+"/draft", "publisher", `{"revision":1,`+details[1:]), http.StatusOK)
	if updated.Revision != 2 || updated.Deadline == nil {
		t.Fatalf("updated draft missing fields: %+v", updated)
	}
	check(request(http.MethodPut, publication, "publisher", `{"revision":1,"audience":{"verifiedUsers":true}}`), http.StatusConflict)
	check(request(http.MethodPut, publication, "publisher", `{"revision":2,"audience":{"roleIds":[9223372036854775807]}}`), http.StatusBadRequest)

	results := make(chan *httptest.ResponseRecorder, 2)
	for i := 0; i < cap(results); i++ {
		go func() { results <- request(http.MethodPut, publication, "publisher", combined) }()
	}
	first := readJob(<-results, http.StatusOK)
	second := readJob(<-results, http.StatusOK)
	if first.PublishedAt == nil || second.PublishedAt == nil || !first.PublishedAt.Equal(*second.PublishedAt) ||
		first.PublishedBy == nil || first.PublishedBy.AccountID != publisher ||
		first.Audience == nil || len(first.Audience.RoleIDs) != 2 || first.RecipientCount != 4 {
		t.Fatalf("unexpected publication receipt: %+v", first)
	}
	check(request(http.MethodPut, publication, "publisher", `{"revision":2,"audience":{"verifiedUsers":true}}`), http.StatusConflict)
	check(request(http.MethodPut, adminDetail+"/draft", "publisher", `{"revision":2}`), http.StatusConflict)
	for _, who := range []string{"verified", "role-only", "overlap", "approval"} {
		result := request(http.MethodGet, userDetail, who, "")
		check(result, http.StatusOK)
		if strings.Contains(result.Body.String(), "audience") || strings.Contains(result.Body.String(), "publishedBy") {
			t.Fatal("private publication fields leaked to recipient")
		}
		feed := request(http.MethodGet, "/api/v1/jobs", who, "")
		check(feed, http.StatusOK)
		if !strings.Contains(feed.Body.String(), "Summer internship") {
			t.Fatalf("feed did not include delivered job for %s", who)
		}
	}
	check(request(http.MethodGet, userDetail, "outsider", ""), http.StatusNotFound)
	outsideFeed := request(http.MethodGet, "/api/v1/jobs", "outsider", "")
	check(outsideFeed, http.StatusOK)
	if strings.TrimSpace(outsideFeed.Body.String()) != `{"items":[]}` {
		t.Fatalf("unexpected feed for an excluded account: %s", outsideFeed.Body)
	}
	exec("INSERT INTO access_assignments VALUES ('user',$1,$2)", outsider, firstRole)
	check(request(http.MethodGet, userDetail, "outsider", ""), http.StatusNotFound)
	var eventCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM access_audit
		WHERE action='job.published' AND detail->>'jobId'=$1`, fmt.Sprint(job.ID)).Scan(&eventCount); err != nil || eventCount != 1 {
		t.Fatalf("expected one publication audit event, got %d: %v", eventCount, err)
	}
	for _, tc := range []struct {
		audienceJSON      string
		allowed, excluded string
	}{
		{fmt.Sprintf(`{"roleIds":[%d]}`, firstRole), "role-only", "verified"},
		{`{"verifiedUsers":true}`, "verified", "role-only"},
	} {
		post := newJob(details)
		response := request(http.MethodPut, fmt.Sprintf("%s/%d/publication", adminPath, post.ID), "publisher",
			`{"revision":1,"audience":`+tc.audienceJSON+`}`)
		check(response, http.StatusOK)
		detail := fmt.Sprintf("/api/v1/jobs/%d", post.ID)
		check(request(http.MethodGet, detail, tc.allowed, ""), http.StatusOK)
		check(request(http.MethodGet, detail, tc.excluded, ""), http.StatusNotFound)
	}
}
