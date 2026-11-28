package jobs

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"strconv"

	"github.com/SufyaanKhateeb/college-placement-app-api/middlewares"
	"github.com/SufyaanKhateeb/college-placement-app-api/service/access"
	"github.com/SufyaanKhateeb/college-placement-app-api/types"
	"github.com/SufyaanKhateeb/college-placement-app-api/utils"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const adminPath = "/api/v1/admin/jobs"

type handler struct {
	db     *pgxpool.Pool
	access access.Store
	origin string
}

func RegisterRoutes(mux *http.ServeMux, db *pgxpool.Pool, auth types.AuthService, origin string) {
	h := handler{db: db, access: access.Store{DB: db}, origin: origin}
	admin := middlewares.Authenticate(auth, h.access.Resolve, types.RoleAdmin, access.PublishJobs)
	user := middlewares.Authenticate(auth, h.access.Resolve, types.RoleUser)

	mux.Handle("GET "+adminPath, admin(h.adminList))
	mux.Handle("GET "+adminPath+"/{id}", admin(h.adminGet))
	mux.Handle("GET "+adminPath+"/audience-roles", admin(h.audienceRoles))
	mux.Handle("POST "+adminPath, admin(h.write(h.create)))
	mux.Handle("PUT "+adminPath+"/{id}/draft", admin(h.write(h.update)))
	mux.Handle("PUT "+adminPath+"/{id}/publication", admin(h.write(h.publish)))
	mux.Handle("GET /api/v1/jobs", user(h.feed))
	mux.Handle("GET /api/v1/jobs/{id}", user(h.delivered))
}

func (h handler) write(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if origin := r.Header.Get("Origin"); origin != "" && origin != h.origin {
			utils.WriteJsonError(w, http.StatusForbidden, errors.New("untrusted request origin"))
			return
		}
		next(w, r)
	}
}

func readInput(w http.ResponseWriter, r *http.Request, input any) bool {
	mediaType, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if mediaType != "application/json" {
		utils.WriteJsonError(w, http.StatusUnsupportedMediaType, errors.New("send a JSON request"))
		return false
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 128<<10))
	decoder.DisallowUnknownFields()
	if decoder.Decode(input) != nil || decoder.Decode(new(any)) != io.EOF {
		utils.WriteJsonError(w, http.StatusBadRequest, errors.New("invalid JSON request"))
		return false
	}
	return true
}

func readPositive(w http.ResponseWriter, value string, field string) (int64, bool) {
	number, err := strconv.ParseInt(value, 10, 64)
	if err != nil || number <= 0 {
		utils.WriteJsonError(w, http.StatusBadRequest, fmt.Errorf("invalid %s", field))
		return 0, false
	}
	return number, true
}

func readCursor(w http.ResponseWriter, r *http.Request) (int64, bool) {
	cursor := r.URL.Query().Get("before")
	if cursor == "" {
		return 0, true
	}
	return readPositive(w, cursor, "job cursor")
}

func respond(w http.ResponseWriter, status int, value any, err error) {
	switch {
	case err == nil:
		utils.WriteJson(w, status, value)
	case errors.Is(err, pgx.ErrNoRows):
		utils.WriteJsonError(w, http.StatusNotFound, errors.New("job opportunity not found"))
	case errors.Is(err, access.ErrDenied):
		utils.WriteJsonError(w, http.StatusForbidden, err)
	case errors.Is(err, errConflict):
		utils.WriteAPIError(w, http.StatusConflict, "job_changed", err, nil)
	case errors.Is(err, errInvalidAudience), errors.Is(err, errIncompleteDraft):
		utils.WriteJsonError(w, http.StatusBadRequest, err)
	default:
		var databaseError interface{ SQLState() string }
		if errors.As(err, &databaseError) {
			slog.Error("Job operation failed", "sqlstate", databaseError.SQLState())
		}
		utils.WriteJsonError(w, http.StatusInternalServerError, errors.New("could not complete the job operation"))
	}
}

func (h handler) create(w http.ResponseWriter, r *http.Request) {
	var input draft
	if !readInput(w, r, &input) {
		return
	}
	if err := input.normalize(); err != nil {
		utils.WriteJsonError(w, http.StatusBadRequest, err)
		return
	}
	var result adminOpportunity
	session, _ := middlewares.Session(r.Context())
	err := h.access.ChangeWithPermission(r.Context(), session, access.PublishJobs, func(tx pgx.Tx) error {
		var err error
		result, err = createDraft(r.Context(), tx, input)
		return err
	})
	if err == nil {
		w.Header().Set("Location", fmt.Sprintf("%s/%d", adminPath, result.ID))
	}
	respond(w, http.StatusCreated, result, err)
}

