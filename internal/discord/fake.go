package discord

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"

	"github.com/bwmarrin/discordgo"

	"github.com/theding0x/eleventh-thesis-discord/internal/model"
)

// DefaultEveryone is the server-wide permission set @everyone has in a freshly
// created Discord server (the subset of it the provisioner models).
const DefaultEveryone int64 = int64(discordgo.PermissionViewChannel |
	discordgo.PermissionSendMessages |
	discordgo.PermissionReadMessageHistory |
	discordgo.PermissionAddReactions |
	discordgo.PermissionVoiceConnect |
	discordgo.PermissionVoiceSpeak)

// Fake is an in-memory Client. It stands in for Discord in end-to-end tests, so
// it follows Discord where convergence depends on it: objects have IDs, names
// are not unique, duplicates can be deleted exactly by ID, Observe returns
// objects in a fixed order, and calls on things that do not exist fail.
//
// Guild may be edited directly to set up a scenario; every Client method
// re-sorts it into the order Observe documents before it reads or changes it.
// IDs are deterministic: roles r<n>, channels c<n>, messages m<n> from counters
// that never reuse a number (the provisioner role created by NewFake is r0).
// A number already taken by an object placed in Guild by hand is skipped.
//
// Bot messages are kept by channel name, as the model has them, so text
// channels that share a name share their messages.
type Fake struct {
	Guild  model.Guild
	FailOn string   // method name; that method returns an error and changes nothing
	Calls  []string // method names, in order, including a call that failed

	provisioner string
	nextRole    int
	nextChannel int
	nextMessage int
}

var _ Client = (*Fake)(nil)

// NewFake returns a guild that holds only the provisioner role (managed, at
// position 10) and has Discord's default @everyone permissions.
func NewFake(provisionerRole string) *Fake {
	return &Fake{
		provisioner: provisionerRole,
		Guild: model.Guild{
			EveryonePermissions: DefaultEveryone,
			Roles:               []model.Role{{ID: "r0", Name: provisionerRole, Position: 10, Managed: true}},
		},
	}
}

// begin records the call, fails it when asked to, and normalises the guild.
func (f *Fake) begin(method string) error {
	f.Calls = append(f.Calls, method)
	if f.FailOn == method {
		return fmt.Errorf("fake: %s: forced failure", method)
	}
	f.normalize()
	return nil
}

// normalize sorts roles by (Position descending, ID ascending) and channels by
// (Parent, Position, ID ascending); see order.go.
func (f *Fake) normalize() {
	sortRoles(f.Guild.Roles)
	sortChannels(f.Guild.Channels)
}

func copyOverwrites(in []model.Overwrite) []model.Overwrite {
	out := append([]model.Overwrite(nil), in...)
	sort.SliceStable(out, func(i, j int) bool { return out[i].Target < out[j].Target })
	return out
}

func copyChannel(c model.Channel) model.Channel {
	c.Overwrites = copyOverwrites(c.Overwrites)
	return c
}

func (f *Fake) newRoleID() string {
	for {
		f.nextRole++
		id := "r" + strconv.Itoa(f.nextRole)
		if f.roleIndexByID(id) < 0 {
			return id
		}
	}
}

func (f *Fake) newChannelID() string {
	for {
		f.nextChannel++
		id := "c" + strconv.Itoa(f.nextChannel)
		if f.channelIndexByID(id) < 0 {
			return id
		}
	}
}

func (f *Fake) newMessageID() string {
	for {
		f.nextMessage++
		id := "m" + strconv.Itoa(f.nextMessage)
		if !f.messageIDInUse(id) {
			return id
		}
	}
}

func (f *Fake) messageIDInUse(id string) bool {
	for _, msgs := range f.Guild.BotMessages {
		for _, m := range msgs {
			if m.ID == id {
				return true
			}
		}
	}
	return false
}

func (f *Fake) roleIndexByID(id string) int {
	for i, r := range f.Guild.Roles {
		if r.ID == id {
			return i
		}
	}
	return -1
}

func (f *Fake) roleIndexByName(name string) int {
	for i, r := range f.Guild.Roles {
		if r.Name == name {
			return i
		}
	}
	return -1
}

func (f *Fake) channelIndexByID(id string) int {
	for i, c := range f.Guild.Channels {
		if c.ID == id {
			return i
		}
	}
	return -1
}

func (f *Fake) channelIndex(t model.ChannelType, name string) int {
	for i, c := range f.Guild.Channels {
		if c.Type == t && c.Name == name {
			return i
		}
	}
	return -1
}

func (f *Fake) hasCategory(name string) bool { return f.channelIndex(model.Category, name) >= 0 }

