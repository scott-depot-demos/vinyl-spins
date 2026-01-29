package app

import (
	"os"
	"path/filepath"

	"github.com/joho/godotenv"
)

// loadDotenvUpward tries to load a `.env` file from the current working directory,
// and if not found, walks up parent directories until it finds one.
//
// This makes `go run` from `backend/` work even when `.env` lives at repo root.
func loadDotenvUpward() error {
	// First try the normal behavior: ./.env
	if err := godotenv.Load(); err == nil {
		return nil
	}

	wd, err := os.Getwd()
	if err != nil {
		return err
	}

	dir := wd
	for {
		p := filepath.Join(dir, ".env")
		if _, statErr := os.Stat(p); statErr == nil {
			return godotenv.Load(p)
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return nil
}

