package app

import (
	"context"
	"fmt"
	"net/http"
	"os"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/cors"
	"github.com/jackc/pgx/v5/pgxpool"
)

type App struct {
	addr string
	db   *pgxpool.Pool
	mux  http.Handler
}

func New(ctx context.Context) (*App, error) {
	// Load .env if present (no-op if missing).
	_ = loadDotenvUpward()

	port := getenvDefault("PORT", "8080")
	addr := ":" + port

	var db *pgxpool.Pool
	if dsn := os.Getenv("DATABASE_URL"); dsn != "" {
		pool, err := pgxpool.New(ctx, dsn)
		if err != nil {
			return nil, fmt.Errorf("db pool: %w", err)
		}
		if err := pool.Ping(ctx); err != nil {
			pool.Close()
			return nil, fmt.Errorf("db ping: %w", err)
		}
		db = pool
	}

	a := &App{addr: addr, db: db}

	r := chi.NewRouter()
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"http://localhost:5173"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type"},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	// Health & meta
	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("ok\n"))
	})

	// Auth stubs (real Discogs OAuth comes next)
	r.Route("/auth", func(r chi.Router) {
		r.Get("/discogs/start", a.handleDiscogsOAuthStart())
		r.Get("/discogs/callback", a.handleDiscogsOAuthCallback())
		r.Post("/logout", a.handleLogout())
	})

	// API stubs
	r.Route("/api", func(r chi.Router) {
		r.Get("/me", a.handleMe())
		r.Get("/albums", a.handleAlbums())
		r.Post("/albums/sync", a.handleAlbumsSync())
		r.Get("/labels", a.handleLabels())
		r.Post("/labels", a.handleCreateLabel())
		r.Post("/albums/{albumID}/labels", a.handleAddAlbumLabel())
		r.Delete("/albums/{albumID}/labels/{labelID}", a.handleRemoveAlbumLabel())
		r.Get("/spins", a.handleSpins())
		r.Post("/spins", a.handleCreateSpin())
		r.Delete("/spins/{spinID}", a.handleDeleteSpin())
	})

	a.mux = r
	return a, nil
}

func (a *App) Addr() string         { return a.addr }
func (a *App) Router() http.Handler { return a.mux }
func (a *App) DB() *pgxpool.Pool    { return a.db }

func getenvDefault(key, def string) string {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	return v
}

func notImplemented(msg string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusNotImplemented)
		_, _ = w.Write([]byte(fmt.Sprintf(`{"error":"%s"}`, msg)))
	}
}
