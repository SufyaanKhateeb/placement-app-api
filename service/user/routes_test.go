package user

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/SufyaanKhateeb/college-placement-app-api/types"
	"github.com/go-chi/chi/v5"
	"github.com/golang-jwt/jwt/v5"
)

func TestUserServiceHandlers(t *testing.T) {
	userStore := &mockUserStore{UserExists: true}
	handler := NewHandler(userStore, &mockAuthService{})

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

		if rr.Code != http.StatusCreated {
			t.Errorf("expected status code %d, got %d", http.StatusBadRequest, rr.Code)
		}
	})
}

type mockAuthService struct{}

func (a *mockAuthService) SignJwt(expirationTime time.Duration, claims types.CustomClaims) (string, error) {
	return "", nil
}

func (a *mockAuthService) VerifyToken(tkn string) (*jwt.Token, error) {
	return &jwt.Token{}, nil
}

type mockUserStore struct {
	UserExists bool
}

func (s *mockUserStore) GetUserByEmail(email string) (*types.User, error) {
	if s.UserExists {
		return &types.User{}, nil
	}
	return nil, fmt.Errorf("user not found")
}

func (s *mockUserStore) GetUserById(id int) (*types.User, error) {
	if s.UserExists {
		return &types.User{}, nil
	}
	return nil, fmt.Errorf("user not found")
}

func (s *mockUserStore) CreateUser(u types.User) (int, error) {
	return 0, nil
}
