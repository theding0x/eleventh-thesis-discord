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
	Role        model.Role    // Create/UpdateRole: desired values; DeleteRole: Name
	RoleOrder   []string      // ReorderRoles: managed role names, highest first
	Channel     model.Channel // Create/Update/DeleteChannel: desired values (Delete: Name, Type)
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
	Unmanaged []string // "role X", "category X", "text #x", "voice X"
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
		return fmt.Errorf("provisioner role %q not found in the server: invite the bot with that role, then drag the bot's role above the roles the spec manages", d.ProvisionerRole)
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
		p.Unmanaged = append(p.Unmanaged, "role "+r.Name)
	}
	for _, c := range channels {
		p.Unmanaged = append(p.Unmanaged, channelLabel(c))
	}

	if prune {
		// Channels first, then categories (which must be empty), then roles.
		for _, c := range channels {
			if c.Type != model.Category {
				p.Actions = append(p.Actions, Action{Kind: DeleteChannel, Channel: model.Channel{Name: c.Name, Type: c.Type}})
			}
		}
		for _, c := range channels {
			if c.Type == model.Category {
				p.Actions = append(p.Actions, Action{Kind: DeleteChannel, Channel: model.Channel{Name: c.Name, Type: c.Type}})
			}
		}
		for _, r := range roles {
			p.Actions = append(p.Actions, Action{Kind: DeleteRole, Role: model.Role{Name: r.Name}})
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
// them would make every run reorder.
func roleOrderDrifted(d Desired, g model.Guild) bool {
	provPos := math.MaxInt
	for _, r := range g.Roles {
		if r.Name == d.ProvisionerRole {
			provPos = r.Position
			break
		}
	}

	var below []model.Role
	for _, r := range g.Roles {
		if r.Managed || r.Name == d.ProvisionerRole || r.Position >= provPos {
			continue
		}
		below = append(below, r)
	}
	sort.Slice(below, func(i, j int) bool {
		if below[i].Position != below[j].Position {
			return below[i].Position > below[j].Position
		}
		return below[i].Name < below[j].Name
	})

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

// reconcileChannels creates missing categories and channels and updates
// drifted ones. d.Channels already lists categories first.
func reconcileChannels(d Desired, g model.Guild) []Action {
	observed := make(map[channelKey]model.Channel, len(g.Channels))
	for _, c := range g.Channels {
		k := channelKey{c.Type, c.Name}
		if _, ok := observed[k]; !ok {
			observed[k] = c
		}
	}

	var actions []Action
	for _, want := range d.Channels {
		have, ok := observed[channelKey{want.Type, want.Name}]
		if !ok {
			actions = append(actions, Action{Kind: CreateChannel, Channel: want})
			continue
		}
		var changes []string
		if have.Parent != want.Parent {
			changes = append(changes, "parent")
		}
		if have.Position != want.Position {
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

// unmanaged returns the observed roles and channels the spec doesn't mention,
// in observed order. The provisioner role and integration-managed roles are
// never unmanaged.
func unmanaged(d Desired, g model.Guild) ([]model.Role, []model.Channel) {
	wantRole := make(map[string]bool, len(d.Roles)+1)
	wantRole[d.ProvisionerRole] = true
	for _, r := range d.Roles {
		wantRole[r.Name] = true
	}
	wantChannel := make(map[channelKey]bool, len(d.Channels))
	for _, c := range d.Channels {
		wantChannel[channelKey{c.Type, c.Name}] = true
	}

	var roles []model.Role
	for _, r := range g.Roles {
		if !r.Managed && !wantRole[r.Name] {
			roles = append(roles, r)
		}
	}
	var channels []model.Channel
	for _, c := range g.Channels {
		if !wantChannel[channelKey{c.Type, c.Name}] {
			channels = append(channels, c)
		}
	}
	return roles, channels
}

// reconcileContent plans the message actions for channels with content.
// Task 6 fills this in.
func reconcileContent(d Desired, g model.Guild) []Action {
	return nil
}
