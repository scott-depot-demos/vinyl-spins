FROM golang:1.22-alpine AS build

WORKDIR /src

ARG DEMO_BIG=0
ARG DEMO_BIG_N=20000

# Build context is repo root (see docker-compose.yml).
COPY go.work go.work
COPY backend/go.mod backend/go.mod
COPY backend/go.sum backend/go.sum

# Local discogs client module (used via backend/go.mod replace)
COPY go-discogs/go.mod go-discogs/go.mod
COPY go-discogs/ go-discogs/

WORKDIR /src/backend
RUN go mod download

WORKDIR /src
COPY backend/ backend/

WORKDIR /src/backend
RUN if [ "$DEMO_BIG" = "1" ]; then DEMO_BIG_N="$DEMO_BIG_N" go run ./tools/demobiggen -out internal/demobig/generated.go; fi
RUN go build -o /out/api ./cmd/api
RUN go build -o /out/migrate ./cmd/migrate
RUN go build -o /out/worker ./cmd/worker
RUN go build -o /out/report ./cmd/report

FROM alpine:3.20
RUN apk add --no-cache tzdata ca-certificates
RUN adduser -D -g '' app
USER app
WORKDIR /home/app

COPY --from=build /out/api /usr/local/bin/api
COPY --from=build /out/migrate /usr/local/bin/migrate
COPY --from=build /out/worker /usr/local/bin/worker
COPY --from=build /out/report /usr/local/bin/report
COPY backend/migrations ./migrations

EXPOSE 8080
ENV PORT=8080

CMD ["api"]

