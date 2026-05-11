package discord

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/bwmarrin/discordgo"

	"laika/internal/domain"
	"laika/internal/provider"
	"laika/pkg/logger"
)

// Session is the subset of *provider.DiscordSession the service depends on.
// Defined here so the service is testable without a live gateway.
type Session interface {
	Underlying() *discordgo.Session
	SendMessage(channelID string, payload []byte) (provider.DiscordSendResult, error)
}

// GuildSummary is the operator-facing view of a guild.
type GuildSummary struct {
	ID   string
	Name string
}

// ChannelSummary is the operator-facing view of a channel.
type ChannelSummary struct {
	ID      string
	Name    string
	Type    string
	CanSend bool
}

// Service holds Discord business logic. State queries are served from the
// in-memory cache populated by the gateway; sends are pass-through to Discord's
// REST API.
type Service struct {
	sess Session
	log  *slog.Logger
}

func NewService(sess Session, log *slog.Logger) *Service {
	return &Service{sess: sess, log: log}
}

// ListGuilds returns every guild the bot is currently in, as known to the
// in-memory state cache.
func (s *Service) ListGuilds(ctx context.Context) []GuildSummary {
	state := s.sess.Underlying().State
	state.RLock()
	defer state.RUnlock()

	out := make([]GuildSummary, 0, len(state.Guilds))
	for _, g := range state.Guilds {
		out = append(out, GuildSummary{ID: g.ID, Name: g.Name})
	}
	return out
}

// ListChannels returns every channel (including active threads) in the given
// guild. Returns domain.ErrNotFound if the bot is not in the guild.
func (s *Service) ListChannels(ctx context.Context, guildID string) ([]ChannelSummary, error) {
	dg := s.sess.Underlying()

	guild, err := dg.State.Guild(guildID)
	if err != nil || guild == nil {
		return nil, domain.ErrNotFound
	}

	botID := ""
	if dg.State.User != nil {
		botID = dg.State.User.ID
	}

	channels := make([]*discordgo.Channel, 0, len(guild.Channels)+len(guild.Threads))
	channels = append(channels, guild.Channels...)
	channels = append(channels, guild.Threads...)

	out := make([]ChannelSummary, 0, len(channels))
	for _, c := range channels {
		canSend := false
		if botID != "" {
			perms, perr := dg.State.UserChannelPermissions(botID, c.ID)
			if perr == nil {
				canSend = perms&discordgo.PermissionSendMessages != 0
			}
		}
		out = append(out, ChannelSummary{
			ID:      c.ID,
			Name:    c.Name,
			Type:    channelTypeLabel(c.Type),
			CanSend: canSend,
		})
	}
	return out, nil
}

// SendResult is the service-level result of a pass-through send. The handler
// translates it to HTTP. Body is Discord's raw response body, forwarded to the
// caller so they see Discord's reason text.
type SendResult struct {
	StatusCode int
	Body       []byte
	RetryAfter int // seconds; populated when StatusCode == 429
}

// ErrDiscordUnreachable is returned when the HTTP call to Discord fails before
// receiving a response. Mapped to 503 by the handler.
var ErrDiscordUnreachable = errors.New("discord unreachable")

// SendMessage forwards a raw Discord-native message payload to a channel.
// Network failures become ErrDiscordUnreachable; everything Discord returns is
// passed through verbatim.
func (s *Service) SendMessage(ctx context.Context, channelID string, payload []byte) (SendResult, error) {
	log := logger.FromContext(ctx, s.log)

	res, err := s.sess.SendMessage(channelID, payload)
	if err != nil {
		log.Error("discord send transport error", "channel_id", channelID, "error", err)
		return SendResult{}, fmt.Errorf("%w: %v", ErrDiscordUnreachable, err)
	}

	out := SendResult{StatusCode: res.StatusCode, Body: res.Body}
	if res.RetryAfter > 0 {
		out.RetryAfter = int(res.RetryAfter.Seconds())
	}
	return out, nil
}

// channelTypeLabel maps discordgo's numeric channel type to the short string
// the API surface exposes. Unknown types fall through to "unknown" rather than
// guessing — Discord adds new channel types over time and we'd rather be loud.
func channelTypeLabel(t discordgo.ChannelType) string {
	switch t {
	case discordgo.ChannelTypeGuildText:
		return "text"
	case discordgo.ChannelTypeGuildVoice:
		return "voice"
	case discordgo.ChannelTypeGuildCategory:
		return "category"
	case discordgo.ChannelTypeGuildNews:
		return "news"
	case discordgo.ChannelTypeGuildNewsThread,
		discordgo.ChannelTypeGuildPublicThread,
		discordgo.ChannelTypeGuildPrivateThread:
		return "thread"
	case discordgo.ChannelTypeGuildStageVoice:
		return "stage"
	case discordgo.ChannelTypeGuildForum:
		return "forum"
	default:
		return "unknown"
	}
}
