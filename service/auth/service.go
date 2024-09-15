package auth

import (
	"time"

	"github.com/SufyaanKhateeb/college-placement-app-api/config"
	"github.com/SufyaanKhateeb/college-placement-app-api/types"
	"github.com/golang-jwt/jwt/v5"
)

type AuthService struct {
	Store types.AuthStore
}

func NewAuthService(store types.AuthStore) *AuthService {
	return &AuthService{
		Store: store,
	}
}

func (a *AuthService) SignJwt(expirationTime time.Duration, claims jwt.MapClaims) (string, error) {
	mapClaims := jwt.MapClaims{}

	for k, v := range claims {
		mapClaims[k] = v
	}

	mapClaims["iat"] = time.Now().Unix()
	mapClaims["exp"] = time.Now().Add(expirationTime).Unix()

	tkn := jwt.NewWithClaims(jwt.SigningMethodRS256, mapClaims)
	return tkn.SignedString(config.Env.PrivateKey)
}
