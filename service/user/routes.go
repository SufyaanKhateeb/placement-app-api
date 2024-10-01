package user

import (
	"fmt"
	"net/http"
	"time"

	"github.com/SufyaanKhateeb/college-placement-app-api/config"
	"github.com/SufyaanKhateeb/college-placement-app-api/middlewares"
	"github.com/SufyaanKhateeb/college-placement-app-api/service/auth"
	"github.com/SufyaanKhateeb/college-placement-app-api/types"
	"github.com/SufyaanKhateeb/college-placement-app-api/utils"
	"github.com/go-chi/chi/v5"
	"github.com/go-playground/validator/v10"
)

type Handler struct {
	Store       types.UserStore
	AuthService types.AuthService
}

func NewHandler(s types.UserStore, authService types.AuthService) *Handler {
	return &Handler{
		Store:       s,
		AuthService: authService,
	}
}

func (h *Handler) RegisterRoutes(r *chi.Mux) {
	// Public Routes
	r.Group(func(r chi.Router) {
		r.Post("/login", h.handleLogin)
		r.Post("/register", h.handleRegister)
	})

	// Private Routes
	// Require Authentication
	r.Group(func(r chi.Router) {
		r.Use(middlewares.AuthMiddleware(h.AuthService), middlewares.RequireUser)
		r.Post("/refresh", h.handleRefresh)
		r.Get("/user", h.getUser)
	})
}

func (h *Handler) getUser(w http.ResponseWriter, r *http.Request) {
	ctxUser := r.Context().Value("user").(types.UserDto)
	u, err := h.Store.GetUserById(ctxUser.Id)
	if err != nil {
		utils.WriteJsonError(w, http.StatusBadRequest, fmt.Errorf("invalid user, user not found"))
		return
	}

	ctxUser.Email = u.Email
	ctxUser.FirstName = u.FirstName
	ctxUser.LastName = u.LastName

	utils.WriteJson(w, http.StatusOK, ctxUser)
}

func (h *Handler) handleRefresh(w http.ResponseWriter, r *http.Request) {
	ctxUser := r.Context().Value("user").(types.UserDto)
	utils.WriteJson(w, http.StatusOK, ctxUser)
}

func (h *Handler) handleLogin(w http.ResponseWriter, r *http.Request) {
	// get the json payload
	var payload types.LoginUserPayload
	if err := utils.ParseJson(r, &payload); err != nil {
		utils.WriteJsonError(w, http.StatusBadRequest, err)
		return
	}

	if err := utils.GetValidator().Struct(payload); err != nil {
		errors := err.(validator.ValidationErrors)
		utils.WriteJsonError(w, http.StatusBadRequest, fmt.Errorf("invalid payload %v", errors))
		return
	}

	// get the user using the email
	u, err := h.Store.GetUserByEmail(payload.Email)
	if err != nil {
		utils.WriteJsonError(w, http.StatusBadRequest, fmt.Errorf("not found, invalid email or password"))
		return
	}

	// check if password matches hash
	if err = auth.CompareHashAndPassword(payload.Password, u.Password); err != nil {
		utils.WriteJsonError(w, http.StatusBadRequest, fmt.Errorf("not found, invalid email or password"))
		return
	}

	// create a jwt tokens and insert in cookie
	accessToken, refreshToken, err := createTokens(h.AuthService, u)
	if err != nil {
		utils.WriteJsonError(w, http.StatusInternalServerError, err)
	}
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

	// check if the user exists
	exists, err := h.Store.CheckUserWithEmailExits(payload.Email)
	if err != nil {
		utils.WriteJsonError(w, http.StatusInternalServerError, err)
		return
	}
	if exists {
		utils.WriteJsonError(w, http.StatusInternalServerError, fmt.Errorf("user with email %s already exists", payload.Email))
		return
	}

	// create a new user
	hashedPassword, err := auth.HashPassword(payload.Password)

	if err != nil {
		utils.WriteJsonError(w, http.StatusInternalServerError, err)
		return
	}

	id, err := h.Store.CreateUser(types.User{
		FirstName: payload.FirstName,
		LastName:  payload.LastName,
		Email:     payload.Email,
		Password:  hashedPassword,
	})
	if err != nil {
		utils.WriteJsonError(w, http.StatusInternalServerError, err)
		return
	}

	// create a jwt access token and insert in cookie
	accessToken, refreshToken, err := createTokens(h.AuthService, &types.User{
		Id:        id,
		FirstName: payload.FirstName,
		LastName:  payload.LastName,
		Email:     payload.Email,
	})
	if err != nil {
		utils.WriteJsonError(w, http.StatusInternalServerError, err)
		return
	}
	utils.WriteJwtToCookie(w, "ACCESS_TOKEN", accessToken, time.Second*time.Duration(config.Env.JWTExpirationTime))
	utils.WriteJwtToCookie(w, "REFRESH_TOKEN", refreshToken, time.Hour*time.Duration(24*30))

	utils.WriteJson(w, http.StatusCreated, nil)
}

func createTokens(authService types.AuthService, u *types.User) (string, string, error) {
	expirationTime := time.Second * time.Duration(config.Env.JWTExpirationTime)
	accessToken, err := authService.SignJwt(expirationTime, types.CustomClaims{
		Uid:   u.Id,
		UType: "",
	})
	if err != nil {
		return "", "", nil
	}

	expirationTime = time.Hour * time.Duration(24*30)
	refreshToken, err := authService.SignJwt(expirationTime, types.CustomClaims{
		Uid:   u.Id,
		UType: "",
	})
	if err != nil {
		return "", "", nil
	}

	return accessToken, refreshToken, nil
}
