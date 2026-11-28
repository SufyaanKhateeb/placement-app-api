package user

import (
	"context"
	"errors"
	"net/http"
	"net/mail"
	"regexp"
	"time"

	"github.com/SufyaanKhateeb/college-placement-app-api/config"
	"github.com/SufyaanKhateeb/college-placement-app-api/middlewares"
	"github.com/SufyaanKhateeb/college-placement-app-api/service/auth"
	"github.com/SufyaanKhateeb/college-placement-app-api/types"
	"github.com/SufyaanKhateeb/college-placement-app-api/utils"
)

type Handler struct {
	UserService     types.UserService
	AuthService     types.AuthService
	Store           *Store
	EmailCodeMailer func(context.Context, string, string) error
	frontendOrigin  string
	prefix          string
}

func NewHandler(users types.UserService, auth types.AuthService) *Handler {
	return &Handler{UserService: users, AuthService: auth}
}

func (h *Handler) RegisterRoutes(mux *http.ServeMux, prefix string, resolve middlewares.ResolvePermissions, frontendOrigin string) {
	h.prefix, h.frontendOrigin = prefix, frontendOrigin
	mux.HandleFunc("POST "+prefix+"/login", h.login(types.RoleUser))
	mux.HandleFunc("POST "+prefix+"/auth/sessions", h.login(""))
	mux.HandleFunc("POST "+prefix+"/users", h.register)
	mux.HandleFunc("POST "+prefix+"/login-admin", h.login(types.RoleAdmin))
	mux.HandleFunc("POST "+prefix+"/register", h.register)

	authenticate := middlewares.Authenticate(h.AuthService, resolve, "")
	mux.HandleFunc("POST "+prefix+"/register-admin", func(w http.ResponseWriter, _ *http.Request) {
		utils.WriteJsonError(w, http.StatusGone, errors.New("use an admin invitation or promote an existing account"))
	})
	if h.Store != nil {
		h.registerInvitationRoutes(mux, prefix, authenticate, frontendOrigin)
		h.registerEmailVerificationRoutes(mux, prefix, frontendOrigin)
	}
	mux.Handle("POST "+prefix+"/refresh", authenticate(h.profile))
	mux.Handle("POST "+prefix+"/logout", authenticate(h.logout))
	mux.Handle("GET "+prefix+"/users/me", authenticate(h.profile))
	mux.Handle("POST "+prefix+"/auth/sessions/current/renewals", authenticate(h.profile))
	mux.Handle("DELETE "+prefix+"/auth/sessions/current", authenticate(h.logout))
	mux.Handle("GET "+prefix+"/user", authenticate(h.profile))
}

func (h *Handler) profile(w http.ResponseWriter, r *http.Request) {
	session, ok := middlewares.Session(r.Context())
	if !ok {
		utils.WriteJsonError(w, http.StatusUnauthorized, errors.New("authentication required"))
		return
	}

	profile := types.UserDto{
		Role: session.Type, UType: session.Type, AccountType: session.Type,
		Permissions: middlewares.Permissions(r.Context()),
	}
	if session.IsAdmin() {
		profile.Role, profile.UType = types.RoleAdmin, types.RoleAdmin
	} else {
		profile.UType = types.LegacyRoleStudent
	}
	if session.Type == types.RoleAdmin {
		user, err := h.UserService.GetAdminUserById(r.Context(), session.Id)
		if err != nil {
			utils.WriteJsonError(w, err.StatusCode, err.Err)
			return
		}
		profile.Id, profile.FirstName, profile.LastName, profile.Email = user.Id, user.FirstName, user.LastName, user.Email
	} else {
		user, err := h.UserService.GetUserById(r.Context(), session.Id)
		if err != nil {
			utils.WriteJsonError(w, err.StatusCode, err.Err)
			return
		}
		profile.Id, profile.FirstName, profile.LastName, profile.Email = user.Id, user.FirstName, user.LastName, user.Email
		profile.Verified = user.Verified
	}
	utils.WriteJson(w, http.StatusOK, profile)
}

