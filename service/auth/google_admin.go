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
	http.Redirect(w, r, g.begin(w, googleAdminFlow, "0"), http.StatusFound)
}

func (g *GoogleSignIn) startAdminLink(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Header.Get("Origin") != g.frontend {
		utils.WriteJsonError(w, http.StatusForbidden, errors.New("invalid request origin"))
		return
	}
	admin, ok := g.activeAdmin(r)
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
	if err := g.accounts.CheckGoogleAdminPassword(r.Context(), admin, input.Password); err != nil {
		if errors.Is(err, ErrGoogleAdminPassword) {
			utils.WriteJsonError(w, http.StatusForbidden, ErrGoogleAdminPassword)
		} else {
			utils.WriteJsonError(w, http.StatusInternalServerError, errors.New("could not start Google connection"))
		}
		return
	}
	utils.WriteJson(w, http.StatusOK, map[string]string{"url": g.begin(w, googleAdminLinkFlow, adminReference(admin))})
}

func (g *GoogleSignIn) activeAdmin(r *http.Request) (types.TokenInput, bool) {
	cookie, err := r.Cookie("ACCESS_TOKEN")
	if err != nil {
		return types.TokenInput{}, false
	}
	token, err := g.sessions.VerifyToken(cookie.Value)
	if err != nil || token == nil || !token.Valid {
		return types.TokenInput{}, false
	}
	claims, ok := token.Claims.(*types.CustomClaims)
	if !ok || claims.Id <= 0 {
		return types.TokenInput{}, false
	}
	admin := types.TokenInput{Id: claims.Id, Type: claims.Type, AdminMode: claims.AdminMode}
	return admin, admin.IsAdmin()
}

func adminReference(admin types.TokenInput) string {
	id := strconv.Itoa(admin.Id)
	if admin.Type == types.RoleUser {
		return "user-" + id
	}
	return id
}

func (g *GoogleSignIn) adminLinkAccount(r *http.Request, reference string) (types.TokenInput, bool) {
	admin, ok := g.activeAdmin(r)
	return admin, ok && adminReference(admin) == reference
}

func (g *GoogleSignIn) adminLinkStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	admin, ok := g.activeAdmin(r)
	if !ok {
		utils.WriteJsonError(w, http.StatusUnauthorized, errors.New("admin session required"))
		return
	}
	linked, err := g.accounts.GoogleAdminLinked(r.Context(), admin, googleIssuer)
	if err != nil {
		utils.WriteJsonError(w, http.StatusInternalServerError, errors.New("could not check Google connection"))
		return
	}
	utils.WriteJson(w, http.StatusOK, map[string]bool{"linked": linked})
}
