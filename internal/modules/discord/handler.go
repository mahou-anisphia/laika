package discord

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"laika/internal/domain"
	"laika/pkg/logger"
)

// -- DTOs ---------------------------------------------------------------------

type guildDTO struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type channelDTO struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Type    string `json:"type"`
	CanSend bool   `json:"can_send"`
}

// -- Handler ------------------------------------------------------------------

// Handler serves the three Discord endpoints. State queries hit the in-memory
// cache; sends go through the service to Discord.
type Handler struct {
	svc *Service
	log *slog.Logger
}

func NewHandler(svc *Service, log *slog.Logger) *Handler {
	return &Handler{svc: svc, log: log}
}

// ListServers handles GET /api/v1/servers. Operator-facing.
func (h *Handler) ListServers(w http.ResponseWriter, r *http.Request) {
	guilds := h.svc.ListGuilds(r.Context())

	out := make([]guildDTO, len(guilds))
	for i, g := range guilds {
		out[i] = guildDTO{ID: g.ID, Name: g.Name}
	}
	writeJSON(w, http.StatusOK, out)
}

// ListChannels handles GET /api/v1/servers/{id}/channels. Operator-facing.
func (h *Handler) ListChannels(w http.ResponseWriter, r *http.Request) {
	log := logger.FromContext(r.Context(), h.log)
	guildID := chi.URLParam(r, "id")

	channels, err := h.svc.ListChannels(r.Context(), guildID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{
				"error": "guild not found or bot is not a member",
			})
			return
		}
		log.Error("list channels failed", "guild_id", guildID, "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}

	out := make([]channelDTO, len(channels))
	for i, c := range channels {
		out[i] = channelDTO{ID: c.ID, Name: c.Name, Type: c.Type, CanSend: c.CanSend}
	}
	writeJSON(w, http.StatusOK, out)
}

// SendMessage handles POST /api/v1/channels/{id}/messages. Pass-through to
// Discord's create-message endpoint. The request body is forwarded as-is, so
// any field Discord supports (embeds, components, allowed_mentions, ...) works
// without Laika needing to know about it.
//
// Status mapping:
//   - 202 on Discord 2xx (sent)
//   - pass-through on Discord 4xx (400/403/404/429), forwarding Discord's JSON
//   - 503 on transport failure (caller retries with backoff)
//
// Message bodies are never logged.
func (h *Handler) SendMessage(w http.ResponseWriter, r *http.Request) {
	log := logger.FromContext(r.Context(), h.log)
	channelID := chi.URLParam(r, "id")

	body, err := io.ReadAll(r.Body)
	if err != nil {
		log.Warn("read body failed", "channel_id", channelID, "error", err)
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "could not read request body"})
		return
	}
	if len(body) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "empty request body"})
		return
	}
	if !json.Valid(body) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "request body is not valid JSON"})
		return
	}

	res, err := h.svc.SendMessage(r.Context(), channelID, body)
	if err != nil {
		if errors.Is(err, ErrDiscordUnreachable) {
			log.Error("discord unreachable", "channel_id", channelID, "error", err)
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{
				"error": "discord unreachable, retry with backoff",
			})
			return
		}
		log.Error("discord send failed", "channel_id", channelID, "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}

	log.Info("discord send outcome",
		"channel_id", channelID,
		"discord_status", res.StatusCode,
	)

	// Discord returned success — translate to 202 per the spec. We deliberately
	// don't echo Discord's message object; callers don't need it.
	if res.StatusCode >= 200 && res.StatusCode < 300 {
		writeJSON(w, http.StatusAccepted, map[string]string{"status": "sent"})
		return
	}

	// Pass-through: forward Discord's status code and JSON body so the caller's
	// logs surface Discord's actual reason ("Missing Access", "Unknown Channel",
	// etc.) instead of a generic Laika error.
	if res.StatusCode == http.StatusTooManyRequests && res.RetryAfter > 0 {
		w.Header().Set("Retry-After", strconv.Itoa(res.RetryAfter))
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(res.StatusCode)
	_, _ = w.Write(res.Body)
}

// -- Helpers ------------------------------------------------------------------

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