func (h handler) update(w http.ResponseWriter, r *http.Request) {
	id, ok := readPositive(w, r.PathValue("id"), "job ID")
	if !ok {
		return
	}
	var input struct {
		Revision int `json:"revision"`
		draft
	}
	if !readInput(w, r, &input) || !validRevision(w, input.Revision) {
		return
	}
	if err := input.draft.normalize(); err != nil {
		utils.WriteJsonError(w, http.StatusBadRequest, err)
		return
	}
	var result adminOpportunity
	session, _ := middlewares.Session(r.Context())
	err := h.access.ChangeWithPermission(r.Context(), session, access.PublishJobs, func(tx pgx.Tx) error {
		var err error
		result, err = updateDraft(r.Context(), tx, id, input.Revision, input.draft)
		return err
	})
	respond(w, http.StatusOK, result, err)
}

func (h handler) publish(w http.ResponseWriter, r *http.Request) {
	id, ok := readPositive(w, r.PathValue("id"), "job ID")
	if !ok {
		return
	}
	var input struct {
		Revision int      `json:"revision"`
		Audience audience `json:"audience"`
	}
	if !readInput(w, r, &input) || !validRevision(w, input.Revision) {
		return
	}
	if err := input.Audience.normalize(); err != nil {
		utils.WriteJsonError(w, http.StatusBadRequest, err)
		return
	}
	var result adminOpportunity
	session, _ := middlewares.Session(r.Context())
	err := h.access.ChangeWithPermission(r.Context(), session, access.PublishJobs, func(tx pgx.Tx) error {
		var err error
		result, err = publish(r.Context(), tx, id, input.Revision, input.Audience, session)
		return err
	})
	respond(w, http.StatusOK, result, err)
}

func validRevision(w http.ResponseWriter, revision int) bool {
	if revision > 0 {
		return true
	}
	utils.WriteJsonError(w, http.StatusBadRequest, errors.New("a draft revision is required"))
	return false
}

func (h handler) adminGet(w http.ResponseWriter, r *http.Request) {
	id, ok := readPositive(w, r.PathValue("id"), "job ID")
	if !ok {
		return
	}
	item, err := scanAdmin(h.db.QueryRow(r.Context(), "SELECT "+adminColumns+" FROM job_opportunities j WHERE j.id=$1", id))
	respond(w, http.StatusOK, item, err)
}

func (h handler) adminList(w http.ResponseWriter, r *http.Request) {
	cursor, ok := readCursor(w, r)
	if !ok {
		return
	}
	rows, err := h.db.Query(r.Context(), "SELECT "+adminColumns+` FROM job_opportunities j
		WHERE ($1::bigint=0 OR j.id<$1) ORDER BY j.id DESC LIMIT $2`, cursor, pageSize+1)
	var result page[adminOpportunity]
	if err == nil {
		result.Items, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (adminOpportunity, error) { return scanAdmin(row) })
		if len(result.Items) > pageSize {
			result.Items = result.Items[:pageSize]
			result.NextBefore = result.Items[pageSize-1].ID
		}
	}
	respond(w, http.StatusOK, result, err)
}

func (h handler) audienceRoles(w http.ResponseWriter, r *http.Request) {
	rows, err := h.db.Query(r.Context(), "SELECT id, key, name FROM access_roles ORDER BY name, id")
	var roles []access.RoleChoice
	if err == nil {
		roles, err = pgx.CollectRows(rows, pgx.RowToStructByPos[access.RoleChoice])
	}
	respond(w, http.StatusOK, roles, err)
}

const deliveredJobs = ` FROM job_deliveries d JOIN job_opportunities j ON j.id=d.job_id
	WHERE d.user_id=$1 AND j.published_at IS NOT NULL`

func (h handler) feed(w http.ResponseWriter, r *http.Request) {
	cursor, ok := readCursor(w, r)
	if !ok {
		return
	}
	session, _ := middlewares.Session(r.Context())
	rows, err := h.db.Query(r.Context(), "SELECT "+visibleColumns+deliveredJobs+
		" AND ($2::bigint=0 OR d.job_id<$2) ORDER BY d.job_id DESC LIMIT $3", session.Id, cursor, pageSize+1)
	var result page[opportunity]
	if err == nil {
		result.Items, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (opportunity, error) { return scanOpportunity(row) })
		if len(result.Items) > pageSize {
			result.Items = result.Items[:pageSize]
			result.NextBefore = result.Items[pageSize-1].ID
		}
	}
	respond(w, http.StatusOK, result, err)
}

func (h handler) delivered(w http.ResponseWriter, r *http.Request) {
	id, ok := readPositive(w, r.PathValue("id"), "job ID")
	if !ok {
		return
	}
	session, _ := middlewares.Session(r.Context())
	item, err := scanOpportunity(h.db.QueryRow(r.Context(), "SELECT "+visibleColumns+deliveredJobs+" AND d.job_id=$2", session.Id, id))
	respond(w, http.StatusOK, item, err)
}
