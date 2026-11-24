package config

import (
	"errors"
	"net/url"
	"os"
	"strings"

	"github.com/golang-jwt/jwt/v5"
)

var ErrInvalidDatabaseURL = errors.New("DATABASE_URL is invalid")

// RuntimeConfig reads the managed environment without opening local key files.
func RuntimeConfig() (Config, error) {
	database := os.Getenv("DATABASE_URL")
	if database == "" {
		return Config{}, errors.New("DATABASE_URL is required")
	}
	origin := strings.TrimSuffix(os.Getenv("FRONTEND_ORIGIN"), "/")
	address, err := url.Parse(origin)
	if err != nil || address.Scheme != "https" || address.Hostname() == "" ||
		address.Path != "" || address.User != nil || address.RawQuery != "" || address.ForceQuery || address.Fragment != "" {
		return Config{}, errors.New("FRONTEND_ORIGIN must be an HTTPS origin")
	}
	pem := os.Getenv("JWT_PRIVATE_KEY_PEM")
	if pem == "" {
		return Config{}, errors.New("JWT_PRIVATE_KEY_PEM is required")
	}
	key, err := jwt.ParseRSAPrivateKeyFromPEM([]byte(pem))
	if err != nil || key.N.BitLen() < 2048 {
		return Config{}, errors.New("JWT_PRIVATE_KEY_PEM must contain an RSA key of at least 2048 bits")
	}
	return Config{
		DbUrl:             database,
		FrontendOrigin:    origin,
		JWTExpirationTime: getEnvAsInt("JWT_EXPIRATION_TIME", 60*5),
		PrivateKey:        key,
		PublicKey:         &key.PublicKey,
	}, nil
}
