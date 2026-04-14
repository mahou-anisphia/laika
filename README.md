# laika

Production-ready Go REST API boilerplate.

## Setup

### Local development

```bash
cp .env.example .env   # fill in your values
make run               # start dev server (reads PORT from .env or defaults to 8080)
```

### Docker (recommended for production)

```bash
cp .env.example .env   # fill in your values
chmod +x script.sh
./script.sh            # checks .env, stops any old container, builds & deploys
```

The script will:
1. Abort if `.env` is missing
2. Stop and remove any existing `laika` container
3. Build a fresh image using the multi-stage `Dockerfile`
4. Start the container with `--env-file .env` on the port defined in `.env`

### Environment variables

| Variable       | Default | Description                        |
|---------------|---------|------------------------------------|
| `PORT`        | `8080`  | Port the server listens on         |
| `SMTP_HOST`   | —       | SMTP server hostname               |
| `SMTP_PORT`   | —       | SMTP server port (e.g. `587`)      |
| `SMTP_USERNAME` | —     | SMTP auth username                 |
| `SMTP_PASSWORD` | —     | SMTP auth password                 |
| `SMTP_FROM`   | —       | Sender address for outbound email  |

## Commands

```bash
make run      # start the dev server (respects PORT env var, defaults to 8080)
make build    # compile to bin/server
make tidy     # sync go.mod / go.sum
```

## Layout

```
hello-world/
├── cmd/server/main.go          # entrypoint — router wiring, server startup
├── internal/
│   ├── handler/
│   │   ├── health.go           # GET /health — dependency ping checks
│   │   └── respond.go          # WriteError helper used by all handlers
│   ├── middleware/
│   │   ├── request_id.go       # injects / forwards X-Request-ID
│   │   ├── logger.go           # structured request log per response
│   │   └── recovery.go         # catches panics, returns 500
│   ├── domain/
│   │   └── errors.go           # sentinel errors (NotFound, Conflict, …)
│   └── reqctx/
│       └── reqctx.go           # context key for request ID (shared, no cycles)
└── pkg/logger/
    └── logger.go               # slog JSON logger + FromContext helper
```

## Where to start wiring logic

**New route** — add a handler file under `internal/handler/`, register it in `cmd/server/main.go`:

```go
r.Get("/things", thingHdl.List)
r.Post("/things", thingHdl.Create)
```

**New domain error** — add a sentinel to `internal/domain/errors.go` and a matching case in `internal/handler/respond.go`.

**New dependency health check** — add the dep to `HealthHandler` in `internal/handler/health.go` and a ping block inside `Check`.

**Structured logging inside a handler** — pull a request-scoped logger from context:

```go
log := logger.FromContext(r.Context(), base)
log.Info("doing thing", "id", id)
```

**Database** — uncomment the `lib/pq` import in `cmd/server/main.go`, pass `*sql.DB` into your handler structs.

## Middleware order

Defined in `cmd/server/main.go` — order matters:

| # | Middleware | Purpose |
|---|-----------|---------|
| 1 | `RequestID` | Assigns / forwards `X-Request-ID` |
| 2 | `Recovery` | Catches panics before the logger runs |
| 3 | `Logger` | Logs status + latency after everything else resolves |

## Log output

One JSON line per request:

```json
{"time":"2025-04-10T08:00:00Z","level":"INFO","msg":"request","request_id":"...","method":"GET","path":"/","status":200,"latency_ms":1}
```
