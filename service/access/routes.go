package access

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"strconv"
	"strings"

	"github.com/SufyaanKhateeb/college-placement-app-api/middlewares"
	"github.com/SufyaanKhateeb/college-placement-app-api/types"
	"github.com/SufyaanKhateeb/college-placement-app-api/utils"
	"github.com/jackc/pgx/v5"
)

type handler struct {
	store  Store
	origin string
	mailer InvitationMailer
}

func (s Store) RegisterRoutes(mux *http.ServeMux, auth types.AuthService, origin string, mailer InvitationMailer) {
	h := handler{store: s, origin: origin, mailer: mailer}
	requireManager := middlewares.Authenticate(auth, s.Resolve, types.RoleAdmin, ManageRoles)
	const path = "/api/v1/admin/access"
	mux.Handle("GET "+path, requireManager(h.catalog))
	mux.Handle("GET "+path+"/accounts", requireManager(h.accounts))
	mux.Handle("GET "+path+"/accounts/{kind}/{id}/roles", requireManager(h.accountRoles))
	mux.Handle("GET "+path+"/audit", requireManager(h.audit))
	mux.Handle("POST "+path+"/roles", requireManager(h.write(h.saveRole)))
	mux.Handle("GET "+path+"/roles/{role}", requireManager(h.getRole))
	mux.Handle("PUT "+path+"/roles/{role}", requireManager(h.write(h.saveRole)))
	mux.Handle("PUT "+path+"/accounts/{kind}/{id}/roles/{role}", requireManager(h.write(h.assignment)))
	mux.Handle("DELETE "+path+"/accounts/{kind}/{id}/roles/{role}", requireManager(h.write(h.assignment)))
	mux.Handle("DELETE "+path+"/accounts/user/{id}/approvals/{submission}", requireManager(h.write(h.revokeApproval)))
	mux.Handle("PUT "+path+"/legacy-approval-roles/{key}", requireManager(h.write(h.mapLegacyRole)))
	mux.Handle("POST "+path+"/invitations", middlewares.Authenticate(auth, s.Resolve, types.RoleAdmin, InviteAdmins)(h.write(h.inviteAdmin)))
	mux.Handle("POST "+path+"/accounts/user/{id}/promote", middlewares.Authenticate(auth, s.Resolve, types.RoleAdmin, PromoteAdmins)(h.write(h.promoteAdmin(false))))
	mux.Handle("PATCH "+path+"/accounts/user/{id}", middlewares.Authenticate(auth, s.Resolve, types.RoleAdmin, PromoteAdmins)(h.write(h.promoteAdmin(true))))
}

func (h handler) catalog(w http.ResponseWriter, r *http.Request) {
	value, err := h.store.catalog(r.Context())
	respond(w, http.StatusOK, value, err)
}

func (h handler) accounts(w http.ResponseWriter, r *http.Request) {
	search := strings.TrimSpace(r.URL.Query().Get("q"))
	if len(search) > 320 {
		utils.WriteJsonError(w, http.StatusBadRequest, errors.New("account search is too long"))
		return
	}
	value, err := h.store.accounts(r.Context(), search)
	respond(w, http.StatusOK, value, err)
}

func (h handler) accountRoles(w http.ResponseWriter, r *http.Request) {
	kind, id, ok := readAccount(w, r)
	if !ok {
		return
	}
	value, err := h.store.accountRoles(r.Context(), kind, id)
	respond(w, http.StatusOK, value, err)
}

func (h handler) audit(w http.ResponseWriter, r *http.Request) {
	var before *int64
	if raw := r.URL.Query().Get("before"); raw != "" {
		id, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || id <= 0 {
			utils.WriteJsonError(w, http.StatusBadRequest, errors.New("invalid audit cursor"))
			return
		}
		before = &id
	}
	value, err := h.store.auditEntries(r.Context(), before)
	respond(w, http.StatusOK, value, err)
}

func (h handler) getRole(w http.ResponseWriter, r *http.Request) {
	id, ok := readID(w, r, "role")
	if !ok {
		return
	}
	value, err := h.store.getRole(r.Context(), id)
	respond(w, http.StatusOK, value, err)
}

func (h handler) saveRole(w http.ResponseWriter, r *http.Request) {
	var id int64
	status := http.StatusCreated
	if r.Method == http.MethodPut {
		var ok bool
		id, ok = readID(w, r, "role")
		if !ok {
			return
		}
		status = http.StatusOK
	}
	var input roleInput
	if !readJSON(w, r, &input) {
		return
	}
	actor, _ := middlewares.Session(r.Context())
	err := h.store.change(r.Context(), actor, func(tx pgx.Tx) error {
		var err error
		id, err = changeRole(r.Context(), tx, actor, id, input)
		return err
	})
	if err == nil && status == http.StatusCreated {
		w.Header().Set("Location", fmt.Sprintf("/api/v1/admin/access/roles/%d", id))
	}
	respond(w, status, map[string]int64{"id": id}, err)
}

