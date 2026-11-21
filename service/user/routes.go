package user

import (
	"errors"
	"net/http"
	"net/mail"
	"regexp"
	"time"

	"github.com/SufyaanKhateeb/college-placement-app-api/config"
	"github.com/SufyaanKhateeb/college-placement-app-api/middlewares"
	"github.com/SufyaanKhateeb/college-placement-app-api/types"
	"github.com/SufyaanKhateeb/college-placement-app-api/utils"
)

type Handler struct {
	UserService types.UserService
	AuthService types.AuthService
}

func NewHandler(users types.UserService, auth types.AuthService) *Handler {
	return &Handler{UserService: users, AuthService: auth}
}

func (h *Handler) RegisterRoutes(mux *http.ServeMux, prefix string) {
	mux.HandleFunc("POST "+prefix+"/login", h.login(types.RoleUser))
	mux.HandleFunc("POST "+prefix+"/login-admin", h.login(types.RoleAdmin))
	mux.HandleFunc("POST "+prefix+"/register", h.register(types.RoleUser))

	authenticate := middlewares.Authenticate(h.AuthService, "")
	adminOnly := middlewares.Authenticate(h.AuthService, types.RoleAdmin)
	mux.Handle("POST "+prefix+"/register-admin", adminOnly(h.register(types.RoleAdmin)))
	mux.Handle("POST "+prefix+"/refresh", authenticate(h.profile))
	mux.Handle("POST "+prefix+"/logout", authenticate(h.logout))
	mux.Handle("GET "+prefix+"/user", authenticate(h.profile))
}

func (h *Handler) profile(w http.ResponseWriter, r *http.Request) {
	session, ok := middlewares.Session(r.Context())
	if !ok {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}

	profile := types.UserDto{Role: session.Type, UType: session.Type}
	if session.Type == types.RoleUser {
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

func (h *Handler) logout(w http.ResponseWriter, _ *http.Request) {
	utils.WriteJwtToCookie(w, "ACCESS_TOKEN", "", 0)
	utils.WriteJwtToCookie(w, "REFRESH_TOKEN", "", 0)
	utils.WriteJson(w, http.StatusAccepted, nil)
}

func (h *Handler) login(role string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var payload types.LoginUserPayload
		if !readPayload(w, r, &payload) {
			return
		}
		if !validEmail(payload.Email) || payload.Password == "" {
			utils.WriteJsonError(w, http.StatusBadRequest, errors.New("a valid email and password are required"))
			return
		}
		if role == types.RoleAdmin {
			user, err := h.UserService.LoginAdminUser(r.Context(), &payload)
			if err != nil {
				utils.WriteJsonError(w, err.StatusCode, err.Err)
				return
			}
			h.startSession(w, http.StatusOK, role, user.Id)
			return
		}
		user, err := h.UserService.LoginUser(r.Context(), &payload)
		if err != nil {
			utils.WriteJsonError(w, err.StatusCode, err.Err)
			return
		}
		h.startSession(w, http.StatusOK, role, user.Id)
	}
}

var passwordLetter = regexp.MustCompile("[a-zA-Z]")
var passwordDigit = regexp.MustCompile("[0-9]")
var passwordSpecial = regexp.MustCompile(`[!@#$%^&*(),.?":{}|<>]`)

func (h *Handler) register(role string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var payload types.RegisterUserPayload
		if !readPayload(w, r, &payload) {
			return
		}
		if payload.FirstName == "" || payload.LastName == "" || !validEmail(payload.Email) {
			utils.WriteJsonError(w, http.StatusBadRequest, errors.New("first name, last name, and a valid email are required"))
			return
		}
		password := payload.Password
		if len([]rune(password)) < 8 || len(password) > 72 || !passwordLetter.MatchString(password) ||
			!passwordDigit.MatchString(password) || !passwordSpecial.MatchString(password) {
			utils.WriteJsonError(w, http.StatusBadRequest, errors.New("password must have at least 8 characters, at most 72 bytes, and include a letter, digit, and special character"))
			return
		}
		if role == types.RoleAdmin {
			user, err := h.UserService.RegisterAdminUser(r.Context(), &payload)
			if err != nil {
				utils.WriteJsonError(w, err.StatusCode, err.Err)
				return
			}
			h.startSession(w, http.StatusCreated, role, user.Id)
			return
		}
		user, err := h.UserService.RegisterUser(r.Context(), &payload)
		if err != nil {
			utils.WriteJsonError(w, err.StatusCode, err.Err)
			return
		}
		h.startSession(w, http.StatusCreated, role, user.Id)
	}
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

func (h *Handler) startSession(w http.ResponseWriter, status int, role string, id int) {
	access, refresh, err := h.AuthService.CreateTokens(&types.TokenInput{Id: id, Type: role})
	if err != nil {
		utils.WriteJsonError(w, http.StatusInternalServerError, errors.New("could not start session"))
		return
	}
	utils.WriteJwtToCookie(w, "ACCESS_TOKEN", access, time.Duration(config.Env.JWTExpirationTime)*time.Second)
	utils.WriteJwtToCookie(w, "REFRESH_TOKEN", refresh, 30*24*time.Hour)
	utils.WriteJson(w, status, nil)
}
