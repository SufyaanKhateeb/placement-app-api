package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/SufyaanKhateeb/college-placement-app-api/service/access"
	"github.com/SufyaanKhateeb/college-placement-app-api/service/auth"
	"github.com/SufyaanKhateeb/college-placement-app-api/service/forms"
	"github.com/SufyaanKhateeb/college-placement-app-api/service/user"
	"github.com/go-chi/cors"
	"github.com/jackc/pgx/v5/pgxpool"
)

func newHandler(db *pgxpool.Pool, frontendOrigin string, google *auth.GoogleSignIn, mailer access.InvitationMailer) http.Handler {
	mux := http.NewServeMux()
	google.RegisterRoutes(mux)
	forms.RegisterRoutes(mux, db, &auth.AuthService{}, frontendOrigin)
	userStore := user.NewStore(db)
	userService := user.NewUserService(userStore)
	authService := &auth.AuthService{}
	accessStore := access.Store{DB: db}
	users := user.NewHandler(userService, authService)
	users.Store = userStore
	users.RegisterRoutes(mux, "/api/v1", accessStore.Resolve, frontendOrigin)
	accessStore.RegisterRoutes(mux, authService, frontendOrigin, mailer)

	return cors.Handler(cors.Options{
		AllowedOrigins:   []string{frontendOrigin},
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-CSRF-Token"},
		ExposedHeaders:   []string{"Link"},
		AllowCredentials: true,
		MaxAge:           300,
	})(mux)
}

func NewHandler(ctx context.Context, db *pgxpool.Pool, frontendOrigin string) (http.Handler, error) {
	mailer, err := access.ReadInvitationMailer()
	if err != nil {
		return nil, err
	}
	google, err := auth.NewGoogleSignIn(ctx, user.NewStore(db), &auth.AuthService{}, frontendOrigin)
	if err != nil {
		return nil, err
	}
	return newHandler(db, frontendOrigin, google, mailer), nil
}

func Run(ctx context.Context, addr string, db *pgxpool.Pool, frontendOrigin string) error {
	handler, err := NewHandler(ctx, db, frontendOrigin)
	if err != nil {
		return err
	}
	server := &http.Server{
		Addr:              addr,
		Handler:           handler,
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
