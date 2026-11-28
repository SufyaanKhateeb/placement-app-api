package handler

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"sync"

	server "github.com/SufyaanKhateeb/college-placement-app-api/cmd/api"
	"github.com/SufyaanKhateeb/college-placement-app-api/config"
	"github.com/SufyaanKhateeb/college-placement-app-api/utils"
	"github.com/jackc/pgx/v5/pgxpool"
)

const routeParam = "__placement_route"

var (
	initialize sync.Once
	app        http.Handler
	initErr    error
)

// Handler adapts the existing router to Vercel's Go runtime.
func Handler(w http.ResponseWriter, r *http.Request) {
	request, ok := apiRequest(r)
	if !ok {
		http.NotFound(w, r)
		return
	}

	initialize.Do(func() {
		app, initErr = newApp()
		if initErr != nil {
			slog.Error("Vercel API could not initialize", "error", initErr)
		}
	})
	if initErr != nil {
		utils.WriteJsonError(w, http.StatusServiceUnavailable, errors.New("API unavailable"))
		return
	}
	w.Header().Set("Cache-Control", "private, no-store")
	app.ServeHTTP(w, request)
}

func newApp() (http.Handler, error) {
	settings, err := config.RuntimeConfig()
	if err != nil {
		return nil, err
	}
	poolSettings, err := pgxpool.ParseConfig(settings.DbUrl)
	if err != nil {
		// A pgx parse error can contain credentials from the connection string.
		return nil, config.ErrInvalidDatabaseURL
	}
	if poolSettings.MaxConns > 2 {
		poolSettings.MaxConns = 2
	}
	poolSettings.MinConns = 0
	ctx := context.Background()
	pool, err := pgxpool.NewWithConfig(ctx, poolSettings)
	if err != nil {
		return nil, config.ErrInvalidDatabaseURL
	}
	config.Env = settings
	router, err := server.NewHandler(ctx, pool, settings.FrontendOrigin)
	if err != nil {
		pool.Close()
	}
	return router, err
}

func apiRequest(r *http.Request) (*http.Request, bool) {
	query := r.URL.Query()
	route := r.URL.Path
	if !isAPIPath(route) {
		if route != "/api/index" && route != "/api/index.go" {
			return nil, false
		}
		target := query[routeParam]
		if len(target) != 1 || !isAPIPath("/api/"+target[0]) {
			return nil, false
		}
		route = "/api/" + target[0]
	}

	request := r.Clone(r.Context())
	query.Del(routeParam)
	if route != r.URL.Path {
		request.URL.Path = route
		request.URL.RawPath = ""
	}
	request.URL.RawQuery = query.Encode()
	request.RequestURI = request.URL.RequestURI()
	return request, true
}

func isAPIPath(path string) bool {
	return path == "/api/v1" || strings.HasPrefix(path, "/api/v1/")
}
