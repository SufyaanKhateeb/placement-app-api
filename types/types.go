package types

import (
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type UserStore interface {
	CheckUserWithEmailExits(email string) (bool, error)
	GetUserByEmail(email string) (*User, error)
	GetUserById(id int) (*User, error)
	CreateUser(User) (int, error)
}

type AuthService interface {
	SignJwt(expirationTime time.Duration, claims CustomClaims) (string, error)
	VerifyToken(tkn string) (*jwt.Token, error)
}

type AuthStore interface{}

type LoginUserPayload struct {
	Email    string `json:"email" validate:"required,email"`
	Password string `json:"password" validate:"required"`
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
	CreatedAt time.Time `json:"createdAt"`
}

type CustomClaims struct {
	Uid   int    `json:"uid"`
	UType string `json:"uType"`
	jwt.RegisteredClaims
}

type UserDto struct {
	Id        int    `json:"id"`
	UType     string `json:"uType"`
	FirstName string `json:"firstName"`
	LastName  string `json:"lastName"`
	Email     string `json:"email"`
}
