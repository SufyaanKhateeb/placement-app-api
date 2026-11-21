package main

import (
	"log"
	"os"
	"strconv"
	"strings"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/lpernett/godotenv"
)

func getEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}

func main() {
	godotenv.Load(".env.local")
	godotenv.Load()
	databaseURL := os.Getenv("MIGRATION_DATABASE_URL")
	if databaseURL == "" {
		databaseURL = getEnv("DATABASE_URL_UNPOOLED", getEnv("DATABASE_URL", "postgres://postgres:postgres@localhost:5432/postgres?sslmode=disable"))
	}
	m, err := migrate.New("file://cmd/migrate/migrations", databaseURL)
	if err != nil {
		log.Fatal(strings.ReplaceAll(err.Error(), databaseURL, "[database URL redacted]"))
	}

	cmd := os.Args[(len(os.Args) - 1)]
	if cmd == "up" {
		if err := m.Up(); err != nil && err != migrate.ErrNoChange {
			log.Fatal(err)
		}
	}
	if cmd == "down-one" {
		if err := m.Steps(-1); err != nil && err != migrate.ErrNoChange {
			log.Fatal(err)
		}
	}
	if cmd == "down" {
		if err := m.Down(); err != nil && err != migrate.ErrNoChange {
			log.Fatal(err)
		}
	}

	if cmd == "force" {
		versionStr := os.Args[(len(os.Args) - 2)]
		version, err := strconv.Atoi(versionStr)
		if err != nil {
			log.Fatal("Version provided is not a number")
		}
		if err := m.Force(version); err != nil && err != migrate.ErrNoChange {
			log.Fatal(err)
		}
	}
}