// Observe returns a deep copy of the guild, in the order the Client interface
// documents. BotMessages holds only the requested channels that exist as text
// channels and have an entry.
func (f *Fake) Observe(_ context.Context, contentChannels []string) (model.Guild, error) {
	if err := f.begin("Observe"); err != nil {
		return model.Guild{}, err
	}
	g := model.Guild{
		EveryonePermissions: f.Guild.EveryonePermissions,
		Roles:               append([]model.Role(nil), f.Guild.Roles...),
		BotMessages:         map[string][]model.Message{},
	}
	for _, c := range f.Guild.Channels {
		g.Channels = append(g.Channels, copyChannel(c))
	}
	for _, name := range contentChannels {
		if f.channelIndex(model.Text, name) < 0 {
			continue
		}
		if msgs, ok := f.Guild.BotMessages[name]; ok {
			g.BotMessages[name] = append([]model.Message(nil), msgs...)
		}
	}
	return g, nil
}

// UpdateEveryone sets @everyone's server-wide permissions.
func (f *Fake) UpdateEveryone(_ context.Context, permissions int64) error {
	if err := f.begin("UpdateEveryone"); err != nil {
		return err
	}
	f.Guild.EveryonePermissions = permissions
	return nil
}

// CreateRole adds a role at position 1 and shifts every role from there up by
// one, so the relative order of the existing roles is kept.
func (f *Fake) CreateRole(_ context.Context, r model.Role) error {
	if err := f.begin("CreateRole"); err != nil {
		return err
	}
	for i := range f.Guild.Roles {
		if f.Guild.Roles[i].Position >= 1 {
			f.Guild.Roles[i].Position++
		}
	}
	r.ID = f.newRoleID()
	r.Position = 1
	r.Managed = false
	f.Guild.Roles = append(f.Guild.Roles, r)
	f.normalize()
	return nil
}

// UpdateRole sets colour, hoist, mentionable and permissions of the first role
// (in Observe order) with that name.
func (f *Fake) UpdateRole(_ context.Context, r model.Role) error {
	if err := f.begin("UpdateRole"); err != nil {
		return err
	}
	i := f.roleIndexByName(r.Name)
	if i < 0 {
		return fmt.Errorf("fake: update role %q: not found", r.Name)
	}
	have := &f.Guild.Roles[i]
	have.Color = r.Color
	have.Hoist = r.Hoist
	have.Mentionable = r.Mentionable
	have.Permissions = r.Permissions
	return nil
}

// ReorderRoles gives the named roles (first match by name) the positions
// provisioner-1, provisioner-2, ... in the given order. Other roles, and the
// provisioner role itself, keep their positions. Nothing changes if a name is
// unknown.
func (f *Fake) ReorderRoles(_ context.Context, namesHighestFirst []string) error {
	if err := f.begin("ReorderRoles"); err != nil {
		return err
	}
	prov := f.roleIndexByName(f.provisioner)
	if prov < 0 {
		return fmt.Errorf("fake: reorder roles: provisioner role %q not found", f.provisioner)
	}
	idx := make([]int, len(namesHighestFirst))
	for k, name := range namesHighestFirst {
		i := f.roleIndexByName(name)
		switch {
		case i < 0:
			return fmt.Errorf("fake: reorder roles: role %q not found", name)
		case i == prov:
			return fmt.Errorf("fake: reorder roles: cannot move the provisioner role %q", name)
		}
		idx[k] = i
	}
	base := f.Guild.Roles[prov].Position
	for k, i := range idx {
		f.Guild.Roles[i].Position = base - 1 - k
	}
	f.normalize()
	return nil
}

// DeleteRole removes the role with r.ID, or the first one named r.Name when r.ID
// is empty.
func (f *Fake) DeleteRole(_ context.Context, r model.Role) error {
	if err := f.begin("DeleteRole"); err != nil {
		return err
	}
	i := -1
	if r.ID != "" {
		i = f.roleIndexByID(r.ID)
	} else {
		i = f.roleIndexByName(r.Name)
	}
	if i < 0 {
		return fmt.Errorf("fake: delete role %q (ID %q): not found", r.Name, r.ID)
	}
	f.Guild.Roles = append(f.Guild.Roles[:i], f.Guild.Roles[i+1:]...)
	return nil
}

// CreateChannel adds the channel as given, with a new ID. A non-empty Parent
// must name an existing category.
func (f *Fake) CreateChannel(_ context.Context, c model.Channel) error {
	if err := f.begin("CreateChannel"); err != nil {
		return err
	}
	if c.Parent != "" && !f.hasCategory(c.Parent) {
		return fmt.Errorf("fake: create channel %q: parent category %q not found", c.Name, c.Parent)
	}
	c = copyChannel(c)
	c.ID = f.newChannelID()
	f.Guild.Channels = append(f.Guild.Channels, c)
	f.normalize()
	return nil
}

