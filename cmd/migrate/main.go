package main

import (
	"errors"
	"flag"
	"log"
	"os"
	"strconv"
	"strings"

	"github.com/SufyaanKhateeb/college-placement-app-api/config"
	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/lpernett/godotenv"
)

func migrationDatabaseURL(managed bool) (string, error) {
	if managed {
		database := os.Getenv("MIGRATION_DATABASE_URL")
		if database == "" {
			return "", errors.New("-managed requires MIGRATION_DATABASE_URL in the environment")
		}
		return database, nil
	}
	return config.LocalDatabaseURL()
}

func main() {
	managed := flag.Bool("managed", false, "use the explicitly supplied managed migration database")
	flag.Parse()
	args := flag.Args()
	if len(args) == 0 || (args[0] != "up" && args[0] != "down" && args[0] != "down-one" && args[0] != "force") {
		log.Fatal("usage: migrate [-managed] up|down|down-one|force <version>")
	}
	if !*managed {
		godotenv.Load(".env.local")
	}
	database, err := migrationDatabaseURL(*managed)
	if err != nil {
		log.Fatal(err)
	}
	m, err := migrate.New("file://cmd/migrate/migrations", database)
	if err != nil {
		log.Fatal(strings.ReplaceAll(err.Error(), database, "[database URL redacted]"))
	}

	switch args[0] {
	case "up":
		err = m.Up()
	case "down-one":
		err = m.Steps(-1)
	case "down":
		err = m.Down()
	case "force":
		if len(args) != 2 {
			log.Fatal("force requires a migration version")
		}
		version, parseErr := strconv.Atoi(args[1])
		if parseErr != nil {
			log.Fatal("migration version must be a number")
		}
		err = m.Force(version)
	}
	if err != nil && !errors.Is(err, migrate.ErrNoChange) {
		log.Fatal(err)
	}
}
