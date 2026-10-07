package reconcile

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/theding0x/eleventh-thesis-discord/internal/model"
)

// Kind is the type of a planned action.
type Kind string

// Action kinds.
const (
	UpdateEveryone Kind = "update-everyone"
	CreateRole     Kind = "create-role"
	UpdateRole     Kind = "update-role"
	ReorderRoles   Kind = "reorder-roles"
	DeleteRole     Kind = "delete-role"
	CreateChannel  Kind = "create-channel"
	UpdateChannel  Kind = "update-channel"
	DeleteChannel  Kind = "delete-channel"
	PostMessage    Kind = "post-message"
	EditMessage    Kind = "edit-message"
	DeleteMessage  Kind = "delete-message"
)

// Action is one step of a plan.
type Action struct {
	Kind        Kind
	Permissions int64         // UpdateEveryone: desired @everyone permissions
	Role        model.Role    // Create/UpdateRole: desired values; DeleteRole: the observed role (with its ID)
	RoleOrder   []string      // ReorderRoles: managed role names, highest first
	Channel     model.Channel // Create/UpdateChannel: desired values; DeleteChannel: the observed channel (with its ID)
	Message     model.Message // Edit/DeleteMessage: ID; Post/Edit: Content
	Target      string        // message actions: channel name
	Index       int           // message actions: 1-based chunk number
	Total       int           // message actions: total chunks
	Changes     []string      // Update*: changed fields, e.g. "permissions", "overwrites"
}

// channelLabel renders a channel as "text #x", "voice X" or "category X".
func channelLabel(c model.Channel) string {
	switch c.Type {
	case model.Category:
		return "category " + c.Name
	case model.Voice:
		return "voice " + c.Name
	default:
		return "text #" + c.Name
	}
}

// String renders the action as one plan line.
func (a Action) String() string {
	switch a.Kind {
	case UpdateEveryone:
		return "~ update @everyone permissions"
	case CreateRole:
		return "+ create role " + a.Role.Name
	case UpdateRole:
		return fmt.Sprintf("~ update role %s (%s)", a.Role.Name, strings.Join(a.Changes, ", "))
	case ReorderRoles:
		return "~ reorder roles: " + strings.Join(a.RoleOrder, ", ")
	case DeleteRole:
		return "- delete role " + a.Role.Name
	case CreateChannel:
		s := "+ create " + channelLabel(a.Channel)
		if a.Channel.Parent != "" {
			s += " in " + a.Channel.Parent
		}
		return s
	case UpdateChannel:
		return fmt.Sprintf("~ update %s (%s)", channelLabel(a.Channel), strings.Join(a.Changes, ", "))
	case DeleteChannel:
		return "- delete " + channelLabel(a.Channel)
	case PostMessage:
		return fmt.Sprintf("+ post message %d/%d in #%s", a.Index, a.Total, a.Target)
	case EditMessage:
		return fmt.Sprintf("~ edit message %d/%d in #%s", a.Index, a.Total, a.Target)
	case DeleteMessage:
		return "- delete message in #" + a.Target
	default:
		return string(a.Kind)
	}
}

// Plan is the ordered list of actions that makes the guild match the spec,
// plus the objects the spec doesn't mention.
type Plan struct {
	Actions   []Action
	Unmanaged []string // "role X", "category X", "text #x", "voice X"; later same-name objects end in " (duplicate)"
}

// String renders the plan: one line per action, then the unmanaged objects
// under an "unmanaged:" line, or "no changes". Lines are joined by newlines,
// with none after the last.
func (p Plan) String() string {
	if len(p.Actions) == 0 && len(p.Unmanaged) == 0 {
		return "no changes"
	}
	lines := make([]string, 0, len(p.Actions)+len(p.Unmanaged)+1)
	for _, a := range p.Actions {
		lines = append(lines, a.String())
	}
	if len(p.Unmanaged) > 0 {
		lines = append(lines, "unmanaged:")
		for _, u := range p.Unmanaged {
			lines = append(lines, "  "+u)
		}
	}
	return strings.Join(lines, "\n")
}

