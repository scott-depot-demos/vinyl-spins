# Vinyl Spin Tracker

Multi-user hosted web app: **Go backend + Postgres + responsive React UI**.

Features (MVP):
- Connect/disconnect Discogs via **OAuth 1.0a**
- Sync collection releases + enrich with release details (title/artist/artwork/years/tracklist/Discogs links)
- Track spins (**CRUD**): datetime + note
- Albums + spins list: sortable/filterable, including spin count + last spun
- Album **groups** + **weighted random** pick (bias toward older last-spun)
- Share a spin (Web Share + shareable link)

## Prereqs
- Flox (recommended for a consistent dev toolchain)
- Docker (optional; useful if you prefer running Postgres via containers)

## Setup

### Flox dev environment (recommended)

1. Initialize/enter the environment:

```bash
flox activate
```

This provides pinned versions of Go + Node/pnpm + Postgres CLI and a few useful dev tools.

2. (Optional) Start a local Postgres via Flox:

```bash
flox services start postgres
```

### App setup

1. Create a Discogs application and set the callback URL to:
   - `http://localhost:8080/auth/discogs/callback`

2. Copy env file and fill in secrets:

```bash
cp env.example .env
```

3. Start everything:

```bash
docker compose up --build
```

Then open:
- UI: `http://localhost:5173`
- API: `http://localhost:8080`

## Notes
- OAuth access tokens/secrets are encrypted at rest using `APP_ENC_KEY` (AES-GCM).
- This repo uses `github.com/stmcallister/go-discogs` (via a Go workspace).

