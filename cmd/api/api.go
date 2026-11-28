package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/SufyaanKhateeb/college-placement-app-api/service/access"
	"github.com/SufyaanKhateeb/college-placement-app-api/service/auth"
	"github.com/SufyaanKhateeb/college-placement-app-api/service/forms"
	"github.com/SufyaanKhateeb/college-placement-app-api/service/jobs"
	"github.com/SufyaanKhateeb/college-placement-app-api/service/user"
	"github.com/SufyaanKhateeb/college-placement-app-api/utils"
	"github.com/go-chi/cors"
	"github.com/jackc/pgx/v5/pgxpool"
)

func newHandler(db *pgxpool.Pool, frontendOrigin string, google *auth.GoogleSignIn, mailer access.InvitationMailer, emailCodeMailer func(context.Context, string, string) error) http.Handler {
	mux := http.NewServeMux()
	google.RegisterRoutes(mux)
	forms.RegisterRoutes(mux, db, &auth.AuthService{}, frontendOrigin)
	userStore := user.NewStore(db)
	userService := user.NewUserService(userStore)
	authService := &auth.AuthService{}
	jobs.RegisterRoutes(mux, db, authService, frontendOrigin)
	accessStore := access.Store{DB: db}
	users := user.NewHandler(userService, authService)
	users.Store = userStore
	users.EmailCodeMailer = emailCodeMailer
	users.RegisterRoutes(mux, "/api/v1", accessStore.Resolve, frontendOrigin)
	accessStore.RegisterRoutes(mux, authService, frontendOrigin, mailer)

	return cors.Handler(cors.Options{
		AllowedOrigins:   []string{frontendOrigin},
		AllowedMethods:   []string{"GET", "HEAD", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-CSRF-Token"},
		ExposedHeaders:   []string{"Link", "Location"},
		AllowCredentials: true,
		MaxAge:           300,
	})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1" || strings.HasPrefix(r.URL.Path, "/api/v1/") {
			if handler, pattern := mux.Handler(r); pattern == "" {
				handler.ServeHTTP(&routerErrorWriter{ResponseWriter: w}, r)
				return
			}
		}
		mux.ServeHTTP(w, r)
	}))
}

// Convert ServeMux's unmatched-route errors while retaining its Allow header
// and path normalization redirects.
type routerErrorWriter struct {
	http.ResponseWriter
	wroteError bool
}

func (w *routerErrorWriter) WriteHeader(status int) {
	if status == http.StatusNotFound || status == http.StatusMethodNotAllowed {
		w.wroteError = true
		// net/http sets plain-text headers before calling WriteHeader.
		w.Header().Del("Content-Length")
		utils.WriteJsonError(w.ResponseWriter, status, errors.New(http.StatusText(status)))
		return
	}
	w.ResponseWriter.WriteHeader(status)
}

func (w *routerErrorWriter) Write(p []byte) (int, error) {
	if w.wroteError {
		return len(p), nil
	}
	return w.ResponseWriter.Write(p)
}

func NewHandler(ctx context.Context, db *pgxpool.Pool, frontendOrigin string) (http.Handler, error) {
	sender, err := access.ReadGmailSender()
	if err != nil {
		return nil, err
	}
	return configuredHandler(ctx, db, frontendOrigin, sender.InvitationMailer(), sender.SendVerificationCode)
}

func configuredHandler(ctx context.Context, db *pgxpool.Pool, frontendOrigin string, mailer access.InvitationMailer, emailCodeMailer func(context.Context, string, string) error) (http.Handler, error) {
	google, err := auth.NewGoogleSignIn(ctx, user.NewStore(db), &auth.AuthService{}, frontendOrigin)
	if err != nil {
		return nil, err
	}
	return newHandler(db, frontendOrigin, google, mailer, emailCodeMailer), nil
}

func RunLocal(ctx context.Context, addr string, db *pgxpool.Pool, frontendOrigin string) error {
	handler, err := configuredHandler(ctx, db, frontendOrigin,
		func(_ context.Context, email, link string) error {
			slog.Info("Local admin invitation", "recipient", email, "link", link)
			return nil
		},
		func(_ context.Context, email, code string) error {
			slog.Info("Local verification email", "recipient", email, "code", code)
			return nil
		})
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