func (h *Handler) logout(w http.ResponseWriter, r *http.Request) {
	if origin := r.Header.Get("Origin"); r.Method == http.MethodDelete && origin != "" && origin != h.frontendOrigin {
		utils.WriteJsonError(w, http.StatusForbidden, errors.New("untrusted request origin"))
		return
	}
	utils.WriteJwtToCookie(w, "ACCESS_TOKEN", "", 0)
	utils.WriteJwtToCookie(w, "REFRESH_TOKEN", "", 0)
	if r.Method == http.MethodDelete {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	utils.WriteJson(w, http.StatusAccepted, nil)
}

func (h *Handler) login(role string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var payload types.LoginUserPayload
		canonical := role == ""
		mode, success := role, http.StatusOK
		if canonical {
			var input struct {
				types.LoginUserPayload
				Mode string `json:"mode"`
			}
			if !readPayload(w, r, &input) {
				return
			}
			payload, mode, success = input.LoginUserPayload, input.Mode, http.StatusNoContent
			if mode != types.RoleUser && mode != types.RoleAdmin {
				utils.WriteJsonError(w, http.StatusBadRequest, errors.New("sign-in mode must be user or admin"))
				return
			}
		} else if !readPayload(w, r, &payload) {
			return
		}
		if !validEmail(payload.Email) || payload.Password == "" {
			utils.WriteJsonError(w, http.StatusBadRequest, errors.New("a valid email and password are required"))
			return
		}
		if mode == types.RoleAdmin {
			user, err := h.UserService.LoginAdminUser(r.Context(), &payload)
			if err == nil {
				h.startSession(w, success, types.TokenInput{Id: user.Id, Type: types.RoleAdmin})
				return
			}
			if h.Store != nil {
				member, memberErr := h.UserService.LoginUser(r.Context(), &payload)
				if memberErr == nil {
					isAdmin, lookupErr := h.Store.IsPromotedAdmin(r.Context(), member.Id)
					if lookupErr != nil {
						utils.WriteJsonError(w, http.StatusInternalServerError, errors.New("could not sign in"))
						return
					}
					if isAdmin {
						h.startSession(w, success, types.TokenInput{Id: member.Id, Type: types.RoleUser, AdminMode: true})
						return
					}
				}
			}
			writeLoginError(w, err.StatusCode, err.Err, canonical)
			return
		}
		user, err := h.UserService.LoginUser(r.Context(), &payload)
		if err != nil {
			writeLoginError(w, err.StatusCode, err.Err, canonical)
			return
		}
		if h.Store != nil {
			verified, err := h.Store.passwordEmailVerified(r.Context(), user.Id, user.Email)
			if err != nil {
				writeEmailVerificationError(w, err)
				return
			}
			if !verified {
				h.startEmailVerification(w, r, emailChallenge{recipient: user.Email, userID: &user.Id, passwordHash: user.Password}, canonical)
				return
			}
		}
		h.startSession(w, success, types.TokenInput{Id: user.Id, Type: types.RoleUser})
	}
}

func writeLoginError(w http.ResponseWriter, status int, err error, canonical bool) {
	if status == http.StatusBadRequest {
		if canonical {
			status = http.StatusUnauthorized
		}
		utils.WriteAPIError(w, status, "invalid_credentials", err, nil)
		return
	}
	utils.WriteJsonError(w, status, err)
}

var passwordLetter = regexp.MustCompile("[a-zA-Z]")
var passwordDigit = regexp.MustCompile("[0-9]")
var passwordSpecial = regexp.MustCompile(`[!@#$%^&*(),.?":{}|<>]`)

func (h *Handler) register(w http.ResponseWriter, r *http.Request) {
	var payload types.RegisterUserPayload
	if !readPayload(w, r, &payload) || !validRegistration(w, payload) {
		return
	}
	if h.Store != nil {
		hash, err := auth.HashPassword(payload.Password)
		if err != nil {
			writeEmailVerificationError(w, err)
			return
		}
		h.startEmailVerification(w, r, emailChallenge{
			recipient: payload.Email, firstName: payload.FirstName,
			lastName: payload.LastName, passwordHash: hash,
		}, r.URL.Path == h.prefix+"/users")
		return
	}
	user, err := h.UserService.RegisterUser(r.Context(), &payload)
	if err != nil {
		utils.WriteJsonError(w, err.StatusCode, err.Err)
		return
	}
	h.startSession(w, http.StatusCreated, types.TokenInput{Id: user.Id, Type: types.RoleUser})
}

func validRegistration(w http.ResponseWriter, payload types.RegisterUserPayload) bool {
	if payload.FirstName == "" || payload.LastName == "" || !validEmail(payload.Email) {
		utils.WriteJsonError(w, http.StatusBadRequest, errors.New("first name, last name, and a valid email are required"))
		return false
	}
	password := payload.Password
	if len([]rune(password)) < 8 || len(password) > 72 || !passwordLetter.MatchString(password) ||
		!passwordDigit.MatchString(password) || !passwordSpecial.MatchString(password) {
		utils.WriteJsonError(w, http.StatusBadRequest, errors.New("password must have at least 8 characters, at most 72 bytes, and include a letter, digit, and special character"))
		return false
	}
	return true
}

func validEmail(email string) bool {
	address, err := mail.ParseAddress(email)
	return err == nil && address.Address == email
}

func readPayload(w http.ResponseWriter, r *http.Request, payload any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	if err := utils.ParseJson(r, payload); err != nil {
		utils.WriteJsonError(w, http.StatusBadRequest, err)
		return false
	}
	return true
}

func (h *Handler) startSession(w http.ResponseWriter, status int, account types.TokenInput) {
	access, refresh, err := h.AuthService.CreateTokens(&account)
	if err != nil {
		utils.WriteJsonError(w, http.StatusInternalServerError, errors.New("could not start session"))
		return
	}
	utils.WriteJwtToCookie(w, "ACCESS_TOKEN", access, time.Duration(config.Env.JWTExpirationTime)*time.Second)
	utils.WriteJwtToCookie(w, "REFRESH_TOKEN", refresh, 30*24*time.Hour)
	if status == http.StatusNoContent {
		w.WriteHeader(status)
		return
	}
	if status == http.StatusCreated {
		w.Header().Set("Location", h.prefix+"/users/me")
	}
	utils.WriteJson(w, status, nil)
}
