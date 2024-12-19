package user

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/SufyaanKhateeb/college-placement-app-api/config"
	"github.com/SufyaanKhateeb/college-placement-app-api/service/auth"
	"github.com/SufyaanKhateeb/college-placement-app-api/service/user/mocks"
	"github.com/SufyaanKhateeb/college-placement-app-api/types"
	"github.com/go-chi/chi/v5"
)

func TestUserServiceHandlers(t *testing.T) {
	userStore := mocks.MockUserStore{UserExists: true}
	userService := NewUserService(&userStore)
	authService := auth.NewAuthService(&mocks.MockAuthStore{})
	handler := NewHandler(userService, authService)
	config.InitConfigWith("../../test.env")

	t.Run("should fail if the user payload is invalid", func(t *testing.T) {
		payload := types.RegisterUserPayload{
			FirstName: "fname",
			LastName:  "lname",
			Email:     "invalidEmail",
			Password:  "pass",
		}
		marshalled, _ := json.Marshal(payload)
		req, err := http.NewRequest(http.MethodPost, "/register", bytes.NewBuffer(marshalled))
		if err != nil {
			t.Fatal(err)
		}

		rr := httptest.NewRecorder()
		router := chi.NewRouter()

		router.Post("/register", handler.handleRegister)
		router.ServeHTTP(rr, req)

		if rr.Code != http.StatusBadRequest {
			t.Errorf("expected status code %d, got %d", http.StatusBadRequest, rr.Code)
		}
	})

	userStore.UserExists = false
	t.Run("should create user for valid payload", func(t *testing.T) {
		payload := types.RegisterUserPayload{
			FirstName: "fname",
			LastName:  "lname",
			Email:     "valid@email.com",
			Password:  "pass@123",
		}
		marshalled, _ := json.Marshal(payload)
		req, err := http.NewRequest(http.MethodPost, "/register", bytes.NewBuffer(marshalled))
		if err != nil {
			t.Fatal(err)
		}

		rr := httptest.NewRecorder()
		router := chi.NewRouter()

		router.Post("/register", handler.handleRegister)
		router.ServeHTTP(rr, req)

		if rr.Code != http.StatusCreated {
			t.Errorf("expected status code %d, got %d", http.StatusCreated, rr.Code)
		}
	})
}
