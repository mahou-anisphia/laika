package provider

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/bwmarrin/discordgo"

	"laika/internal/config"
)

// discordAPIBase is the Discord REST API base. Pinned to v10 so a library
// upgrade can't silently swap it.
const discordAPIBase = "https://discord.com/api/v10"

// DiscordSendResult is the raw outcome of a pass-through send call.
type DiscordSendResult struct {
	StatusCode int
	Body       []byte
	RetryAfter time.Duration // populated when StatusCode == 429
}

// DiscordSession wraps a discordgo.Session for state queries plus a thin HTTP
// client for the pass-through send endpoint. The discordgo session is opened to
// keep the in-memory guild/channel cache fresh; sends bypass it so the HTTP
// payload is forwarded byte-for-byte.
type DiscordSession struct {
	sess  *discordgo.Session
	token string
	http  *http.Client
}

// NewDiscordSession constructs a session and opens the gateway with the minimum
// intents needed to maintain a guild/channel cache. It explicitly does not
// request message or member intents — Laika never reads inbound traffic.
func NewDiscordSession(cfg config.Discord) (*DiscordSession, error) {
	if cfg.BotToken == "" {
		return nil, errors.New("discord: bot token is empty")
	}

	s, err := discordgo.New("Bot " + cfg.BotToken)
	if err != nil {
		return nil, fmt.Errorf("discord: new session: %w", err)
	}
	s.Identify.Intents = discordgo.IntentsGuilds
	s.StateEnabled = true

	if err := s.Open(); err != nil {
		return nil, fmt.Errorf("discord: open gateway: %w", err)
	}

	return &DiscordSession{
		sess:  s,
		token: cfg.BotToken,
		http:  &http.Client{Timeout: 15 * time.Second},
	}, nil
}

// Close shuts the gateway connection. Safe to call multiple times.
func (d *DiscordSession) Close() error {
	if d.sess == nil {
		return nil
	}
	return d.sess.Close()
}

// Underlying exposes the wrapped discordgo session for read-only state access
// from the service layer. Callers must not Open/Close it.
func (d *DiscordSession) Underlying() *discordgo.Session {
	return d.sess
}

// SendMessage forwards a raw JSON payload to Discord's create-message endpoint
// for the given channel. It returns Discord's HTTP status, response body, and
// (for 429s) the Retry-After duration parsed from the response header.
func (d *DiscordSession) SendMessage(channelID string, payload []byte) (DiscordSendResult, error) {
	url := fmt.Sprintf("%s/channels/%s/messages", discordAPIBase, channelID)
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return DiscordSendResult{}, fmt.Errorf("discord: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bot "+d.token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Laika (https://github.com/mahou-anisphia, 1)")

	resp, err := d.http.Do(req)
	if err != nil {
		return DiscordSendResult{}, fmt.Errorf("discord: send: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return DiscordSendResult{}, fmt.Errorf("discord: read response: %w", err)
	}

	out := DiscordSendResult{StatusCode: resp.StatusCode, Body: body}
	if resp.StatusCode == http.StatusTooManyRequests {
		if v := resp.Header.Get("Retry-After"); v != "" {
			if secs, perr := time.ParseDuration(v + "s"); perr == nil {
				out.RetryAfter = secs
			}
		}
	}
	return out, nil
}
