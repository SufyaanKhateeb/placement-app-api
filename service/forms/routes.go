package forms

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"strconv"

	"github.com/SufyaanKhateeb/college-placement-app-api/middlewares"
	"github.com/SufyaanKhateeb/college-placement-app-api/service/access"
	"github.com/SufyaanKhateeb/college-placement-app-api/types"
	"github.com/SufyaanKhateeb/college-placement-app-api/utils"
	"github.com/jackc/pgx/v5/pgxpool"
)

type handler struct {
	store  store
	origin string
}

func RegisterRoutes(mux *http.ServeMux, db *pgxpool.Pool, sessions types.AuthService, origin string) {
	h := handler{store: store{db: db}, origin: origin}
	accessStore := access.Store{DB: db}
	user := middlewares.Authenticate(sessions, accessStore.Resolve, "", access.BrowseForms)
	admin := middlewares.Authenticate(sessions, accessStore.Resolve, "", access.ManageForms)
	reviewer := middlewares.Authenticate(sessions, accessStore.Resolve, "", access.ReviewSubmissions)
	submitter := middlewares.Authenticate(sessions, accessStore.Resolve, types.RoleUser, access.OwnSubmissions)

	mux.Handle("GET /api/v1/forms/{id}/submission", submitter(h.getMySubmission))
	mux.Handle("POST /api/v1/forms/{id}/submission", submitter(h.fromFrontend(h.submitAnswers(false))))
	mux.Handle("PUT /api/v1/forms/{id}/submission", submitter(h.fromFrontend(h.submitAnswers(true))))

	mux.Handle("GET /api/v1/forms", user(h.list(false)))
	mux.Handle("GET /api/v1/forms/{id}", user(h.getPublished))
	mux.Handle("GET /api/v1/admin/forms", admin(h.list(true)))
	mux.Handle("GET /api/v1/admin/forms/{id}", admin(h.get))
	mux.Handle("POST /api/v1/admin/forms", admin(h.fromFrontend(h.create)))
	mux.Handle("PUT /api/v1/admin/forms/{id}/draft", admin(h.fromFrontend(h.save)))
	mux.Handle("POST /api/v1/admin/forms/{id}/draft", admin(h.fromFrontend(h.revisionAction(h.store.editPublished))))
	mux.Handle("POST /api/v1/admin/forms/{id}/publish", admin(h.fromFrontend(h.revisionAction(h.store.publish))))
	mux.Handle("POST /api/v1/admin/forms/preview", admin(h.fromFrontend(h.preview)))
	mux.Handle("GET /api/v1/admin/forms/approval-roles", admin(func(w http.ResponseWriter, r *http.Request) {
		roles, err := accessStore.ApprovalRoles(r.Context())
		writeResult(w, http.StatusOK, roles, err)
	}))
	mux.Handle("GET /api/v1/admin/submissions", reviewer(h.listReviews))
	mux.Handle("GET /api/v1/admin/submissions/{id}", reviewer(h.getReview))
	mux.Handle("PUT /api/v1/admin/submissions/{id}/review", reviewer(h.fromFrontend(h.reviewSubmission)))
}

func (h handler) fromFrontend(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if origin := r.Header.Get("Origin"); origin != "" && origin != h.origin {
			utils.WriteJsonError(w, http.StatusForbidden, errors.New("untrusted request origin"))
			return
		}
		next(w, r)
	}
}

func (h handler) list(admin bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		forms, err := h.store.list(ctx, admin, publishedAccessAccount(ctx), middlewares.HasPermission(ctx, access.BrowseRestricted))
		writeResult(w, http.StatusOK, forms, err)
	}
}

func (h handler) get(w http.ResponseWriter, r *http.Request) {
	id, ok := readID(w, r)
	if !ok {
		return
	}
	form, err := h.store.get(r.Context(), id)
	writeResult(w, http.StatusOK, form, err)
}

