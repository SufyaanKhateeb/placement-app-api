package config

import (
	"crypto/rand"
	"crypto/rsa"
	"errors"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"

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
	database := os.Getenv("LOCAL_DATABASE_URL")
	neonHost := strings.ToLower(strings.TrimSpace(os.Getenv("NEON_DEV_HOST")))
	if database == "" {
		if neonHost != "" {
			return "", errors.New("NEON_DEV_HOST requires LOCAL_DATABASE_URL")
		}
		database = defaultLocalDatabaseURL
	}

	address, err := url.Parse(database)
	if err != nil || (address.Scheme != "postgres" && address.Scheme != "postgresql") ||
		address.Hostname() == "" || address.Path == "" || address.Path == "/" || address.Fragment != "" {
		return "", errors.New("LOCAL_DATABASE_URL must be a PostgreSQL URL with a host and database name")
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
	host := strings.ToLower(address.Hostname())
	if neonHost == "" {
		if !loopbackHost(host) {
			return "", errors.New("remote LOCAL_DATABASE_URL requires a pinned NEON_DEV_HOST")
		}
		return database, nil
	}
	if host != neonHost || !strings.HasSuffix(host, ".neon.tech") || host != directNeonHost(host) ||
		(address.Port() != "" && address.Port() != "5432") || options.Get("sslmode") != "verify-full" {
		return "", errors.New("LOCAL_DATABASE_URL must use the pinned direct NEON_DEV_HOST with sslmode=verify-full")
	}
	for _, key := range []string{"DATABASE_URL", "DATABASE_URL_UNPOOLED", "MIGRATION_DATABASE_URL"} {
		managed, err := url.Parse(os.Getenv(key))
		if err == nil && directNeonHost(strings.ToLower(managed.Hostname())) == host {
			return "", errors.New("NEON_DEV_HOST matches a managed database endpoint")
		}
	}
	return database, nil
}

// Neon uses the same endpoint name for direct and pooled connections.
func directNeonHost(host string) string {
	endpoint, domain, found := strings.Cut(host, ".")
	if !found {
		return host
	}
	return strings.TrimSuffix(endpoint, "-pooler") + "." + domain
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
