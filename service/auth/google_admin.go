package auth

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/SufyaanKhateeb/college-placement-app-api/types"
	"github.com/SufyaanKhateeb/college-placement-app-api/utils"
)

var (
	ErrGoogleAdminUnlinked      = errors.New("Google identity is not linked to an admin account")
	ErrGoogleAdminEmailMismatch = errors.New("Google email does not match the admin account")
	ErrGoogleAdminLinkConflict  = errors.New("Google identity or admin account is already linked")
	ErrGoogleAdminPassword      = errors.New("incorrect admin password")
)

func (g *GoogleSignIn) startAdmin(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, g.begin(w, googleAdminFlow, 0), http.StatusFound)
}

func (g *GoogleSignIn) startAdminLink(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Header.Get("Origin") != g.frontend {
		utils.WriteJsonError(w, http.StatusForbidden, errors.New("invalid request origin"))
		return
	}
	adminID, ok := g.activeAdminID(r)
	if !ok {
		utils.WriteJsonError(w, http.StatusUnauthorized, errors.New("sign in as an admin to connect Google"))
		return
	}
	body := http.MaxBytesReader(w, r.Body, 4096)
	decoder := json.NewDecoder(body)
	var input struct {
		Password string `json:"password"`
	}
	if decoder.Decode(&input) != nil || input.Password == "" || decoder.Decode(new(any)) != io.EOF {
		utils.WriteJsonError(w, http.StatusBadRequest, errors.New("admin password is required"))
		return
	}
	if err := g.accounts.CheckGoogleAdminPassword(r.Context(), adminID, input.Password); err != nil {
		if errors.Is(err, ErrGoogleAdminPassword) {
			utils.WriteJsonError(w, http.StatusForbidden, ErrGoogleAdminPassword)
		} else {
			utils.WriteJsonError(w, http.StatusInternalServerError, errors.New("could not start Google connection"))
		}
		return
	}
	utils.WriteJson(w, http.StatusOK, map[string]string{"url": g.begin(w, googleAdminLinkFlow, adminID)})
}

func (g *GoogleSignIn) activeAdminID(r *http.Request) (int, bool) {
	cookie, err := r.Cookie("ACCESS_TOKEN")
	if err != nil {
		return 0, false
	}
	token, err := g.sessions.VerifyToken(cookie.Value)
	if err != nil || token == nil || !token.Valid {
		return 0, false
	}
	claims, ok := token.Claims.(*types.CustomClaims)
	if !ok || claims.Id <= 0 || claims.Type != types.RoleAdmin {
		return 0, false
	}
	return claims.Id, true
}

func (g *GoogleSignIn) adminLinkID(r *http.Request, flowID string) (int, bool) {
	id, err := strconv.Atoi(flowID)
	active, ok := g.activeAdminID(r)
	return id, err == nil && ok && id == active
}

func (g *GoogleSignIn) adminLinkStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	id, ok := g.activeAdminID(r)
	if !ok {
		utils.WriteJsonError(w, http.StatusUnauthorized, errors.New("admin session required"))
		return
	}
	linked, err := g.accounts.GoogleAdminLinked(r.Context(), id, googleIssuer)
	if err != nil {
		utils.WriteJsonError(w, http.StatusInternalServerError, errors.New("could not check Google connection"))
		return
	}
	utils.WriteJson(w, http.StatusOK, map[string]bool{"linked": linked})
}
