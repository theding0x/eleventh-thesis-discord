package discord

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/bwmarrin/discordgo"

	"github.com/theding0x/eleventh-thesis-discord/internal/model"
)

// Live is the discordgo implementation of Client. It uses the REST API only
// (no gateway connection), and it is not safe for concurrent use.
//
// Names are resolved through caches that Observe fills in Observe order, so the
// first object with a given name is the managed one, as the Client interface
// documents. Creates add to the caches but never replace an existing entry.
// Observe must be called before any other method that takes a name.
//
// The bot token is kept only to scrub it out of error text (see scrubToken):
// no method returns, logs or prints it, and String/GoString hide it too.
type Live struct {
	s       *discordgo.Session
	token   string
	guildID string
	botID   string

	// provisioner is the name of the bot's own role. The Client interface has no
	// way to pass it, so the CLI calls SetProvisionerRole once after NewLive. (The
	// alternative, taking the highest-position Managed role, picks the wrong role
	// as soon as another bot sits above ours.)
	provisioner string

	roleIDs    map[string]string  // role name → ID of the first role with that name
	channelIDs map[chanKey]string // (type, name) → ID of the first channel with that key

	// everyone is @everyone's permissions as of the last Observe; everyoneKnown is
	// false until then. A 403 on UpdateEveryone uses it to name the offending bits.
	everyone      int64
	everyoneKnown bool
}

var _ Client = (*Live)(nil)

// chanKey identifies a channel by (type, name), the way the model matches them.
type chanKey struct {
	Type model.ChannelType
	Name string
}

// NewLive connects to Discord with a bot token and learns the bot's own user ID
// (used to recognise its messages). The token never appears in a returned error.
func NewLive(token, guildID string) (*Live, error) {
	if token == "" {
		return nil, errors.New("discord: empty bot token")
	}
	if guildID == "" {
		return nil, errors.New("discord: empty guild ID")
	}
	s, err := discordgo.New("Bot " + token)
	if err != nil {
		return nil, fmt.Errorf("discord: create session: %w", scrubToken(err, token))
	}
	// Debug logging would print request headers, which include the token.
	s.Debug = false
	s.StateEnabled = false

	me, err := s.User("@me")
	if err != nil {
		return nil, fmt.Errorf("discord: fetch bot user: %w", scrubToken(err, token))
	}
	return &Live{
		s:          s,
		token:      token,
		guildID:    guildID,
		botID:      me.ID,
		roleIDs:    map[string]string{},
		channelIDs: map[chanKey]string{},
	}, nil
}

// SetProvisionerRole names the role the bot runs as. ReorderRoles places roles
// below it, and DeleteRole refuses to delete it.
func (l *Live) SetProvisionerRole(name string) { l.provisioner = name }

// String and GoString keep the token out of any %v / %+v / %#v rendering.
func (l *Live) String() string   { return "discord.Live{guild: " + l.guildID + "}" }
func (l *Live) GoString() string { return l.String() }

// scrubToken returns a new error whose text is err's with every occurrence of
// the token replaced by "***". It deliberately does not wrap err: a discordgo
// *RESTError carries the http.Request, including the Authorization header.
func scrubToken(err error, token string) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	if token != "" {
		msg = strings.ReplaceAll(msg, token, "***")
	}
	return errors.New(msg)
}

// fail wraps a discordgo error for return: context errors keep their identity
// (they cannot contain the token); everything else is scrubbed.
func (l *Live) fail(ctx context.Context, op string, err error) error {
	if err == nil {
		return nil
	}
	if cerr := ctx.Err(); cerr != nil {
		return fmt.Errorf("discord: %s: %w", op, cerr)
	}
	return fmt.Errorf("discord: %s: %w", op, scrubToken(err, l.token))
}

// opts returns the request options every call gets. discordgo v0.29.0 accepts
// WithContext on every REST method (restapi.go:160); ctx.Err() is also checked
// before each call because the rate-limit back-off sleeps without looking at it.
func opts(ctx context.Context) []discordgo.RequestOption {
	return []discordgo.RequestOption{discordgo.WithContext(ctx)}
}

// ---- pure helpers -----------------------------------------------------------