// UpdateChannel replaces Parent, Position, Topic and Overwrites of the first
// channel (in Observe order) with that type and name.
func (f *Fake) UpdateChannel(_ context.Context, c model.Channel) error {
	if err := f.begin("UpdateChannel"); err != nil {
		return err
	}
	i := f.channelIndex(c.Type, c.Name)
	if i < 0 {
		return fmt.Errorf("fake: update channel %q: not found", c.Name)
	}
	if c.Parent != "" && !f.hasCategory(c.Parent) {
		return fmt.Errorf("fake: update channel %q: parent category %q not found", c.Name, c.Parent)
	}
	have := &f.Guild.Channels[i]
	have.Parent = c.Parent
	have.Position = c.Position
	have.Topic = c.Topic
	have.Overwrites = copyOverwrites(c.Overwrites)
	f.normalize()
	return nil
}

// DeleteChannel removes the channel with c.ID, or the first one matching
// (c.Type, c.Name) when c.ID is empty. A text channel's bot messages go with it
// unless another text channel of that name remains. Deleting a category leaves
// its channels without a parent, as Discord does.
func (f *Fake) DeleteChannel(_ context.Context, c model.Channel) error {
	if err := f.begin("DeleteChannel"); err != nil {
		return err
	}
	i := -1
	if c.ID != "" {
		i = f.channelIndexByID(c.ID)
	} else {
		i = f.channelIndex(c.Type, c.Name)
	}
	if i < 0 {
		return fmt.Errorf("fake: delete channel %q (ID %q): not found", c.Name, c.ID)
	}
	gone := f.Guild.Channels[i]
	f.Guild.Channels = append(f.Guild.Channels[:i], f.Guild.Channels[i+1:]...)

	switch gone.Type {
	case model.Text:
		if f.channelIndex(model.Text, gone.Name) < 0 {
			delete(f.Guild.BotMessages, gone.Name)
		}
	case model.Category:
		if !f.hasCategory(gone.Name) {
			for k := range f.Guild.Channels {
				if f.Guild.Channels[k].Parent == gone.Name {
					f.Guild.Channels[k].Parent = ""
				}
			}
		}
	}
	f.normalize()
	return nil
}

var errNoMessage = errors.New("message not found")

// messages returns the bot messages of the named text channel.
func (f *Fake) messages(op, channel string) ([]model.Message, error) {
	if f.channelIndex(model.Text, channel) < 0 {
		return nil, fmt.Errorf("fake: %s: text channel %q not found", op, channel)
	}
	return f.Guild.BotMessages[channel], nil
}

func messageIndex(msgs []model.Message, id string) int {
	for i, m := range msgs {
		if m.ID == id {
			return i
		}
	}
	return -1
}

// PostMessage appends a bot message to the named text channel.
func (f *Fake) PostMessage(_ context.Context, channel, content string) error {
	if err := f.begin("PostMessage"); err != nil {
		return err
	}
	msgs, err := f.messages("post message", channel)
	if err != nil {
		return err
	}
	if f.Guild.BotMessages == nil {
		f.Guild.BotMessages = map[string][]model.Message{}
	}
	f.Guild.BotMessages[channel] = append(msgs, model.Message{ID: f.newMessageID(), Content: content})
	return nil
}

// EditMessage changes the content of a message and keeps its ID.
func (f *Fake) EditMessage(_ context.Context, channel, messageID, content string) error {
	if err := f.begin("EditMessage"); err != nil {
		return err
	}
	msgs, err := f.messages("edit message", channel)
	if err != nil {
		return err
	}
	i := messageIndex(msgs, messageID)
	if i < 0 {
		return fmt.Errorf("fake: edit message %q in #%s: %w", messageID, channel, errNoMessage)
	}
	msgs[i].Content = content
	return nil
}

// DeleteMessage removes a message.
func (f *Fake) DeleteMessage(_ context.Context, channel, messageID string) error {
	if err := f.begin("DeleteMessage"); err != nil {
		return err
	}
	msgs, err := f.messages("delete message", channel)
	if err != nil {
		return err
	}
	i := messageIndex(msgs, messageID)
	if i < 0 {
		return fmt.Errorf("fake: delete message %q in #%s: %w", messageID, channel, errNoMessage)
	}
	f.Guild.BotMessages[channel] = append(msgs[:i], msgs[i+1:]...)
	return nil
}
