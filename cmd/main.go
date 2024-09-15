package main

import (
	"context"
	"log"

	"github.com/SufyaanKhateeb/college-placement-app-api/cmd/api"
	"github.com/SufyaanKhateeb/college-placement-app-api/config"
	"github.com/SufyaanKhateeb/college-placement-app-api/db"
)

func main() {
	config.InitConfig()
	dbpool, err := db.NewDbPool(config.Env.DbUrl)

	if err != nil {
		log.Fatal(err)
	} else {
		err := dbpool.Ping(context.Background())
		if err != nil {
			log.Fatal(err)
		}
		log.Println("Successfully connected to database")
	}

	defer dbpool.Close()

	server := api.NewAPIServer(":"+config.Env.Port, dbpool)
	if err := server.Run(); err != nil {
		log.Fatal(err)
	}
}