// toModelChannel converts a Discord channel. It returns false for types other
// than text, voice and category. Position is the raw Discord position;
// normalizePositions turns the whole set into sibling indexes afterwards.
// Overwrite targets are role names ("@everyone" for the guild-ID role,
// "role:<id>" when the role is unknown) or "member:<id>".
func toModelChannel(c *discordgo.Channel, guildID string, roleNames, categoryNames map[string]string) (model.Channel, bool) {
	var t model.ChannelType
	switch c.Type {
	case discordgo.ChannelTypeGuildText:
		t = model.Text
	case discordgo.ChannelTypeGuildVoice:
		t = model.Voice
	case discordgo.ChannelTypeGuildCategory:
		t = model.Category
	default:
		return model.Channel{}, false
	}
	out := model.Channel{ID: c.ID, Name: c.Name, Type: t, Position: c.Position}
	if t == model.Text {
		out.Topic = c.Topic
	}
	if t != model.Category && c.ParentID != "" {
		name, ok := categoryNames[c.ParentID]
		if !ok {
			name = "category:" + c.ParentID
		}
		out.Parent = name
	}
	for _, ow := range c.PermissionOverwrites {
		target := "member:" + ow.ID
		if ow.Type == discordgo.PermissionOverwriteTypeRole {
			switch name, ok := roleNames[ow.ID]; {
			case ow.ID == guildID:
				target = "@everyone"
			case ok:
				target = name
			default:
				target = "role:" + ow.ID
			}
		}
		out.Overwrites = append(out.Overwrites, model.Overwrite{Target: target, Allow: ow.Allow, Deny: ow.Deny})
	}
	sort.SliceStable(out.Overwrites, func(i, j int) bool { return out.Overwrites[i].Target < out.Overwrites[j].Target })
	return out, true
}

// normalizePositions rewrites each channel's Position as its 0-based index among
// its siblings in the order Discord shows them: text channels before voice
// channels, each by Discord position then ID. Siblings share a Parent;
// categories are siblings of each other only.
func normalizePositions(chs []model.Channel) {
	type group struct {
		category bool
		parent   string
	}
	groups := map[group][]int{}
	for i, c := range chs {
		k := group{c.Type == model.Category, c.Parent}
		groups[k] = append(groups[k], i)
	}
	for _, idxs := range groups {
		sort.SliceStable(idxs, func(a, b int) bool {
			x, y := chs[idxs[a]], chs[idxs[b]]
			return displayLess(x.Type == model.Voice, x.Position, x.ID, y.Type == model.Voice, y.Position, y.ID)
		})
		for rank, i := range idxs {
			chs[i].Position = rank
		}
	}
}

// observeChannels converts every supported channel, numbers siblings and sorts
// the result the way the Client interface documents.
func observeChannels(in []*discordgo.Channel, guildID string, roleNames map[string]string) []model.Channel {
	categoryNames := map[string]string{}
	for _, c := range in {
		if c.Type == discordgo.ChannelTypeGuildCategory {
			categoryNames[c.ID] = c.Name
		}
	}
	var out []model.Channel
	for _, c := range in {
		if mc, ok := toModelChannel(c, guildID, roleNames, categoryNames); ok {
			out = append(out, mc)
		}
	}
	normalizePositions(out)
	sortChannels(out)
	return out
}

// rolesFromDiscord maps Discord roles to the model, in input order. The role
// whose ID equals the guild ID is @everyone: it is left out of the returned
// roles and its permissions are returned separately.
func rolesFromDiscord(in []*discordgo.Role, guildID string) (roles []model.Role, everyone int64) {
	for _, r := range in {
		if r == nil {
			continue
		}
		if r.ID == guildID {
			everyone = r.Permissions
			continue
		}
		roles = append(roles, model.Role{
			ID: r.ID, Name: r.Name, Color: r.Color, Hoist: r.Hoist, Mentionable: r.Mentionable,
			Permissions: r.Permissions, Position: r.Position, Managed: r.Managed,
		})
	}
	return roles, everyone
}

// roleNamesByID maps every role ID, including @everyone's, to its name.
func roleNamesByID(roles []*discordgo.Role) map[string]string {
	m := make(map[string]string, len(roles))
	for _, r := range roles {
		if r != nil {
			m[r.ID] = r.Name
		}
	}
	return m
}

// buildRoleIDs maps role name → ID from roles already in Observe order; the
// first role with a name wins.
func buildRoleIDs(sorted []model.Role) map[string]string {
	m := make(map[string]string, len(sorted))
	for _, r := range sorted {
		if _, ok := m[r.Name]; !ok {
			m[r.Name] = r.ID
		}
	}
	return m
}