// Check verifies that the provisioner role exists and sits above every role
// the spec manages, so that planning fails early instead of with a 403 partway
// through an apply.
func Check(g model.Guild, d Desired) error {
	var prov *model.Role
	for i := range g.Roles {
		if g.Roles[i].Name == d.ProvisionerRole {
			prov = &g.Roles[i]
			break
		}
	}
	if prov == nil {
		return fmt.Errorf("no role named %q: Discord names the bot's role after the bot — rename it (Server Settings → Roles) or set provisioner_role in server.yaml", d.ProvisionerRole)
	}

	var tooHigh []string
	for _, r := range d.Roles { // spec order, so the message is deterministic
		for _, have := range g.Roles {
			if have.Name == r.Name && have.Position >= prov.Position {
				tooHigh = append(tooHigh, r.Name)
				break
			}
		}
	}
	if len(tooHigh) > 0 {
		return fmt.Errorf("role %q is not above the roles the spec manages (%s): drag the bot's role above them in Server Settings > Roles", d.ProvisionerRole, strings.Join(tooHigh, ", "))
	}
	return nil
}

// Reconcile compares the desired state with the observed guild and returns the
// ordered plan. It is pure: it does not modify its inputs, and its output is
// deterministic. With prune, unmanaged roles and channels are also deleted.
func Reconcile(d Desired, g model.Guild, prune bool) Plan {
	var p Plan

	if g.EveryonePermissions != d.EveryonePermissions {
		p.Actions = append(p.Actions, Action{Kind: UpdateEveryone, Permissions: d.EveryonePermissions})
	}
	p.Actions = append(p.Actions, reconcileRoles(d, g)...)
	p.Actions = append(p.Actions, reconcileChannels(d, g)...)
	p.Actions = append(p.Actions, reconcileContent(d, g)...)

	roles, channels := unmanaged(d, g)
	for _, r := range roles {
		p.Unmanaged = append(p.Unmanaged, "role "+r.Name+r.suffix())
	}
	for _, c := range channels {
		p.Unmanaged = append(p.Unmanaged, channelLabel(c.Channel)+c.suffix())
	}

	if prune {
		// Channels first, then categories (which must be empty), then roles.
		// The delete actions carry the observed objects, so a duplicate is
		// deleted by ID and never confused with the managed object.
		for _, c := range channels {
			if c.Type != model.Category {
				p.Actions = append(p.Actions, Action{Kind: DeleteChannel, Channel: c.Channel})
			}
		}
		for _, c := range channels {
			if c.Type == model.Category {
				p.Actions = append(p.Actions, Action{Kind: DeleteChannel, Channel: c.Channel})
			}
		}
		for _, r := range roles {
			p.Actions = append(p.Actions, Action{Kind: DeleteRole, Role: r.Role})
		}
	}
	return p
}

// reconcileRoles creates missing roles, updates drifted ones (both in spec
// order) and reorders when needed.
func reconcileRoles(d Desired, g model.Guild) []Action {
	observed := make(map[string]model.Role, len(g.Roles))
	for _, r := range g.Roles {
		if _, ok := observed[r.Name]; !ok {
			observed[r.Name] = r
		}
	}

	var actions []Action
	created := false
	for _, want := range d.Roles {
		have, ok := observed[want.Name]
		if !ok {
			created = true
			actions = append(actions, Action{Kind: CreateRole, Role: want})
			continue
		}
		var changes []string
		if have.Color != want.Color {
			changes = append(changes, "color")
		}
		if have.Hoist != want.Hoist {
			changes = append(changes, "hoist")
		}
		if have.Mentionable != want.Mentionable {
			changes = append(changes, "mentionable")
		}
		if have.Permissions != want.Permissions {
			changes = append(changes, "permissions")
		}
		if len(changes) > 0 {
			actions = append(actions, Action{Kind: UpdateRole, Role: want, Changes: changes})
		}
	}

	if created || roleOrderDrifted(d, g) {
		order := make([]string, len(d.Roles))
		for i, r := range d.Roles {
			order[i] = r.Name
		}
		actions = append(actions, Action{Kind: ReorderRoles, RoleOrder: order})
	}
	return actions
}

