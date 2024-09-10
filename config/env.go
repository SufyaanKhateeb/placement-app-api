package config

import (
	"os"

	"github.com/lpernett/godotenv"
)

type Config struct {
	DbUrl  string
	Port string
}

var Env Config = initConfig()

func initConfig() Config {
	godotenv.Load()

	return Config{
		DbUrl:  getEnv("DATABASE_URL", "postgres://postgres:postgres@localhost:5432/postgres?sslmode=disable"),
		Port: getEnv("PORT", "8090"),
	}
}

func getEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}