func (h handler) assignment(w http.ResponseWriter, r *http.Request) {
	kind, id, ok := readAccount(w, r)
	if !ok {
		return
	}
	roleID, ok := readID(w, r, "role")
	if !ok {
		return
	}
	actor, _ := middlewares.Session(r.Context())
	err := h.store.change(r.Context(), actor, func(tx pgx.Tx) error {
		return changeAssignment(r.Context(), tx, actor, kind, id, roleID, r.Method == http.MethodPut)
	})
	respond(w, http.StatusNoContent, nil, err)
}

func (h handler) revokeApproval(w http.ResponseWriter, r *http.Request) {
	id, ok := readID(w, r, "id")
	if !ok || id > 2147483647 {
		if ok {
			utils.WriteJsonError(w, http.StatusBadRequest, errors.New("invalid account ID"))
		}
		return
	}
	submissionID, ok := readID(w, r, "submission")
	if !ok {
		return
	}
	actor, _ := middlewares.Session(r.Context())
	err := h.store.change(r.Context(), actor, func(tx pgx.Tx) error {
		return revokeApproval(r.Context(), tx, actor, int(id), submissionID)
	})
	respond(w, http.StatusNoContent, nil, err)
}

func (h handler) mapLegacyRole(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	var input struct {
		RoleID int64 `json:"roleId"`
	}
	if !readJSON(w, r, &input) {
		return
	}
	if !validKey.MatchString(key) || input.RoleID <= 0 {
		utils.WriteJsonError(w, http.StatusBadRequest, errInvalidRole)
		return
	}
	actor, _ := middlewares.Session(r.Context())
	err := h.store.change(r.Context(), actor, func(tx pgx.Tx) error {
		return mapLegacyRole(r.Context(), tx, actor, key, input.RoleID)
	})
	respond(w, http.StatusNoContent, nil, err)
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

func readAccount(w http.ResponseWriter, r *http.Request) (string, int, bool) {
	kind := r.PathValue("kind")
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 32)
	if err != nil || id <= 0 || (kind != types.RoleAdmin && kind != types.RoleUser) {
		utils.WriteJsonError(w, http.StatusBadRequest, errors.New("invalid account"))
		return "", 0, false
	}
	return kind, int(id), true
}

func readID(w http.ResponseWriter, r *http.Request, param string) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue(param), 10, 64)
	if err != nil || id <= 0 {
		utils.WriteJsonError(w, http.StatusBadRequest, errors.New("invalid "+param+" ID"))
		return 0, false
	}
	return id, true
}

func readJSON(w http.ResponseWriter, r *http.Request, input any) bool {
	if media, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type")); media != "application/json" {
		utils.WriteJsonError(w, http.StatusUnsupportedMediaType, errors.New("send a JSON request"))
		return false
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10))
	decoder.DisallowUnknownFields()
	if decoder.Decode(input) != nil || decoder.Decode(new(any)) != io.EOF {
		utils.WriteJsonError(w, http.StatusBadRequest, errors.New("invalid JSON request"))
		return false
	}
	return true
}

func respond(w http.ResponseWriter, status int, value any, err error) {
	switch {
	case err == nil && status == http.StatusNoContent:
		w.WriteHeader(status)
	case err == nil:
		utils.WriteJson(w, status, value)
	case errors.Is(err, pgx.ErrNoRows):
		utils.WriteJsonError(w, http.StatusNotFound, errors.New("account, role, or grant not found"))
	case errors.Is(err, errDenied):
		utils.WriteJsonError(w, http.StatusForbidden, err)
	case errors.Is(err, errLastManager), errors.Is(err, errMappingExists),
		errors.Is(err, errEmailInUse), errors.Is(err, errAlreadyAdmin), errors.Is(err, errAccountChanged):
		utils.WriteJsonError(w, http.StatusConflict, err)
	case errors.Is(err, errInvalidRole), errors.Is(err, errReservedRole), errors.Is(err, errUnsafeApproval), errors.Is(err, errAdminRequired):
		utils.WriteJsonError(w, http.StatusBadRequest, err)
	default:
		var databaseError interface{ SQLState() string }
		if errors.As(err, &databaseError) {
			slog.Error("Access operation failed", "sqlstate", databaseError.SQLState())
		}
		utils.WriteJsonError(w, http.StatusInternalServerError, errors.New("could not complete the access operation"))
	}
}