// roleOrderDrifted reports whether the roles directly below the provisioner
// are not the spec's roles in spec order. Roles managed by integrations and the
// provisioner itself are left out: they are never repositioned, so counting
// them would make every run reorder. So are later duplicates of a spec role
// name (the first observed one is the managed one), which are reported as
// unmanaged instead.
func roleOrderDrifted(d Desired, g model.Guild) bool {
	provPos := math.MaxInt
	for _, r := range g.Roles {
		if r.Name == d.ProvisionerRole {
			provPos = r.Position
			break
		}
	}

	wanted := make(map[string]bool, len(d.Roles))
	for _, r := range d.Roles {
		wanted[r.Name] = true
	}
	seen := make(map[string]bool, len(d.Roles))
	var below []model.Role
	for _, r := range g.Roles {
		duplicate := wanted[r.Name] && seen[r.Name]
		seen[r.Name] = true
		if r.Managed || duplicate || r.Name == d.ProvisionerRole || r.Position >= provPos {
			continue
		}
		below = append(below, r)
	}
	// Stable on Position only: g.Roles is in Observe order (ID ascending among
	// equal positions), and ties must keep that order.
	sort.SliceStable(below, func(i, j int) bool { return below[i].Position > below[j].Position })

	if len(below) < len(d.Roles) {
		return true
	}
	for i, want := range d.Roles {
		if below[i].Name != want.Name {
			return true
		}
	}
	return false
}

type channelKey struct {
	typ  model.ChannelType
	name string
}

// managedChannels maps each wanted (type, name) to the index in g.Channels of
// the observed channel that is managed: the first one in observed order. Later
// channels with the same key are duplicates.
func managedChannels(d Desired, g model.Guild) map[channelKey]int {
	wanted := make(map[channelKey]bool, len(d.Channels))
	for _, c := range d.Channels {
		wanted[channelKey{c.Type, c.Name}] = true
	}
	first := make(map[channelKey]int, len(d.Channels))
	for i, c := range g.Channels {
		k := channelKey{c.Type, c.Name}
		if _, ok := first[k]; ok || !wanted[k] {
			continue
		}
		first[k] = i
	}
	return first
}

type siblingGroup struct {
	category bool
	parent   string
}

// siblingRanks returns two ranks for the managed channels that already sit
// under their desired parent: obs is the rank of each (by index into
// g.Channels) among those siblings in the order Discord shows them (observed
// Position, then observed order), and want is the rank of each (by key) among
// the same siblings in the desired order.
//
// Only these channels take part, so a position is reported as drift only when
// the managed siblings are in a different relative order. An unmanaged stray or
// duplicate, a channel still to be created and a channel that is under the
// wrong parent (reported as a parent change) don't shift anyone else's rank.
func siblingRanks(d Desired, g model.Guild, managed map[channelKey]int) (obs map[int]int, want map[channelKey]int) {
	inPlace := make(map[int]bool, len(managed))
	for _, w := range d.Channels {
		k := channelKey{w.Type, w.Name}
		if i, ok := managed[k]; ok && g.Channels[i].Parent == w.Parent {
			inPlace[i] = true
		}
	}

	observed := map[siblingGroup][]int{}
	for i, c := range g.Channels {
		if inPlace[i] {
			k := siblingGroup{c.Type == model.Category, c.Parent}
			observed[k] = append(observed[k], i)
		}
	}
	obs = make(map[int]int, len(inPlace))
	for _, idxs := range observed {
		sort.SliceStable(idxs, func(a, b int) bool { return g.Channels[idxs[a]].Position < g.Channels[idxs[b]].Position })
		for rank, i := range idxs {
			obs[i] = rank
		}
	}

	want = make(map[channelKey]int, len(inPlace))
	next := map[siblingGroup]int{}
	for _, w := range d.Channels { // desired order, so the ranks follow it
		k := channelKey{w.Type, w.Name}
		if i, ok := managed[k]; ok && inPlace[i] {
			grp := siblingGroup{w.Type == model.Category, w.Parent}
			want[k] = next[grp]
			next[grp]++
		}
	}
	return obs, want
}

// reconcileChannels creates missing categories and channels and updates
// drifted ones. d.Channels already lists categories first.
//
// Position is compared as relative order among the managed siblings (see
// siblingRanks), and only when the parent already matches: a channel that has
// to move is re-ranked by the move itself. The update action still carries the
// desired Position.
func reconcileChannels(d Desired, g model.Guild) []Action {
	managed := managedChannels(d, g)
	obsRank, wantRank := siblingRanks(d, g, managed)

	var actions []Action
	for _, want := range d.Channels {
		key := channelKey{want.Type, want.Name}
		idx, ok := managed[key]
		if !ok {
			actions = append(actions, Action{Kind: CreateChannel, Channel: want})
			continue
		}
		have := g.Channels[idx]
		var changes []string
		if have.Parent != want.Parent {
			changes = append(changes, "parent")
		} else if obsRank[idx] != wantRank[key] {
			changes = append(changes, "position")
		}
		if want.Type == model.Text && have.Topic != want.Topic {
			changes = append(changes, "topic")
		}
		if !sameOverwrites(have.Overwrites, want.Overwrites) {
			changes = append(changes, "overwrites")
		}
		if len(changes) > 0 {
			actions = append(actions, Action{Kind: UpdateChannel, Channel: want, Changes: changes})
		}
	}
	return actions
}

