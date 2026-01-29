set dotenv-load := true

default:
  @just --list

# Flox Postgres service (optional)
pg-start:
  flox services start postgres

pg-stop:
  flox services stop postgres

pg-status:
  flox services status

# Backend
api:
  cd backend && go run ./cmd/api

migrate-up:
  cd backend && go run ./cmd/migrate up

migrate-down:
  cd backend && go run ./cmd/migrate down

migrate-status:
  cd backend && go run ./cmd/migrate status

# Frontend
ui:
  cd frontend && pnpm install && pnpm dev

# Docker (matches README path)
compose-up:
  docker compose up --build

