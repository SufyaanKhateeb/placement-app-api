package config

import (
	"crypto/rand"
	"crypto/rsa"
	"errors"
	"net"
	"net/url"
	"os"
	"strconv"

	"github.com/lpernett/godotenv"
)

type Config struct {
	DbUrl             string
	Port              string
	FrontendOrigin    string
	JWTExpirationTime int64
	PrivateKey        *rsa.PrivateKey
	PublicKey         *rsa.PublicKey
}

var Env Config

const defaultLocalDatabaseURL = "postgres://postgres:postgres@127.0.0.1:5432/placement_dev?sslmode=disable"

func LocalDatabaseURL() (string, error) {
	database := getEnv("LOCAL_DATABASE_URL", defaultLocalDatabaseURL)
	address, err := url.Parse(database)
	if err != nil || (address.Scheme != "postgres" && address.Scheme != "postgresql") ||
		!loopbackHost(address.Hostname()) || address.Path == "" || address.Path == "/" || address.Fragment != "" {
		return "", errors.New("LOCAL_DATABASE_URL must be a PostgreSQL URL with a loopback host and a database name")
	}

	options, err := url.ParseQuery(address.RawQuery)
	if err != nil {
		return "", errors.New("LOCAL_DATABASE_URL has invalid query options")
	}
	for name, values := range options {
		// Connection options such as host and service can override the URL host.
		if name != "sslmode" || len(values) != 1 {
			return "", errors.New("LOCAL_DATABASE_URL only accepts the sslmode query option")
		}
	}
	return database, nil
}

func loopbackHost(host string) bool {
	return host == "localhost" || net.ParseIP(host).IsLoopback()
}

func InitConfig() error {
	godotenv.Load(".env.local")
	database, err := LocalDatabaseURL()
	if err != nil {
		return err
	}
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return err
	}
	Env = Config{
		DbUrl:             database,
		Port:              "8090",
		FrontendOrigin:    "http://localhost:3000",
		JWTExpirationTime: getEnvAsInt("JWT_EXPIRATION_TIME", 60*5),
		PrivateKey:        key,
		PublicKey:         &key.PublicKey,
	}
	return nil
}

func getEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok && value != "" {
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