// buildChannelIDs maps (type, name) → ID from channels already in Observe order;
// the first channel with a key wins.
func buildChannelIDs(sorted []model.Channel) map[chanKey]string {
	m := make(map[chanKey]string, len(sorted))
	for _, c := range sorted {
		k := chanKey{c.Type, c.Name}
		if _, ok := m[k]; !ok {
			m[k] = c.ID
		}
	}
	return m
}

// toRoleParams sets every field as a non-nil pointer, zero values included.
// Discord gives a role created without explicit permissions a copy of
// @everyone's (spec §2.3 step 2), and a nil field on update leaves drift in place.
func toRoleParams(r model.Role) *discordgo.RoleParams {
	color, hoist, mentionable, perms := r.Color, r.Hoist, r.Mentionable, r.Permissions
	return &discordgo.RoleParams{
		Name:        r.Name,
		Color:       &color,
		Hoist:       &hoist,
		Mentionable: &mentionable,
		Permissions: &perms,
	}
}

// toDiscordOverwrites converts overwrites for a write. "@everyone" is the role
// overwrite with the guild's ID, a role name is looked up in roleIDs, and
// "member:<id>" is a member overwrite. Allow and Deny are copied exactly. The
// result is never nil, so an empty set is sent as [] and clears old overwrites.
func toDiscordOverwrites(ows []model.Overwrite, roleIDs map[string]string, guildID string) ([]*discordgo.PermissionOverwrite, error) {
	out := make([]*discordgo.PermissionOverwrite, 0, len(ows))
	for _, ow := range ows {
		d := &discordgo.PermissionOverwrite{Allow: ow.Allow, Deny: ow.Deny}
		switch {
		case ow.Target == "@everyone":
			d.ID, d.Type = guildID, discordgo.PermissionOverwriteTypeRole
		case strings.HasPrefix(ow.Target, "member:"):
			d.ID, d.Type = strings.TrimPrefix(ow.Target, "member:"), discordgo.PermissionOverwriteTypeMember
		default:
			id, ok := roleIDs[ow.Target]
			if !ok {
				return nil, fmt.Errorf("overwrite target role %q not found in the server", ow.Target)
			}
			d.ID, d.Type = id, discordgo.PermissionOverwriteTypeRole
		}
		out = append(out, d)
	}
	return out, nil
}

// isVoiceLike reports whether Discord lists the channel with the voice channels.
func isVoiceLike(t discordgo.ChannelType) bool {
	return t == discordgo.ChannelTypeGuildVoice || t == discordgo.ChannelTypeGuildStageVoice
}

// siblingPayload builds the bulk-reorder request for one sibling list: the
// siblings in display order (text before voice, then Discord position, then ID),
// with the target placed at index (clamped to the list) and everything numbered
// 0..n-1. Only ID and Position are set; see writePosition for why that matters.
func siblingPayload(siblings []*discordgo.Channel, targetID string, index int) ([]*discordgo.Channel, error) {
	sorted := append([]*discordgo.Channel(nil), siblings...)
	sort.SliceStable(sorted, func(i, j int) bool {
		a, b := sorted[i], sorted[j]
		return displayLess(isVoiceLike(a.Type), a.Position, a.ID, isVoiceLike(b.Type), b.Position, b.ID)
	})
	var target *discordgo.Channel
	others := make([]*discordgo.Channel, 0, len(sorted))
	for _, c := range sorted {
		if c.ID == targetID {
			target = c
		} else {
			others = append(others, c)
		}
	}
	if target == nil {
		return nil, fmt.Errorf("channel %s is not among its siblings", targetID)
	}
	if index < 0 {
		index = 0
	}
	if index > len(others) {
		index = len(others)
	}
	ordered := make([]*discordgo.Channel, 0, len(sorted))
	ordered = append(ordered, others[:index]...)
	ordered = append(ordered, target)
	ordered = append(ordered, others[index:]...)

	out := make([]*discordgo.Channel, len(ordered))
	for i, c := range ordered {
		out[i] = &discordgo.Channel{ID: c.ID, Position: i}
	}
	return out, nil
}