func (h handler) getPublished(w http.ResponseWriter, r *http.Request) {
	id, ok := readID(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	form, err := readPublished(ctx, h.store.db, id, publishedAccessAccount(ctx), middlewares.HasPermission(ctx, access.BrowseRestricted))
	writeResult(w, http.StatusOK, form, err)
}

func publishedAccessAccount(ctx context.Context) *int {
	session, _ := middlewares.Session(ctx)
	if session.Type == types.RoleUser {
		return &session.Id
	}
	return nil
}

func (h handler) create(w http.ResponseWriter, r *http.Request) {
	// Require JSON so a browser form on another site cannot create a draft.
	var input struct{}
	if !readJSON(w, r, &input) {
		return
	}
	form, err := h.store.create(r.Context())
	writeResult(w, http.StatusCreated, form, err)
}

func (h handler) save(w http.ResponseWriter, r *http.Request) {
	id, ok := readID(w, r)
	if !ok {
		return
	}
	var input struct {
		Revision int64      `json:"revision"`
		Draft    Definition `json:"draft"`
	}
	if !readJSON(w, r, &input) || !validRevision(w, input.Revision) {
		return
	}
	if err := input.Draft.validate(); err != nil {
		utils.WriteJsonError(w, http.StatusBadRequest, err)
		return
	}
	revision, err := h.store.save(r.Context(), id, input.Revision, input.Draft)
	writeResult(w, http.StatusOK, map[string]any{"revision": revision, "draft": input.Draft}, err)
}

func (h handler) revisionAction(action func(context.Context, int64, int64) (Form, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := readID(w, r)
		if !ok {
			return
		}
		var input struct {
			Revision int64 `json:"revision"`
		}
		if !readJSON(w, r, &input) || !validRevision(w, input.Revision) {
			return
		}
		form, err := action(r.Context(), id, input.Revision)
		writeResult(w, http.StatusOK, form, err)
	}
}

func (h handler) preview(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Draft  Definition                 `json:"draft"`
		Values map[string]json.RawMessage `json:"values"`
	}
	if !readJSON(w, r, &input) {
		return
	}
	if err := input.Draft.validate(); err != nil {
		utils.WriteJsonError(w, http.StatusBadRequest, err)
		return
	}
	problems := input.Draft.validateValues(input.Values)
	utils.WriteJson(w, http.StatusOK, map[string]any{"valid": len(problems) == 0, "errors": problems})
}

func readID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		utils.WriteJsonError(w, http.StatusBadRequest, errors.New("invalid form ID"))
		return 0, false
	}
	return id, true
}

func validRevision(w http.ResponseWriter, revision int64) bool {
	if revision > 0 {
		return true
	}
	utils.WriteJsonError(w, http.StatusBadRequest, errors.New("a draft revision is required"))
	return false
}

func readJSON(w http.ResponseWriter, r *http.Request, input any) bool {
	return readJSONWithLimit(w, r, input, 128<<10)
}

func readJSONWithLimit(w http.ResponseWriter, r *http.Request, input any, limit int64) bool {
	if mediaType, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type")); mediaType != "application/json" {
		utils.WriteJsonError(w, http.StatusUnsupportedMediaType, errors.New("send a JSON request"))
		return false
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, limit))
	decoder.DisallowUnknownFields()
	if decoder.Decode(input) != nil || decoder.Decode(new(any)) != io.EOF {
		utils.WriteJsonError(w, http.StatusBadRequest, errors.New("invalid JSON request"))
		return false
	}
	return true
}

func writeResult(w http.ResponseWriter, status int, result any, err error) {
	switch {
	case err == nil:
		utils.WriteJson(w, status, result)
	case errors.Is(err, errConflict):
		utils.WriteJsonError(w, http.StatusConflict, err)
	case errors.Is(err, errNotFound):
		utils.WriteJsonError(w, http.StatusNotFound, err)
	case errors.Is(err, errEmptyForm), errors.Is(err, errInvalidFollowUp), errors.Is(err, errInvalidApprovalRole):
		utils.WriteJsonError(w, http.StatusBadRequest, err)
	default:
		var databaseError interface{ SQLState() string }
		if errors.As(err, &databaseError) {
			slog.Error("Form operation failed", "sqlstate", databaseError.SQLState())
		}
		utils.WriteJsonError(w, http.StatusInternalServerError, errors.New("could not complete the form operation"))
	}
}
