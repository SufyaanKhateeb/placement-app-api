package auth

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/SufyaanKhateeb/college-placement-app-api/types"
	"github.com/SufyaanKhateeb/college-placement-app-api/utils"
)

var ErrGoogleUserPassword = errors.New("sign in with your verified password account to connect Google")

func (g *GoogleSignIn) activeUser(r *http.Request) (types.TokenInput, bool) {
	account, ok := g.activeAccount(r)
	return account, ok && account.Type == types.RoleUser && !account.IsAdmin()
}

func (g *GoogleSignIn) startUserLink(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Header.Get("Origin") != g.frontend {
		utils.WriteJsonError(w, http.StatusForbidden, errors.New("invalid request origin"))
		return
	}
	user, ok := g.activeUser(r)
	if !ok {
		utils.WriteJsonError(w, http.StatusUnauthorized, ErrGoogleUserPassword)
		return
	}
	password, ok := googleLinkPassword(w, r)
	if !ok {
		return
	}
	if err := g.accounts.CheckGoogleUserPassword(r.Context(), user.Id, password); err != nil {
		if errors.Is(err, ErrGoogleUserPassword) {
			utils.WriteJsonError(w, http.StatusForbidden, err)
		} else {
			utils.WriteJsonError(w, http.StatusInternalServerError, errors.New("could not start Google connection"))
		}
		return
	}
	utils.WriteJson(w, http.StatusOK, map[string]string{"url": g.begin(w, googleUserLinkFlow, strconv.Itoa(user.Id))})
}

func (g *GoogleSignIn) finishUserLink(w http.ResponseWriter, r *http.Request, ctx context.Context, id int, identity GoogleIdentity) {
	err := g.accounts.LinkGoogleUser(ctx, id, identity)
	switch {
	case err == nil:
		http.Redirect(w, r, g.frontend+"/account", http.StatusSeeOther)
	case errors.Is(err, ErrGoogleAdminEmailMismatch):
		g.redirectError(w, r, googleUserLinkFlow, "google_user_email_mismatch")
	case errors.Is(err, ErrGoogleAdminLinkConflict):
		g.redirectError(w, r, googleUserLinkFlow, "google_user_link_conflict")
	case errors.Is(err, ErrGoogleUserPassword):
		g.redirectError(w, r, googleUserLinkFlow, "google_user_session_expired")
	default:
		g.redirectError(w, r, googleUserLinkFlow, "google_failed")
	}
}

func (g *GoogleSignIn) userLinkStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	user, ok := g.activeUser(r)
	if !ok {
		utils.WriteJsonError(w, http.StatusUnauthorized, ErrGoogleUserPassword)
		return
	}
	linked, canLink, err := g.accounts.GoogleUserLinkStatus(r.Context(), user.Id, googleIssuer)
	if err != nil {
		utils.WriteJsonError(w, http.StatusInternalServerError, errors.New("could not check Google connection"))
		return
	}
	utils.WriteJson(w, http.StatusOK, map[string]bool{"linked": linked, "canLink": canLink})
}