// positionsDiffer reports whether the payload changes any sibling's position.
func positionsDiffer(siblings, payload []*discordgo.Channel) bool {
	have := make(map[string]int, len(siblings))
	for _, c := range siblings {
		have[c.ID] = c.Position
	}
	for _, p := range payload {
		if pos, ok := have[p.ID]; !ok || pos != p.Position {
			return true
		}
	}
	return false
}

// channelPatchBody is the body of the single-channel edit (PATCH /channels/{id}).
// It is built by hand rather than with discordgo.ChannelEdit because that type
// marks Topic, ParentID and PermissionOverwrites omitempty (structs.go:471-479),
// so an empty topic, no parent or no overwrites would be left out of the request
// and the old value would stay. Position is never sent here (see writePosition).
//
// Text channels send topic; text and voice channels send parent_id (null when
// they have none); categories send neither. Overwrites are always sent, and
// replace the existing set.
func channelPatchBody(c model.Channel, parentID string, ows []*discordgo.PermissionOverwrite) map[string]any {
	if ows == nil {
		ows = []*discordgo.PermissionOverwrite{}
	}
	body := map[string]any{"permission_overwrites": ows}
	if c.Type == model.Text {
		body["topic"] = c.Topic
	}
	if c.Type != model.Category {
		if parentID == "" {
			body["parent_id"] = nil
		} else {
			body["parent_id"] = parentID
		}
	}
	return body
}

// rolePosition is one entry of the role-reorder request.
type rolePosition struct {
	ID       string `json:"id"`
	Position int    `json:"position"`
}

// rolePositions numbers the roles provisionerPos-1, provisionerPos-2, ...
// Position 0 is @everyone's, so the lowest position must be at least 1.
func rolePositions(provisionerPos int, idsHighestFirst []string) ([]rolePosition, error) {
	if len(idsHighestFirst) > 0 && provisionerPos-len(idsHighestFirst) < 1 {
		return nil, fmt.Errorf("the provisioner role is at position %d, which leaves no room for %d roles below it", provisionerPos, len(idsHighestFirst))
	}
	out := make([]rolePosition, len(idsHighestFirst))
	for i, id := range idsHighestFirst {
		out[i] = rolePosition{ID: id, Position: provisionerPos - 1 - i}
	}
	return out, nil
}

// noMentions is the allowed_mentions value that parses nothing: Parse is not
// omitempty in discordgo (message.go:339), so the empty slice is sent as [].
func noMentions() *discordgo.MessageAllowedMentions {
	return &discordgo.MessageAllowedMentions{Parse: []discordgo.AllowedMentionType{}}
}

// ownMessages keeps the plain messages written by the bot, in input order.
func ownMessages(msgs []*discordgo.Message, botID string) []model.Message {
	var out []model.Message
	for _, m := range msgs {
		if m == nil || m.Author == nil || m.Author.ID != botID || m.Type != discordgo.MessageTypeDefault {
			continue
		}
		out = append(out, model.Message{ID: m.ID, Content: m.Content})
	}
	return out
}

// ---- Client -----------------------------------------------------------------

// Observe reads the guild, rebuilds the name caches, and reads the bot's own
// messages in the managed (first) text channel of each requested name.
func (l *Live) Observe(ctx context.Context, contentChannels []string) (model.Guild, error) {
	if err := ctx.Err(); err != nil {
		return model.Guild{}, err
	}
	rawRoles, err := l.s.GuildRoles(l.guildID, opts(ctx)...)
	if err != nil {
		return model.Guild{}, l.fail(ctx, "list roles", err)
	}
	roles, everyone := rolesFromDiscord(rawRoles, l.guildID)
	sortRoles(roles)

	if err := ctx.Err(); err != nil {
		return model.Guild{}, err
	}
	rawChannels, err := l.s.GuildChannels(l.guildID, opts(ctx)...)
	if err != nil {
		return model.Guild{}, l.fail(ctx, "list channels", err)
	}
	channels := observeChannels(rawChannels, l.guildID, roleNamesByID(rawRoles))

	l.roleIDs = buildRoleIDs(roles)
	l.channelIDs = buildChannelIDs(channels)
	l.everyone, l.everyoneKnown = everyone, true

	g := model.Guild{
		EveryonePermissions: everyone,
		Roles:               roles,
		Channels:            channels,
		BotMessages:         map[string][]model.Message{},
	}
	for _, name := range contentChannels {
		id, ok := l.channelIDs[chanKey{model.Text, name}]
		if !ok {
			continue
		}
		msgs, err := l.ownHistory(ctx, id)
		if err != nil {
			return model.Guild{}, l.fail(ctx, "read messages in #"+name, err)
		}
		if len(msgs) > 0 {
			g.BotMessages[name] = msgs
		}
	}
	return g, nil
}

