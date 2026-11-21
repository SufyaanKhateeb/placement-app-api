package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/SufyaanKhateeb/college-placement-app-api/service/auth"
	"github.com/SufyaanKhateeb/college-placement-app-api/service/user"
	"github.com/go-chi/cors"
	"github.com/jackc/pgx/v5/pgxpool"
)

func newHandler(db *pgxpool.Pool, frontendOrigin string) http.Handler {
	mux := http.NewServeMux()
	userService := user.NewUserService(user.NewStore(db))
	authService := &auth.AuthService{}
	user.NewHandler(userService, authService).RegisterRoutes(mux, "/api/v1")

	return cors.Handler(cors.Options{
		AllowedOrigins:   []string{frontendOrigin},
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-CSRF-Token"},
		ExposedHeaders:   []string{"Link"},
		AllowCredentials: true,
		MaxAge:           300,
	})(mux)
}

func Run(ctx context.Context, addr string, db *pgxpool.Pool, frontendOrigin string) error {
	server := &http.Server{
		Addr:              addr,
		Handler:           newHandler(db, frontendOrigin),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	stopped := make(chan error, 1)
	go func() {
		slog.Info("Starting HTTP server", "address", addr)
		stopped <- server.ListenAndServe()
	}()

	select {
	case err := <-stopped:
		return err
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdown); err != nil {
			return err
		}
		err := <-stopped
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}
