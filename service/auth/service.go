package auth

import (
	"fmt"
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

const Issuer = "placement-app-server"
const Audience = "placement-app-client"

func (a *AuthService) SignJwt(expirationTime time.Duration, claims types.CustomClaims) (string, error) {
	claims.IssuedAt = &jwt.NumericDate{Time: time.Now()}
	claims.ExpiresAt = &jwt.NumericDate{Time: time.Now().Add(expirationTime)}
	claims.Issuer = Issuer
	claims.Audience = jwt.ClaimStrings{Audience}

	tkn := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	return tkn.SignedString(config.Env.PrivateKey)
}

func (a *AuthService) VerifyToken(tkn string) (*jwt.Token, error) {
	token, err := jwt.ParseWithClaims(tkn, &types.CustomClaims{}, func(token *jwt.Token) (interface{}, error) {
		// validate the alg
		if _, ok := token.Method.(*jwt.SigningMethodRSA); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}

		return config.Env.PublicKey, nil
	}, jwt.WithIssuer(Issuer), jwt.WithAudience(Audience))

	return token, err
}