// ownHistory pages through a channel's history, newest first, and returns the
// bot's own messages oldest first.
func (l *Live) ownHistory(ctx context.Context, channelID string) ([]model.Message, error) {
	const pageSize = 100
	var newestFirst []model.Message
	before := ""
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		batch, err := l.s.ChannelMessages(channelID, pageSize, before, "", "", opts(ctx)...)
		if err != nil {
			return nil, err
		}
		if len(batch) == 0 {
			break
		}
		newestFirst = append(newestFirst, ownMessages(batch, l.botID)...)
		before = batch[len(batch)-1].ID
		if len(batch) < pageSize {
			break
		}
	}
	oldestFirst := make([]model.Message, len(newestFirst))
	for i, m := range newestFirst {
		oldestFirst[len(newestFirst)-1-i] = m
	}
	return oldestFirst, nil
}

// UpdateEveryone sets @everyone's server-wide permissions. @everyone is the role
// whose ID equals the guild ID.
//
// Discord does not let a bot change permissions it does not hold, and a new
// server's @everyone holds some the bot lacks, so the first apply can get a 403
// here. That case gets a message naming the bits the user has to change by hand
// (see the launch checklist). It is checked before the error is flattened.
func (l *Live) UpdateEveryone(ctx context.Context, permissions int64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	p := permissions
	_, err := l.s.GuildRoleEdit(l.guildID, l.guildID, &discordgo.RoleParams{Permissions: &p}, opts(ctx)...)
	if err != nil && ctx.Err() == nil && isForbidden(err) {
		msg := everyoneForbiddenMessage(l.everyone, permissions, l.everyoneKnown)
		return fmt.Errorf("discord: update @everyone: %w", scrubToken(errors.New(msg), l.token))
	}
	return l.fail(ctx, "update @everyone", err)
}

// CreateRole creates the role with every field sent explicitly.
func (l *Live) CreateRole(ctx context.Context, r model.Role) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	created, err := l.s.GuildRoleCreate(l.guildID, toRoleParams(r), opts(ctx)...)
	if err != nil {
		return l.fail(ctx, "create role "+r.Name, err)
	}
	if _, ok := l.roleIDs[r.Name]; !ok {
		l.roleIDs[r.Name] = created.ID
	}
	return nil
}

// UpdateRole updates the role matched by name, sending every field.
func (l *Live) UpdateRole(ctx context.Context, r model.Role) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	id, ok := l.roleIDs[r.Name]
	if !ok {
		return fmt.Errorf("discord: update role %q: not found", r.Name)
	}
	_, err := l.s.GuildRoleEdit(l.guildID, id, toRoleParams(r), opts(ctx)...)
	return l.fail(ctx, "update role "+r.Name, err)
}

