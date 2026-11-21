package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/SufyaanKhateeb/college-placement-app-api/cmd/api"
	"github.com/SufyaanKhateeb/college-placement-app-api/config"
	"github.com/SufyaanKhateeb/college-placement-app-api/db"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx); err != nil {
		slog.Error("API stopped", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	config.InitConfig()
	pool, err := db.NewDbPool(config.Env.DbUrl)
	if err != nil {
		return fmt.Errorf("initialize database pool: %w", err)
	}
	defer pool.Close()

	startup, cancel := context.WithTimeout(ctx, 5*time.Second)
	err = pool.Ping(startup)
	cancel()
	if err != nil {
		return fmt.Errorf("connect to database: %w", err)
	}
	slog.Info("Connected to PostgreSQL")
	return api.Run(ctx, ":"+config.Env.Port, pool, config.Env.FrontendOrigin)
}
