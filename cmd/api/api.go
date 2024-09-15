package api

import (
	"log"
	"net/http"

	"github.com/SufyaanKhateeb/college-placement-app-api/service/auth"
	"github.com/SufyaanKhateeb/college-placement-app-api/service/user"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"
)

type APIServer struct {
	addr string
	db   *pgxpool.Pool
}

func NewAPIServer(addr string, db *pgxpool.Pool) *APIServer {
	return &APIServer{
		addr: addr,
		db:   db,
	}
}

func (s *APIServer) Run() error {
	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	subRouter := chi.NewRouter()

	userStore := user.NewStore(s.db)
	authStore := auth.NewAuthStore(s.db)
	authService := auth.NewAuthService(*authStore)
	userHandler := user.NewHandler(userStore, authService)
	userHandler.RegisterRoutes(subRouter)

	r.Mount("/api/v1", subRouter)

	log.Println("Listening on", s.addr)

	return http.ListenAndServe(s.addr, r)
}