// ReorderRoles places the named roles at positions provisioner-1, -2, ... The
// provisioner's current position is read from Discord just before, because
// creating roles shifts positions. The request is built by hand with only id and
// position: discordgo.GuildRoleReorder (restapi.go:1147) marshals whole Role
// objects, and Discord documents only those two fields for this endpoint.
func (l *Live) ReorderRoles(ctx context.Context, namesHighestFirst []string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if l.provisioner == "" {
		return errors.New("discord: reorder roles: provisioner role not set (SetProvisionerRole)")
	}
	provID, ok := l.roleIDs[l.provisioner]
	if !ok {
		return fmt.Errorf("discord: reorder roles: provisioner role %q not found", l.provisioner)
	}
	ids := make([]string, len(namesHighestFirst))
	for i, name := range namesHighestFirst {
		id, ok := l.roleIDs[name]
		switch {
		case !ok:
			return fmt.Errorf("discord: reorder roles: role %q not found", name)
		case id == provID:
			return fmt.Errorf("discord: reorder roles: cannot move the provisioner role %q", name)
		}
		ids[i] = id
	}
	if len(ids) == 0 {
		return nil
	}

	roles, err := l.s.GuildRoles(l.guildID, opts(ctx)...)
	if err != nil {
		return l.fail(ctx, "list roles", err)
	}
	provPos := -1
	for _, r := range roles {
		if r != nil && r.ID == provID {
			provPos = r.Position
		}
	}
	if provPos < 0 {
		return fmt.Errorf("discord: reorder roles: provisioner role %q not found", l.provisioner)
	}
	payload, err := rolePositions(provPos, ids)
	if err != nil {
		return fmt.Errorf("discord: reorder roles: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	url := discordgo.EndpointGuildRoles(l.guildID)
	_, err = l.s.RequestWithBucketID("PATCH", url, payload, url, opts(ctx)...)
	return l.fail(ctx, "reorder roles", err)
}

// DeleteRole deletes by r.ID, or by name through the cache when r.ID is empty.
// It refuses to delete @everyone or the provisioner role.
func (l *Live) DeleteRole(ctx context.Context, r model.Role) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	id := r.ID
	if id == "" {
		var ok bool
		if id, ok = l.roleIDs[r.Name]; !ok {
			return fmt.Errorf("discord: delete role %q: not found", r.Name)
		}
	}
	if id == l.guildID {
		return errors.New("discord: refusing to delete the @everyone role")
	}
	if l.provisioner != "" && (r.Name == l.provisioner || id == l.roleIDs[l.provisioner]) {
		return fmt.Errorf("discord: refusing to delete the provisioner role %q", l.provisioner)
	}
	if err := l.s.GuildRoleDelete(l.guildID, id, opts(ctx)...); err != nil {
		return l.fail(ctx, "delete role "+r.Name, err)
	}
	for name, cached := range l.roleIDs {
		if cached == id {
			delete(l.roleIDs, name)
		}
	}
	return nil
}

// parentID resolves a channel's category name to its ID ("" for none).
func (l *Live) parentID(c model.Channel) (string, error) {
	if c.Parent == "" || c.Type == model.Category {
		return "", nil
	}
	id, ok := l.channelIDs[chanKey{model.Category, c.Parent}]
	if !ok {
		return "", fmt.Errorf("parent category %q not found", c.Parent)
	}
	return id, nil
}

func discordType(t model.ChannelType) discordgo.ChannelType {
	switch t {
	case model.Voice:
		return discordgo.ChannelTypeGuildVoice
	case model.Category:
		return discordgo.ChannelTypeGuildCategory
	default:
		return discordgo.ChannelTypeGuildText
	}
}

// CreateChannel creates the channel with its parent, topic (text only) and the
// full overwrite set, then writes its position.
func (l *Live) CreateChannel(ctx context.Context, c model.Channel) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	parent, err := l.parentID(c)
	if err != nil {
		return fmt.Errorf("discord: create channel %q: %w", c.Name, err)
	}
	ows, err := toDiscordOverwrites(c.Overwrites, l.roleIDs, l.guildID)
	if err != nil {
		return fmt.Errorf("discord: create channel %q: %w", c.Name, err)
	}
	data := discordgo.GuildChannelCreateData{
		Name:                 c.Name,
		Type:                 discordType(c.Type),
		ParentID:             parent,
		PermissionOverwrites: ows,
	}
	if c.Type == model.Text {
		data.Topic = c.Topic
	}
	created, err := l.s.GuildChannelCreateComplex(l.guildID, data, opts(ctx)...)
	if err != nil {
		return l.fail(ctx, "create channel "+c.Name, err)
	}
	key := chanKey{c.Type, c.Name}
	if _, ok := l.channelIDs[key]; !ok {
		l.channelIDs[key] = created.ID
	}
	return l.writePosition(ctx, created.ID, c.Position)
}

// UpdateChannel replaces the parent, topic and overwrites of the channel matched
// by (Type, Name), then writes its position.
func (l *Live) UpdateChannel(ctx context.Context, c model.Channel) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	id, ok := l.channelIDs[chanKey{c.Type, c.Name}]
	if !ok {
		return fmt.Errorf("discord: update channel %q: not found", c.Name)
	}
	parent, err := l.parentID(c)
	if err != nil {
		return fmt.Errorf("discord: update channel %q: %w", c.Name, err)
	}
	ows, err := toDiscordOverwrites(c.Overwrites, l.roleIDs, l.guildID)
	if err != nil {
		return fmt.Errorf("discord: update channel %q: %w", c.Name, err)
	}
	url := discordgo.EndpointChannel(id)
	_, err = l.s.RequestWithBucketID("PATCH", url, channelPatchBody(c, parent, ows), url, opts(ctx)...)
	if err != nil {
		return l.fail(ctx, "update channel "+c.Name, err)
	}
	return l.writePosition(ctx, id, c.Position)
}

