package user

import (
	"errors"
	"mime"
	"net/http"
	"time"

	"github.com/SufyaanKhateeb/college-placement-app-api/middlewares"
	"github.com/SufyaanKhateeb/college-placement-app-api/service/access"
	"github.com/SufyaanKhateeb/college-placement-app-api/types"
	"github.com/SufyaanKhateeb/college-placement-app-api/utils"
)

func (h *Handler) registerInvitationRoutes(mux *http.ServeMux, prefix string, authenticate func(http.HandlerFunc) http.Handler, origin string) {
	store := access.Store{DB: h.Store.db}
	path := prefix + "/auth/admin-invitation"
	mux.HandleFunc("POST "+path+"/open", invitationWrite(origin, func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Token string `json:"token"`
		}
		if !readPayload(w, r, &input) {
			return
		}
		hash, err := access.InvitationHash(input.Token)
		if err != nil {
			writeInvitationError(w, err)
			return
		}
		invite, err := store.Invitation(r.Context(), hash)
		if err != nil {
			writeInvitationError(w, err)
			return
		}
		http.SetCookie(w, utils.AdminInvitationCookie(origin, input.Token, 24*time.Hour))
		writeInvitation(w, invite)
	}))
	mux.HandleFunc("GET "+path, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		hash, ok := invitationHash(w, r, origin)
		if !ok {
			return
		}
		invite, err := store.Invitation(r.Context(), hash)
		if err != nil {
			writeInvitationError(w, err)
			return
		}
		writeInvitation(w, invite)
	})
	mux.HandleFunc("POST "+path+"/register", invitationWrite(origin, func(w http.ResponseWriter, r *http.Request) {
		hash, ok := invitationHash(w, r, origin)
		if !ok {
			return
		}
		invite, err := store.Invitation(r.Context(), hash)
		if err != nil {
			writeInvitationError(w, err)
			return
		}
		var payload types.RegisterUserPayload
		if !readPayload(w, r, &payload) {
			return
		}
		payload.Email = invite.Email
		if !validRegistration(w, payload) {
			return
		}
		id, err := h.Store.CreateInvitedUser(r.Context(), hash, payload)
		if err != nil {
			writeInvitationError(w, err)
			return
		}
		h.startSession(w, http.StatusCreated, types.TokenInput{Id: id, Type: types.RoleUser})
	}))
	mux.Handle("POST "+path+"/accept", authenticate(invitationWrite(origin, func(w http.ResponseWriter, r *http.Request) {
		hash, ok := invitationHash(w, r, origin)
		if !ok {
			return
		}
		recipient, _ := middlewares.Session(r.Context())
		if err := store.AcceptInvitation(r.Context(), hash, recipient); err != nil {
			writeInvitationError(w, err)
			return
		}
		http.SetCookie(w, utils.AdminInvitationCookie(origin, "", 0))
		w.WriteHeader(http.StatusNoContent)
	})))
}

func invitationWrite(origin string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if r.Header.Get("Origin") != origin {
			utils.WriteJsonError(w, http.StatusForbidden, errors.New("untrusted request origin"))
			return
		}
		if media, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type")); media != "application/json" {
			utils.WriteJsonError(w, http.StatusUnsupportedMediaType, errors.New("send a JSON request"))
			return
		}
		next(w, r)
	}
}

func invitationHash(w http.ResponseWriter, r *http.Request, origin string) ([]byte, bool) {
	cookie, err := r.Cookie(utils.AdminInvitationCookie(origin, "", 0).Name)
	if err != nil {
		writeInvitationError(w, access.ErrInvalidInvitation)
		return nil, false
	}
	hash, err := access.InvitationHash(cookie.Value)
	if err != nil {
		writeInvitationError(w, err)
	}
	return hash, err == nil
}

func writeInvitation(w http.ResponseWriter, invite access.Invitation) {
	utils.WriteJson(w, http.StatusOK, map[string]any{"email": invite.Email, "enrolled": invite.UserID != nil})
}

func writeInvitationError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, access.ErrInvalidInvitation):
		utils.WriteJsonError(w, http.StatusGone, err)
	case errors.Is(err, access.ErrInvitationAccount):
		utils.WriteJsonError(w, http.StatusConflict, err)
	default:
		utils.WriteJsonError(w, http.StatusInternalServerError, errors.New("could not complete the invitation"))
	}
}
