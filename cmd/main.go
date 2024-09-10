package main

import (
	"log"

	"github.com/SufyaanKhateeb/college-placement-app-api/cmd/api"
	"github.com/SufyaanKhateeb/college-placement-app-api/config"
	"github.com/SufyaanKhateeb/college-placement-app-api/db"
)

func main() {
	dbpool, err := db.NewDbPool(config.Env.DbUrl)

	if err != nil {
		log.Fatal(err)
	}

	defer dbpool.Close()

	server := api.NewAPIServer(":8090", dbpool)
	if err := server.Run(); err != nil {
		log.Fatal(err)
	}
}
