FROM golang:1.22-alpine AS build

WORKDIR /src

# Build context is repo root (see docker-compose.yml), so paths are rooted there.
COPY discogs-listen-tracker/go.work go.work
COPY discogs-listen-tracker/backend/go.mod backend/go.mod
COPY go-discogs/go.mod /go-discogs/go.mod

WORKDIR /src/backend
RUN go mod download

WORKDIR /src
COPY discogs-listen-tracker/backend/ backend/
COPY go-discogs/ /go-discogs/

WORKDIR /src/backend
RUN go build -o /out/api ./cmd/api
RUN go build -o /out/migrate ./cmd/migrate

FROM alpine:3.20
RUN adduser -D -g '' app
USER app
WORKDIR /home/app

COPY --from=build /out/api /usr/local/bin/api
COPY --from=build /out/migrate /usr/local/bin/migrate
COPY discogs-listen-tracker/backend/migrations ./migrations

EXPOSE 8080
ENV PORT=8080

CMD ["api"]

