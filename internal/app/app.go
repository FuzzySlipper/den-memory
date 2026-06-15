// Package app wires the Den Memories service dependencies.
package app

import (
	"net/http"

	"den-memories/internal/contracts"
	"den-memories/internal/httpapi"
	"den-memories/internal/recall"
	"den-memories/internal/store"
)

// Config contains process-level service settings.
type Config struct {
	DatabasePath string
	RootPath     string
}

// App owns the service dependencies.
type App struct {
	store   *store.Store
	handler http.Handler
}

// New initializes the application, applies migrations, and builds the handler.
func New(cfg Config) (*App, error) {
	s, err := store.Open(cfg.DatabasePath)
	if err != nil {
		return nil, err
	}
	if _, err := s.ApplyMigrations(); err != nil {
		s.Close()
		return nil, err
	}
	if err := s.CheckCapabilities(); err != nil {
		s.Close()
		return nil, err
	}

	loader := contracts.NewLoader(cfg.RootPath)
	scoring, err := recall.LoadScoring(loader)
	if err != nil {
		s.Close()
		return nil, err
	}

	api := httpapi.New(httpapi.Config{
		Store:   s,
		Loader:  loader,
		Scoring: scoring,
	})
	return &App{store: s, handler: api.Routes()}, nil
}

// Handler returns the HTTP service handler.
func (a *App) Handler() http.Handler {
	return a.handler
}

// Close releases application resources.
func (a *App) Close() error {
	return a.store.Close()
}
