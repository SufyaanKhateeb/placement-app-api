package user

import (
	"fmt"
	"net/http"
	"time"

	"github.com/SufyaanKhateeb/college-placement-app-api/config"
	"github.com/SufyaanKhateeb/college-placement-app-api/contextKeys"
	"github.com/SufyaanKhateeb/college-placement-app-api/middlewares"
	"github.com/SufyaanKhateeb/college-placement-app-api/types"
	"github.com/SufyaanKhateeb/college-placement-app-api/utils"
	"github.com/go-chi/chi/v5"
	"github.com/go-playground/validator/v10"
)

type Handler struct {
	UserService types.UserService
	AuthService types.AuthService
}

func NewHandler(userService types.UserService, authService types.AuthService) *Handler {
	return &Handler{
		UserService: userService,
		AuthService: authService,
	}
}

func (h *Handler) RegisterRoutes(r *chi.Mux) {
	// Public Routes
	r.Group(func(r chi.Router) {
		r.Post("/login", h.handleLogin)
		r.Post("/login-admin", h.handleAdminLogin)
		r.Post("/register", h.handleRegister)
	})

	// Admin Routes
	// Require Admin Authentication
	r.Group(func(r chi.Router) {
		r.Use(middlewares.AuthMiddleware(h.AuthService), middlewares.RequireAdminUser)
		r.Post("/register-admin", h.handleAdminRegister)
	})

	// Private Routes
	// Require Authentication
	r.Group(func(r chi.Router) {
		r.Use(middlewares.AuthMiddleware(h.AuthService), middlewares.RequireUser)
		r.Post("/refresh", h.handleRefresh)
		r.Post("/logout", h.handleLogout)
		r.Get("/user", h.getUser)
	})
}

func (h *Handler) getUser(w http.ResponseWriter, r *http.Request) {
	tokenInput := r.Context().Value(contextKeys.TokenInputKey).(types.TokenInput)
	u, err := h.UserService.GetStudentUserById(r.Context(), tokenInput.Id)
	if err != nil {
		utils.WriteJsonError(w, utils.GetHttpStatusCodeFromContext(r.Context()), fmt.Errorf("invalid user, user not found"))
		return
	}

	utils.WriteJson(w, http.StatusOK, types.UserDto{
		Id:        u.Id,
		UType:     tokenInput.Type,
		FirstName: u.FirstName,
		LastName:  u.LastName,
		Email:     u.Email,
		Verified:  u.Verified,
	})
}

func (h *Handler) handleRefresh(w http.ResponseWriter, r *http.Request) {
	if tokenInput, ok := r.Context().Value(contextKeys.TokenInputKey).(types.TokenInput); ok {
		u, err := h.UserService.GetStudentUserById(r.Context(), tokenInput.Id)
		if err != nil {
			utils.WriteJsonError(w, utils.GetHttpStatusCodeFromContext(r.Context()), fmt.Errorf("invalid user, user not found"))
			return
		}

		utils.WriteJson(w, http.StatusOK, types.UserDto{
			Id:        u.Id,
			UType:     tokenInput.Type,
			FirstName: u.FirstName,
			LastName:  u.LastName,
			Email:     u.Email,
			Verified:  u.Verified,
		})
		return
	}
	utils.WriteJsonError(w, http.StatusUnauthorized, fmt.Errorf("Unauthorized"))
}

func (h *Handler) handleLogout(w http.ResponseWriter, r *http.Request) {
	// Check for valid user not needed but keeping if required later
	// ctxUser := r.Context().Value("user").(types.UserDto)
	// _, err1 := h.UserService.GetAdminUserById(r.Context(), ctxUser.Id)
	// _, err2 := h.UserService.GetStudentUserById(r.Context(), ctxUser.Id)
	// if err1 != nil && err2 != nil {
	// 	utils.WriteJsonError(w, utils.GetHttpStatusCodeFromContext(r.Context()), fmt.Errorf("invalid request"))
	// 	return
	// }

	utils.WriteJwtToCookie(w, "ACCESS_TOKEN", "", time.Duration(0))
	utils.WriteJwtToCookie(w, "REFRESH_TOKEN", "", time.Duration(0))

	utils.WriteJson(w, http.StatusAccepted, nil)
}

func (h *Handler) handleLogin(w http.ResponseWriter, r *http.Request) {
	// get the json payload
	var payload types.LoginUserPayload
	if err := utils.ParseJson(r, &payload); err != nil {
		utils.WriteJsonError(w, http.StatusBadRequest, err)
		return
	}

	// validate the payload
	if err := utils.GetValidator().Struct(payload); err != nil {
		errors := err.(validator.ValidationErrors)
		utils.WriteJsonError(w, http.StatusBadRequest, fmt.Errorf("invalid payload %v", errors))
		return
	}

	// call user service login
	u, err := h.UserService.LoginStudentUser(r.Context(), &payload)
	if err != nil {
		utils.WriteJsonError(w, utils.GetHttpStatusCodeFromContext(r.Context()), err)
		return
	}

	// create a jwt tokens
	accessToken, refreshToken, err := h.AuthService.CreateTokens(&types.TokenInput{
		Id:   u.Id,
		Type: "student",
	})
	if err != nil {
		utils.WriteJsonError(w, http.StatusInternalServerError, err)
	}

	// add the access tokens to the response cookies
	utils.WriteJwtToCookie(w, "ACCESS_TOKEN", accessToken, time.Second*time.Duration(config.Env.JWTExpirationTime))
	utils.WriteJwtToCookie(w, "REFRESH_TOKEN", refreshToken, time.Hour*time.Duration(24*30))

	utils.WriteJson(w, http.StatusOK, nil)
}