// writePosition puts the channel at index among its siblings and renumbers the
// whole sibling list 0..n-1 through the bulk endpoint, skipping the write when
// nothing would change. The siblings are read fresh, because the create or edit
// that came just before may already have moved them.
//
// What GuildChannelsReorder puts on the wire (discordgo v0.29.0
// restapi.go:1067-1081): an array of {"id", "position"} and nothing else. It
// never serialises parent_id, so the bulk write cannot detach a channel from its
// category; parents change only through the PATCH in UpdateChannel.
//
// Known limitation: with unmanaged strays inside a managed category, the
// relative order of the managed channels is only guaranteed for the channels
// that get rewritten. Uncategorised non-category channels are not siblings of
// categories here, matching how reconcile groups them.
func (l *Live) writePosition(ctx context.Context, id string, index int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	all, err := l.s.GuildChannels(l.guildID, opts(ctx)...)
	if err != nil {
		return l.fail(ctx, "list channels", err)
	}
	var target *discordgo.Channel
	for _, c := range all {
		if c.ID == id {
			target = c
		}
	}
	if target == nil {
		return fmt.Errorf("discord: set position: channel %s not found", id)
	}
	var siblings []*discordgo.Channel
	for _, c := range all {
		isCat := c.Type == discordgo.ChannelTypeGuildCategory
		if isCat == (target.Type == discordgo.ChannelTypeGuildCategory) && c.ParentID == target.ParentID {
			siblings = append(siblings, c)
		}
	}
	payload, err := siblingPayload(siblings, id, index)
	if err != nil {
		return fmt.Errorf("discord: set position: %w", err)
	}
	if !positionsDiffer(siblings, payload) {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return l.fail(ctx, "set channel positions", l.s.GuildChannelsReorder(l.guildID, payload, opts(ctx)...))
}

// DeleteChannel deletes by c.ID, or by (Type, Name) through the cache when c.ID
// is empty.
func (l *Live) DeleteChannel(ctx context.Context, c model.Channel) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	id := c.ID
	if id == "" {
		var ok bool
		if id, ok = l.channelIDs[chanKey{c.Type, c.Name}]; !ok {
			return fmt.Errorf("discord: delete channel %q: not found", c.Name)
		}
	}
	if _, err := l.s.ChannelDelete(id, opts(ctx)...); err != nil {
		return l.fail(ctx, "delete channel "+c.Name, err)
	}
	for k, cached := range l.channelIDs {
		if cached == id {
			delete(l.channelIDs, k)
		}
	}
	return nil
}

func (l *Live) textChannelID(channel string) (string, error) {
	id, ok := l.channelIDs[chanKey{model.Text, channel}]
	if !ok {
		return "", fmt.Errorf("text channel %q not found", channel)
	}
	return id, nil
}

// PostMessage posts content to the managed text channel with that name. No
// mentions are parsed.
func (l *Live) PostMessage(ctx context.Context, channel, content string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	id, err := l.textChannelID(channel)
	if err != nil {
		return fmt.Errorf("discord: post message: %w", err)
	}
	_, err = l.s.ChannelMessageSendComplex(id, &discordgo.MessageSend{Content: content, AllowedMentions: noMentions()}, opts(ctx)...)
	return l.fail(ctx, "post message in #"+channel, err)
}

// EditMessage replaces the content of a message. No mentions are parsed.
func (l *Live) EditMessage(ctx context.Context, channel, messageID, content string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	id, err := l.textChannelID(channel)
	if err != nil {
		return fmt.Errorf("discord: edit message: %w", err)
	}
	edit := discordgo.NewMessageEdit(id, messageID).SetContent(content)
	edit.AllowedMentions = noMentions()
	_, err = l.s.ChannelMessageEditComplex(edit, opts(ctx)...)
	return l.fail(ctx, "edit message in #"+channel, err)
}

// DeleteMessage deletes a message.
func (l *Live) DeleteMessage(ctx context.Context, channel, messageID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	id, err := l.textChannelID(channel)
	if err != nil {
		return fmt.Errorf("discord: delete message: %w", err)
	}
	return l.fail(ctx, "delete message in #"+channel, l.s.ChannelMessageDelete(id, messageID, opts(ctx)...))
}
