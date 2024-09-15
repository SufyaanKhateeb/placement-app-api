package config

import (
	"crypto/rsa"
	"log"
	"os"
	"strconv"

	"github.com/golang-jwt/jwt/v5"
	"github.com/lpernett/godotenv"
)

type Config struct {
	DbUrl             string
	Port              string
	JWTExpirationTime int64
	PrivateKey        *rsa.PrivateKey
	PublicKey         *rsa.PublicKey
}

var Env Config = Config{}

func InitConfig() {
	godotenv.Load()

	Env = Config{
		DbUrl:             getEnv("DATABASE_URL", "postgres://postgres:postgres@localhost:5432/postgres?sslmode=disable"),
		Port:              getEnv("PORT", "8090"),
		JWTExpirationTime: getEnvAsInt("JWT_EXPIRATION_TIME", 60*5),
		PrivateKey:        loadPrivateKey(getEnv("PRIVATE_KEY_PATH", "./private.key")),
		PublicKey:         loadPublicKey(getEnv("PUBLIC_KEY_PATH", "./public.key")),
	}
}

func InitConfigWith(config Config) {
	Env = config
}

func getRequiredEnv(key string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	log.Fatalf("no %s env variable found", key)
	return ""
}

func getEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}

func getEnvAsInt(key string, fallback int64) int64 {
	if value, ok := os.LookupEnv(key); ok {
		i, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			return fallback
		}
		return i
	}
	return fallback
}

func loadPrivateKey(path string) *rsa.PrivateKey {
	data, err := os.ReadFile(path)
	if err != nil {
		log.Fatal(err)
	}
	key, err := jwt.ParseRSAPrivateKeyFromPEM(data)
	if err != nil {
		log.Fatal(err)
	}
	return key
}

func loadPublicKey(path string) *rsa.PublicKey {
	data, err := os.ReadFile(path)
	if err != nil {
		log.Fatal(err)
	}
	key, err := jwt.ParseRSAPublicKeyFromPEM(data)
	if err != nil {
		log.Fatal(err)
	}
	return key
}