// sameOverwrites compares two overwrite sets exactly, ignoring order. It sorts
// copies and leaves its arguments alone.
func sameOverwrites(a, b []model.Overwrite) bool {
	if len(a) != len(b) {
		return false
	}
	a = sortedOverwrites(a)
	b = sortedOverwrites(b)
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func sortedOverwrites(in []model.Overwrite) []model.Overwrite {
	out := append([]model.Overwrite(nil), in...)
	sort.Slice(out, func(i, j int) bool { return out[i].Target < out[j].Target })
	return out
}

// unmanagedRole is an observed role the spec doesn't manage. duplicate marks a
// later role that shares its name with a managed one.
type unmanagedRole struct {
	model.Role
	duplicate bool
}

func (r unmanagedRole) suffix() string { return dupSuffix(r.duplicate) }

// unmanagedChannel is the channel counterpart of unmanagedRole.
type unmanagedChannel struct {
	model.Channel
	duplicate bool
}

func (c unmanagedChannel) suffix() string { return dupSuffix(c.duplicate) }

func dupSuffix(duplicate bool) string {
	if duplicate {
		return " (duplicate)"
	}
	return ""
}

// unmanaged returns the observed roles and channels that are not managed, in
// observed order: those the spec doesn't mention, and later duplicates of ones
// it does (the first observed object of a name is the managed one). The
// provisioner role and integration-managed roles are never unmanaged.
func unmanaged(d Desired, g model.Guild) ([]unmanagedRole, []unmanagedChannel) {
	wantRole := make(map[string]bool, len(d.Roles))
	for _, r := range d.Roles {
		wantRole[r.Name] = true
	}
	seen := make(map[string]bool, len(d.Roles))
	var roles []unmanagedRole
	for _, r := range g.Roles {
		duplicate := wantRole[r.Name] && seen[r.Name]
		seen[r.Name] = true
		switch {
		case r.Managed || r.Name == d.ProvisionerRole:
		case !wantRole[r.Name]:
			roles = append(roles, unmanagedRole{Role: r})
		case duplicate:
			roles = append(roles, unmanagedRole{Role: r, duplicate: true})
		}
	}

	wantChannel := make(map[channelKey]bool, len(d.Channels))
	for _, c := range d.Channels {
		wantChannel[channelKey{c.Type, c.Name}] = true
	}
	managed := managedChannels(d, g)
	var channels []unmanagedChannel
	for i, c := range g.Channels {
		k := channelKey{c.Type, c.Name}
		switch {
		case !wantChannel[k]:
			channels = append(channels, unmanagedChannel{Channel: c})
		case managed[k] != i:
			channels = append(channels, unmanagedChannel{Channel: c, duplicate: true})
		}
	}
	return roles, channels
}

// reconcileContent plans the bot's own messages in each text channel that has
// content, in d.Channels order. Observed messages are matched to chunks by
// position: a differing one is edited, missing chunks are posted in order and
// surplus messages are deleted, after the edits and posts. An entry with no
// chunks therefore deletes every bot message in the channel. g.BotMessages only
// holds the bot's own messages, so other users' messages are never touched.
func reconcileContent(d Desired, g model.Guild) []Action {
	var actions []Action
	for _, c := range d.Channels {
		if c.Type != model.Text {
			continue
		}
		chunks, ok := d.Content[c.Name]
		if !ok {
			continue
		}
		observed := g.BotMessages[c.Name]
		total := len(chunks)

		for i, chunk := range chunks {
			switch {
			case i >= len(observed):
				actions = append(actions, Action{Kind: PostMessage, Message: model.Message{Content: chunk}, Target: c.Name, Index: i + 1, Total: total})
			case observed[i].Content != chunk:
				actions = append(actions, Action{Kind: EditMessage, Message: model.Message{ID: observed[i].ID, Content: chunk}, Target: c.Name, Index: i + 1, Total: total})
			}
		}
		for i := total; i < len(observed); i++ {
			actions = append(actions, Action{Kind: DeleteMessage, Message: model.Message{ID: observed[i].ID}, Target: c.Name, Index: i + 1, Total: total})
		}
	}
	return actions
}
