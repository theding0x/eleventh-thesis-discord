// Package model holds the name-based guild state shared by the desired side
// (built from the spec) and the observed side (read from Discord).
package model

// ChannelType is the kind of a channel. Other Discord channel types are not
// modelled.
type ChannelType int

// Channel kinds.
const (
	Text ChannelType = iota
	Voice
	Category
)

// Role is a guild role.
type Role struct {
	ID          string // empty on the desired side
	Name        string
	Color       int
	Hoist       bool
	Mentionable bool
	Permissions int64
	Position    int  // observed: Discord position (higher = higher in the hierarchy); desired: unused
	Managed     bool // integration/bot role (observed only)
}

// Overwrite is a permission overwrite on a channel.
type Overwrite struct {
	Target      string // role name, "@everyone", or "member:<user id>"
	Allow, Deny int64
}

// Channel is a category, text channel or voice channel.
type Channel struct {
	ID         string
	Name       string
	Type       ChannelType
	Parent     string // category name; "" for categories
	Position   int    // 0-based index among siblings (same Parent; categories among categories)
	Topic      string
	Overwrites []Overwrite // sorted by Target
}

// Message is a message posted by the bot.
type Message struct{ ID, Content string }

// Guild is the observed state of a Discord server.
type Guild struct {
	EveryonePermissions int64                // @everyone's server-wide permissions
	Roles               []Role               // excludes @everyone
	Channels            []Channel            // categories and channels; other types are omitted
	BotMessages         map[string][]Message // text channel name → the bot's own messages, oldest first
}
