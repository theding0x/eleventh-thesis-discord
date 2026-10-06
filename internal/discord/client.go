// Package discord defines the Client interface the provisioner talks to and an
// in-memory Fake of it. The live discordgo implementation lives in live.go.
package discord

import (
	"context"

	"github.com/theding0x/eleventh-thesis-discord/internal/model"
)

// Client reads and mutates a Discord server. Roles and channels are addressed
// by name, except where the argument carries an ID.
type Client interface {
	// Observe reads the guild. Roles are ordered by Position descending, then ID
	// ascending; Channels by Parent, Position, then ID ascending; so the first
	// object with a given name is well defined. BotMessages holds the bot's own
	// messages, oldest first, for the requested content channels only.
	Observe(ctx context.Context, contentChannels []string) (model.Guild, error)
	// UpdateEveryone sets @everyone's server-wide permissions.
	UpdateEveryone(ctx context.Context, permissions int64) error
	CreateRole(ctx context.Context, r model.Role) error
	// UpdateRole updates the role matched by Name.
	UpdateRole(ctx context.Context, r model.Role) error
	// ReorderRoles places the named roles directly below the provisioner role,
	// highest first.
	ReorderRoles(ctx context.Context, namesHighestFirst []string) error
	// DeleteRole deletes the role with r.ID, or the first one named r.Name when
	// r.ID is empty.
	DeleteRole(ctx context.Context, r model.Role) error
	// CreateChannel creates the channel; its Parent is resolved by name.
	CreateChannel(ctx context.Context, c model.Channel) error
	// UpdateChannel updates the channel matched by (Type, Name) and replaces its
	// parent, position, topic and overwrites.
	UpdateChannel(ctx context.Context, c model.Channel) error
	// DeleteChannel deletes the channel with c.ID, or the first one matching
	// (c.Type, c.Name) when c.ID is empty.
	DeleteChannel(ctx context.Context, c model.Channel) error
	PostMessage(ctx context.Context, channel, content string) error
	EditMessage(ctx context.Context, channel, messageID, content string) error
	DeleteMessage(ctx context.Context, channel, messageID string) error
}
