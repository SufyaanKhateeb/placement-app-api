package types

import (
	"context"
	"time"

	"github.com/SufyaanKhateeb/college-placement-app-api/service"
	"github.com/golang-jwt/jwt/v5"
)

const (
	RoleUser          = "user"
	RoleAdmin         = "admin"
	LegacyRoleStudent = "student"
)

type UserStore interface {
	CheckUserWithEmailExists(email string) (bool, error)
	CheckAdminUserWithEmailExists(email string) (bool, error)
	GetUserByEmail(email string) (*User, error)
	GetAdminUserByEmail(email string) (*AdminUser, error)
	GetUserById(id int) (*User, error)
	GetAdminById(id int) (*AdminUser, error)
	CreateUser(User) (int, error)
	CreateAdminUser(AdminUser) (int, error)
}

type UserService interface {
	GetUserById(ctx context.Context, uid int) (*User, *service.ServiceError)
	GetAdminUserById(ctx context.Context, uid int) (*AdminUser, *service.ServiceError)
	GetUserByEmail(ctx context.Context, emailId string) (*User, *service.ServiceError)
	GetAdminUserByEmail(ctx context.Context, emailId string) (*AdminUser, *service.ServiceError)
	LoginUser(ctx context.Context, payload *LoginUserPayload) (*User, *service.ServiceError)
	LoginAdminUser(ctx context.Context, payload *LoginUserPayload) (*AdminUser, *service.ServiceError)
	RegisterUser(ctx context.Context, payload *RegisterUserPayload) (*User, *service.ServiceError)
	RegisterAdminUser(ctx context.Context, payload *RegisterUserPayload) (*AdminUser, *service.ServiceError)
}

type AuthService interface {
	SignJwt(expirationTime time.Duration, claims CustomClaims) (string, error)
	VerifyToken(tkn string) (*jwt.Token, error)
	CreateTokens(tokenInput *TokenInput) (string, string, error)
}

type LoginUserPayload struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	UserType string `json:"userType"`
}

type RegisterUserPayload struct {
	FirstName string `json:"firstName"`
	LastName  string `json:"lastName"`
	Email     string `json:"email"`
	Password  string `json:"password"`
}

type User struct {
	Id        int       `json:"id"`
	FirstName string    `json:"firstName"`
	LastName  string    `json:"lastName"`
	Email     string    `json:"email"`
	Password  string    `json:"-"`
	Verified  bool      `json:"verified"`
	CreatedAt time.Time `json:"createdAt"`
}

type AdminUser struct {
	Id        int       `json:"id"`
	FirstName string    `json:"firstName"`
	LastName  string    `json:"lastName"`
	Email     string    `json:"email"`
	Password  string    `json:"-"`
	Role      string    `json:"role"`
	CreatedAt time.Time `json:"createdAt"`
}

type TokenInput struct {
	Id        int
	Type      string
	TokenData string
}

type CustomClaims struct {
	Id        int    `json:"id"`
	Type      string `json:"type"`
	TokenData string `json:"tokenData"`
	jwt.RegisteredClaims
}

type UserDto struct {
	Id        int    `json:"id"`
	Role      string `json:"role"`
	UType     string `json:"uType"`
	FirstName string `json:"firstName"`
	LastName  string `json:"lastName"`
	Email     string `json:"email"`
	Verified  bool   `json:"verified"`
}
