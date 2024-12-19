package types

import (
	"context"
	"time"

	"github.com/golang-jwt/jwt/v5"
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
	GetStudentUserById(ctx context.Context, uid int) (*User, error)
	GetAdminUserById(ctx context.Context, uid int) (*AdminUser, error)
	GetStudentUserByEmail(ctx context.Context, emailId string) (*User, error)
	GetAdminUserByEmail(ctx context.Context, emailId string) (*AdminUser, error)
	LoginStudentUser(ctx context.Context, payload *LoginUserPayload) (*User, error)
	LoginAdminUser(ctx context.Context, payload *LoginUserPayload) (*AdminUser, error)
	RegisterStudentUser(ctx context.Context, payload *RegisterUserPayload) (*User, error)
	RegisterAdminUser(ctx context.Context, payload *RegisterUserPayload) (*AdminUser, error)
}

type AuthService interface {
	SignJwt(expirationTime time.Duration, claims CustomClaims) (string, error)
	VerifyToken(tkn string) (*jwt.Token, error)
	CreateTokens(tokenInput *TokenInput) (string, string, error)
}

type AuthStore interface{}

type LoginUserPayload struct {
	Email    string `json:"email" validate:"required,email"`
	Password string `json:"password" validate:"required"`
	UserType string `json:"userType"`
}

type RegisterUserPayload struct {
	FirstName string `json:"firstName" validate:"required"`
	LastName  string `json:"lastName" validate:"required"`
	Email     string `json:"email" validate:"required,email"`
	Password  string `json:"password" validate:"required,min=8,max=130,password"`
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
	UType     string `json:"uType"`
	FirstName string `json:"firstName"`
	LastName  string `json:"lastName"`
	Email     string `json:"email"`
	Verified  bool   `json:"verified"`
}

type httpStatusCodeKeyType string

const HttpStatusCodeKey httpStatusCodeKeyType = "httpStatusCode"
