# laika

Notification gateway for the Stellar Guide homelab. Holds the Discord bot token
and SMTP credentials so every other service can fire-and-forget over HTTP.

Single-operator. No multi-tenancy. Sits behind Caddy + Tailscale.

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

| Variable                | Default | Description                                                  |
|-------------------------|---------|--------------------------------------------------------------|
| `PORT`                  | `8080`  | Port the server listens on                                   |
| `DISCORD_BOT_TOKEN`     | —       | **Required.** Bot token. Server refuses to start without it. |
| `LAIKA_RATELIMIT_RPM`   | `60`    | Per-caller refill rate (requests per minute)                 |
| `LAIKA_RATELIMIT_BURST` | `30`    | Per-caller burst capacity                                    |
| `SMTP_<FLOW>_HOST`      | —       | SMTP server hostname for the named flow                      |
| `SMTP_<FLOW>_PORT`      | —       | SMTP server port (e.g. `587`)                                |
| `SMTP_<FLOW>_USERNAME`  | —       | SMTP auth username                                           |
| `SMTP_<FLOW>_PASSWORD`  | —       | SMTP auth password                                           |
| `SMTP_<FLOW>_FROM`      | —       | Sender address for outbound email                            |

## Commands

```bash
make run      # start the dev server (respects PORT env var, defaults to 8080)
make build    # compile to bin/server
make tidy     # sync go.mod / go.sum
```

## API

All paths are prefixed `/api/v1`.

### Email

```
POST /noti/email/{flow}
```

Body: `{emails, message_type, subject, html_body}`. See [internal/modules/email/handler.go](internal/modules/email/handler.go) for the schema.

### Discord

The Discord side has three endpoints. The two GETs are operator-facing — use
them once when wiring up a new caller to find a channel ID, then paste the ID
into that caller's `.env`. Callers never invoke the GET endpoints at runtime.

#### `GET /noti/discord/servers`

Lists every guild the bot is in.

```json
[
  { "id": "123456789012345678", "name": "Stellar Guide" }
]
```

#### `GET /noti/discord/servers/{id}/channels`

Lists every channel + active thread in the guild, with the bot's resolved
`send_messages` permission.

```json
[
  { "id": "111111111111111111", "name": "alerts", "type": "text", "can_send": true }
]
```

`type` is one of `text`, `voice`, `category`, `news`, `thread`, `stage`,
`forum`, or `unknown`. Returns `404` if the bot is not in the guild.

#### `POST /noti/discord/channels/{id}/messages`

Pass-through to Discord's [Create Message](https://discord.com/developers/docs/resources/channel#create-message) endpoint. Whatever you send is forwarded byte-for-byte.

```json
{ "content": "Application created: Senior Backend Engineer @ Acme" }
```

Status codes:

| Code  | Meaning                                                          |
|-------|------------------------------------------------------------------|
| `202` | Accepted (Discord returned 2xx)                                  |
| `400` | Body is empty or not valid JSON                                  |
| `403` | Discord says the bot lacks send permission — pass-through        |
| `404` | Channel doesn't exist or bot was kicked — pass-through           |
| `429` | Rate limited (Laika's bucket or Discord's). `Retry-After` set    |
| `503` | Discord unreachable. Caller should retry with backoff            |

## Adding a new caller

1. `GET /api/v1/noti/discord/servers` to find the guild ID.
2. `GET /api/v1/noti/discord/servers/{guild_id}/channels` to find a channel where `can_send: true`.
3. Paste the channel ID into the caller's `.env`.
4. Caller does `POST /api/v1/noti/discord/channels/{channel_id}/messages` with a Discord-native body.

## Non-goals

These are intentional omissions, not TODOs. Don't bolt them on.

- **Slash commands / message listeners.** Laika is outbound only. A separate
  command bot would consume Laika as a downstream API.
- **Topic routing.** Callers hold channel IDs in their own `.env`. The two GET
  endpoints exist so the operator can find a channel ID, not for runtime
  routing.
- **Quiet hours, severity, dedup, priority.** Caller concerns. Laika has no
  concept of time of day.
- **Templating / formatting.** Body is Discord-native. Future connectors
  translate from this format, not into a Laika-flavored abstraction.
- **Auth at the application layer.** Network isolation comes from Tailscale.
  The per-caller rate limiter is self-protection, not access control.
- **Persistent message queue.** v1 is best-effort. If Laika crashes mid-send,
  the message is lost and the caller (which got no `202`) retries.
- **Multi-tenant isolation.** Single operator, single bot identity.

## Layout

```
laika/
├── cmd/server/main.go              # entrypoint — router wiring, server startup
├── internal/
│   ├── config/                     # env-driven config; one file per sub-config
│   ├── domain/errors.go            # sentinel errors (NotFound, Conflict, …)
│   ├── handler/respond.go          # WriteError helper
│   ├── middleware/
│   │   ├── request_id.go           # injects / forwards X-Request-ID
│   │   ├── recovery.go             # catches panics, returns 500
│   │   ├── logger.go               # structured request log per response
│   │   └── ratelimit.go            # per-caller token bucket
│   ├── modules/
│   │   ├── email/                  # email gateway (handler + service + registry)
│   │   ├── discord/                # discord gateway (handler + service)
│   │   └── health/handler.go       # GET /health — dependency ping checks
│   ├── provider/
│   │   ├── smtp.go                 # SMTP transport
│   │   └── discord.go              # discordgo session + raw send
│   └── reqctx/reqctx.go            # context key for request ID
└── pkg/logger/logger.go            # slog JSON logger + FromContext helper
```

## Middleware order

Defined in `cmd/server/main.go` — order matters:

| # | Middleware  | Purpose                                                |
|---|-------------|--------------------------------------------------------|
| 1 | `RequestID` | Assigns / forwards `X-Request-ID`                      |
| 2 | `Recovery`  | Catches panics before the logger runs                  |
| 3 | `Logger`    | Logs status + latency after everything else resolves   |
| 4 | `RateLimit` | Per-caller token bucket on authenticated routes        |

## Discord bot configuration

- **Intents:** `Guilds` only. Do **not** enable `GuildMessages`,
  `MessageContent`, or `GuildMembers` — Laika never reads inbound traffic.
- **Permissions per server:** View Channels, Send Messages, Embed Links,
  Attach Files. Add Send Messages in Threads if you want thread support.

## Log output

One JSON line per request:

```json
{"time":"2025-04-10T08:00:00Z","level":"INFO","msg":"request","request_id":"...","method":"POST","path":"/api/v1/noti/discord/channels/.../messages","status":202,"latency_ms":124}
```

Message bodies are never logged.
