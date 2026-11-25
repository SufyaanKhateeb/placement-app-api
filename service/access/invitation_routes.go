package access

import (
	"context"
	"errors"
	"net/http"
	"net/mail"
	"strconv"
	"strings"

	"github.com/SufyaanKhateeb/college-placement-app-api/middlewares"
	"github.com/SufyaanKhateeb/college-placement-app-api/utils"
)

type InvitationMailer func(ctx context.Context, email, link string) error

func (h handler) inviteAdmin(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Email string `json:"email"`
	}
	if !readJSON(w, r, &input) {
		return
	}
	email := strings.TrimSpace(input.Email)
	if !ValidInvitationEmail(email) {
		utils.WriteJsonError(w, http.StatusBadRequest, errors.New("a valid email is required"))
		return
	}
	if h.mailer == nil {
		utils.WriteJsonError(w, http.StatusServiceUnavailable, errors.New("invitation email is not configured"))
		return
	}
	actor, _ := middlewares.Session(r.Context())
	token, err := h.store.createInvitation(r.Context(), actor, email)
	if err != nil {
		respond(w, http.StatusAccepted, nil, err)
		return
	}
	link := strings.TrimRight(h.origin, "/") + "/admin/invitation#" + token
	if err := h.mailer(r.Context(), strings.ToLower(email), link); err != nil {
		utils.WriteJsonError(w, http.StatusBadGateway, errors.New("email delivery could not be confirmed; resend to create a new link"))
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

func (h handler) promoteAdmin(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 32)
	if err != nil || id <= 0 {
		utils.WriteJsonError(w, http.StatusBadRequest, errors.New("invalid account ID"))
		return
	}
	var input struct {
		Email string `json:"email"`
	}
	if !readJSON(w, r, &input) {
		return
	}
	if !ValidInvitationEmail(input.Email) {
		utils.WriteJsonError(w, http.StatusBadRequest, errors.New("confirm the selected account email"))
		return
	}
	actor, _ := middlewares.Session(r.Context())
	respond(w, http.StatusNoContent, nil, h.store.promote(r.Context(), actor, int(id), input.Email))
}

func ValidInvitationEmail(email string) bool {
	address, err := mail.ParseAddress(email)
	return err == nil && address.Address == email && len(email) <= 254
}