func (h *Handler) handleAdminLogin(w http.ResponseWriter, r *http.Request) {
	// get the json payload
	var payload types.LoginUserPayload
	if err := utils.ParseJson(r, &payload); err != nil {
		utils.WriteJsonError(w, http.StatusBadRequest, err)
		return
	}

	// validate the payload
	if err := utils.GetValidator().Struct(payload); err != nil {
		errors := err.(validator.ValidationErrors)
		utils.WriteJsonError(w, http.StatusBadRequest, fmt.Errorf("invalid payload %v", errors))
		return
	}

	// call user service admin login
	u, err := h.UserService.LoginAdminUser(r.Context(), &payload)
	if err != nil {
		utils.WriteJsonError(w, utils.GetHttpStatusCodeFromContext(r.Context()), err)
		return
	}

	// create a jwt tokens and insert in cookie
	accessToken, refreshToken, err := h.AuthService.CreateTokens(&types.TokenInput{
		Id:   u.Id,
		Type: "admin",
	})
	if err != nil {
		utils.WriteJsonError(w, http.StatusInternalServerError, err)
	}

	// add the access tokens to the response cookies
	utils.WriteJwtToCookie(w, "ACCESS_TOKEN", accessToken, time.Second*time.Duration(config.Env.JWTExpirationTime))
	utils.WriteJwtToCookie(w, "REFRESH_TOKEN", refreshToken, time.Hour*time.Duration(24*30))

	utils.WriteJson(w, http.StatusOK, nil)
}

func (h *Handler) handleRegister(w http.ResponseWriter, r *http.Request) {
	// get the json payload
	var payload types.RegisterUserPayload
	if err := utils.ParseJson(r, &payload); err != nil {
		utils.WriteJsonError(w, http.StatusBadRequest, err)
		return
	}

	// validate the payload
	if err := utils.GetValidator().Struct(payload); err != nil {
		errors := err.(validator.ValidationErrors)
		utils.WriteJsonError(w, http.StatusBadRequest, fmt.Errorf("invalid payload %v", errors))
		return
	}

	// call user service register
	u, err := h.UserService.RegisterStudentUser(r.Context(), &payload)
	if err != nil {
		utils.WriteJsonError(w, utils.GetHttpStatusCodeFromContext(r.Context()), err)
		return
	}

	// create a jwt access token and insert in cookie
	accessToken, refreshToken, err := h.AuthService.CreateTokens(&types.TokenInput{
		Id:   u.Id,
		Type: "student",
	})
	if err != nil {
		utils.WriteJsonError(w, http.StatusInternalServerError, err)
		return
	}
	utils.WriteJwtToCookie(w, "ACCESS_TOKEN", accessToken, time.Second*time.Duration(config.Env.JWTExpirationTime))
	utils.WriteJwtToCookie(w, "REFRESH_TOKEN", refreshToken, time.Hour*time.Duration(24*30))

	utils.WriteJson(w, http.StatusCreated, nil)
}

func (h *Handler) handleAdminRegister(w http.ResponseWriter, r *http.Request) {
	// Can be called only by admin user
	// Check is done in the middleware token check
	// TODO: Provide a way to register admin user using access-secret only avaiable to admins

	// get the json payload
	var payload types.RegisterUserPayload
	if err := utils.ParseJson(r, &payload); err != nil {
		utils.WriteJsonError(w, http.StatusBadRequest, err)
		return
	}

	// validate the payload
	if err := utils.GetValidator().Struct(payload); err != nil {
		errors := err.(validator.ValidationErrors)
		utils.WriteJsonError(w, http.StatusBadRequest, fmt.Errorf("invalid payload %v", errors))
		return
	}

	// call user service admin register
	u, err := h.UserService.RegisterAdminUser(r.Context(), &payload)
	if err != nil {
		utils.WriteJsonError(w, utils.GetHttpStatusCodeFromContext(r.Context()), err)
		return
	}

	// create a jwt access token and insert in cookie
	accessToken, refreshToken, err := h.AuthService.CreateTokens(&types.TokenInput{
		Id:   u.Id,
		Type: "admin",
	})
	if err != nil {
		utils.WriteJsonError(w, http.StatusInternalServerError, err)
		return
	}
	utils.WriteJwtToCookie(w, "ACCESS_TOKEN", accessToken, time.Second*time.Duration(config.Env.JWTExpirationTime))
	utils.WriteJwtToCookie(w, "REFRESH_TOKEN", refreshToken, time.Hour*time.Duration(24*30))

	utils.WriteJson(w, http.StatusCreated, nil)
}
