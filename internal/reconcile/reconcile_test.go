package reconcile

import (
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/theding0x/eleventh-thesis-discord/internal/model"
)

// cloneGuild deep-copies a guild, including the overwrite slices.
func cloneGuild(g model.Guild) model.Guild {
	out := g
	out.Roles = append([]model.Role(nil), g.Roles...)
	out.Channels = make([]model.Channel, len(g.Channels))
	for i, c := range g.Channels {
		c.Overwrites = append([]model.Overwrite(nil), c.Overwrites...)
		out.Channels[i] = c
	}
	if g.BotMessages != nil {
		out.BotMessages = make(map[string][]model.Message, len(g.BotMessages))
		for k, v := range g.BotMessages {
			out.BotMessages[k] = append([]model.Message(nil), v...)
		}
	}
	return out
}

// converged builds the observed state an applied guild would have: the
// provisioner at position 10, the managed roles at 9, 8, 7, the channels as
// desired and the bot's messages equal to the chunks.
func converged(d Desired) model.Guild {
	g := model.Guild{
		EveryonePermissions: d.EveryonePermissions,
		BotMessages:         map[string][]model.Message{},
	}
	g.Roles = append(g.Roles, model.Role{ID: "r-prov", Name: d.ProvisionerRole, Position: 10})
	for i, r := range d.Roles {
		r.ID = "r-" + r.Name
		r.Position = 9 - i
		g.Roles = append(g.Roles, r)
	}
	for _, c := range d.Channels {
		c.ID = "c-" + c.Name
		c.Overwrites = append([]model.Overwrite(nil), c.Overwrites...)
		g.Channels = append(g.Channels, c)
	}
	for name, chunks := range d.Content {
		for i, chunk := range chunks {
			g.BotMessages[name] = append(g.BotMessages[name], model.Message{ID: name + "-m" + strconv.Itoa(i), Content: chunk})
		}
	}
	return g
}

// strs renders a plan's actions.
func strs(p Plan) []string {
	var out []string
	for _, a := range p.Actions {
		out = append(out, a.String())
	}
	return out
}

func mutateRole(g *model.Guild, name string, f func(*model.Role)) {
	for i := range g.Roles {
		if g.Roles[i].Name == name {
			f(&g.Roles[i])
			return
		}
	}
	panic("no role " + name)
}

func mutateChannel(g *model.Guild, typ model.ChannelType, name string, f func(*model.Channel)) {
	for i := range g.Channels {
		if g.Channels[i].Type == typ && g.Channels[i].Name == name {
			f(&g.Channels[i])
			return
		}
	}
	panic("no channel " + name)
}

func TestReconcile(t *testing.T) {
	d := mustDesired(t)
	everyoneDefaults := mustParse(t, "view_channel", "send_messages", "read_message_history")

	tests := []struct {
		name          string
		observed      func(t *testing.T) model.Guild
		prune         bool
		wantActions   []string // exact, in order
		wantUnmanaged []string
		check         func(t *testing.T, p Plan)
	}{
		{
			name: "empty guild",
			observed: func(t *testing.T) model.Guild {
				return model.Guild{
					EveryonePermissions: everyoneDefaults,
					Roles:               []model.Role{{ID: "r-prov", Name: "Provisioner", Position: 1}},
				}
			},
			wantActions: []string{
				"~ update @everyone permissions",
				"+ create role Director",
				"+ create role Member",
				"+ create role Probation",
				"~ reorder roles: Director, Member, Probation",
				"+ create category Front Door",
				"+ create category The Collective",
				"+ create category Operations",
				"+ create category Directorate",
				"+ create text #welcome in Front Door",
				"+ create text #constitution in Front Door",
				"+ create text #open-ledger in Front Door",
				"+ create text #apply in Front Door",
				"+ create text #public-chat in Front Door",
				"+ create text #announcements in The Collective",
				"+ create text #general in The Collective",
				"+ create text #contributions in The Collective",
				"+ create text #production in The Collective",
				"+ create text #pi-and-mining in The Collective",
				"+ create text #fits-and-skills in The Collective",
				"+ create text #amendments in The Collective",
				"+ create voice Comms in The Collective",
				"+ create text #logistics in Operations",
				"+ create text #vetting in Directorate",
				"+ create text #directors in Directorate",
			},
			check: func(t *testing.T, p Plan) {
				if len(p.Actions) != 25 {
					t.Fatalf("len(Actions) = %d, want 25", len(p.Actions))
				}
				if a := p.Actions[0]; a.Kind != UpdateEveryone || a.Permissions != d.EveryonePermissions {
					t.Errorf("first action = %+v, want update-everyone to %b", a, d.EveryonePermissions)
				}
				if a := p.Actions[4]; a.Kind != ReorderRoles || !reflect.DeepEqual(a.RoleOrder, []string{"Director", "Member", "Probation"}) {
					t.Errorf("action 4 = %+v, want reorder-roles", a)
				}
				// Create actions carry the desired values.
				for i, a := range p.Actions[5:] {
					if a.Kind != CreateChannel {
						t.Fatalf("action %d = %v, want create-channel", i+5, a.Kind)
					}
					if !reflect.DeepEqual(a.Channel, d.Channels[i]) {
						t.Errorf("action %d channel = %+v, want %+v", i+5, a.Channel, d.Channels[i])
					}
				}
				if a := p.Actions[1]; !reflect.DeepEqual(a.Role, d.Roles[0]) {
					t.Errorf("create Director role = %+v, want %+v", a.Role, d.Roles[0])
				}
			},
		},
		{
			name:     "converged",
			observed: func(t *testing.T) model.Guild { return converged(d) },
			check: func(t *testing.T, p Plan) {
				if len(p.Actions) != 0 || len(p.Unmanaged) != 0 {
					t.Errorf("plan = %+v, want empty", p)
				}
				if got := p.String(); got != "no changes" {
					t.Errorf("String() = %q, want %q", got, "no changes")
				}
			},
		},
		{
			name: "everyone drift",
			observed: func(t *testing.T) model.Guild {
				g := converged(d)
				g.EveryonePermissions |= mustParse(t, "send_messages")
				return g
			},
			wantActions: []string{"~ update @everyone permissions"},
			check: func(t *testing.T, p Plan) {
				if len(p.Actions) != 1 || p.Actions[0].Kind != UpdateEveryone || p.Actions[0].Permissions != d.EveryonePermissions {
					t.Errorf("actions = %+v", p.Actions)
				}
			},
		},
		{
			name: "drifted role permissions",
			observed: func(t *testing.T) model.Guild {
				g := converged(d)
				mutateRole(&g, "Member", func(r *model.Role) { r.Permissions |= mustParse(t, "manage_messages") })
				return g
			},
			wantActions: []string{"~ update role Member (permissions)"},
			check: func(t *testing.T, p Plan) {
				a := p.Actions[0]
				if a.Kind != UpdateRole || a.Role.Name != "Member" || !reflect.DeepEqual(a.Changes, []string{"permissions"}) {
					t.Errorf("action = %+v", a)
				}
				if a.Role.Permissions != 0 {
					t.Errorf("Role.Permissions = %b, want the desired value 0", a.Role.Permissions)
				}
			},
		},
		{
			name: "role changes are listed in a fixed order",
			observed: func(t *testing.T) model.Guild {
				g := converged(d)
				mutateRole(&g, "Director", func(r *model.Role) {
					r.Color++
					r.Hoist = !r.Hoist
					r.Mentionable = !r.Mentionable
					r.Permissions = 0
				})
				return g
			},
			wantActions: []string{"~ update role Director (color, hoist, mentionable, permissions)"},
			check: func(t *testing.T, p Plan) {
				if want := []string{"color", "hoist", "mentionable", "permissions"}; !reflect.DeepEqual(p.Actions[0].Changes, want) {
					t.Errorf("Changes = %v, want %v", p.Actions[0].Changes, want)
				}
			},
		},
		{
			name: "member overwrite is drift",
			observed: func(t *testing.T) model.Guild {
				g := converged(d)
				mutateChannel(&g, model.Text, "general", func(c *model.Channel) {
					c.Overwrites = append(c.Overwrites, model.Overwrite{Target: "member:42", Allow: mustParse(t, "view_channel")})
				})
				return g
			},
			wantActions: []string{"~ update text #general (overwrites)"},
			check: func(t *testing.T, p Plan) {
				a := p.Actions[0]
				if a.Kind != UpdateChannel || a.Channel.Name != "general" || !reflect.DeepEqual(a.Changes, []string{"overwrites"}) {
					t.Errorf("action = %+v", a)
				}
				if !reflect.DeepEqual(a.Channel, findChannel(t, d, model.Text, "general")) {
					t.Errorf("Channel = %+v, want the desired channel", a.Channel)
				}
			},
		},
		{
			name: "overwrite order does not matter",
			observed: func(t *testing.T) model.Guild {
				g := converged(d)
				mutateChannel(&g, model.Text, "general", func(c *model.Channel) {
					o := c.Overwrites
					for i, j := 0, len(o)-1; i < j; i, j = i+1, j-1 {
						o[i], o[j] = o[j], o[i]
					}
				})
				return g
			},
		},
		{
			name: "overwrite value drift",
			observed: func(t *testing.T) model.Guild {
				g := converged(d)
				mutateChannel(&g, model.Text, "announcements", func(c *model.Channel) {
					for i := range c.Overwrites {
						if c.Overwrites[i].Target == "Member" {
							c.Overwrites[i].Allow |= mustParse(t, "send_messages")
						}
					}
				})
				return g
			},
			wantActions: []string{"~ update text #announcements (overwrites)"},
		},
		{
			name: "category overwrite drift",
			observed: func(t *testing.T) model.Guild {
				g := converged(d)
				mutateChannel(&g, model.Category, "Operations", func(c *model.Channel) { c.Overwrites = c.Overwrites[:len(c.Overwrites)-1] })
				return g
			},
			wantActions: []string{"~ update category Operations (overwrites)"},
		},
		{
			name: "moved channel",
			observed: func(t *testing.T) model.Guild {
				g := converged(d)
				mutateChannel(&g, model.Text, "logistics", func(c *model.Channel) { c.Parent = "The Collective" })
				return g
			},
			wantActions: []string{"~ update text #logistics (parent)"},
			check: func(t *testing.T, p Plan) {
				a := p.Actions[0]
				if a.Kind != UpdateChannel || a.Channel.Parent != "Operations" || !reflect.DeepEqual(a.Changes, []string{"parent"}) {
					t.Errorf("action = %+v", a)
				}
			},
		},
		{
			name: "text topic drift",
			observed: func(t *testing.T) model.Guild {
				g := converged(d)
				mutateChannel(&g, model.Text, "contributions", func(c *model.Channel) { c.Topic = "old" })
				return g
			},
			wantActions: []string{"~ update text #contributions (topic)"},
		},
		{
			name: "channel changes are listed in a fixed order",
			observed: func(t *testing.T) model.Guild {
				g := converged(d)
				mutateChannel(&g, model.Text, "general", func(c *model.Channel) { c.Position = 2 })
				mutateChannel(&g, model.Text, "contributions", func(c *model.Channel) {
					c.Position = 1
					c.Topic = "old"
					c.Overwrites = nil
				})
				return g
			},
			wantActions: []string{
				"~ update text #general (position)",
				"~ update text #contributions (position, topic, overwrites)",
			},
		},
		{
			name: "a moved channel is reported as parent, with its other drift",
			observed: func(t *testing.T) model.Guild {
				g := converged(d)
				mutateChannel(&g, model.Text, "contributions", func(c *model.Channel) {
					c.Parent = "Operations"
					c.Position = 9
					c.Topic = "old"
					c.Overwrites = nil
				})
				return g
			},
			wantActions: []string{"~ update text #contributions (parent, topic, overwrites)"},
		},
		{
			name: "voice topic is ignored",
			observed: func(t *testing.T) model.Guild {
				g := converged(d)
				mutateChannel(&g, model.Voice, "Comms", func(c *model.Channel) { c.Topic = "stray" })
				return g
			},
		},
		{
			name: "rename",
			observed: func(t *testing.T) model.Guild {
				g := converged(d)
				mutateChannel(&g, model.Text, "general", func(c *model.Channel) { c.Name = "general-chat" })
				return g
			},
			wantActions:   []string{"+ create text #general in The Collective"},
			wantUnmanaged: []string{"text #general-chat"},
		},
		{
			name: "prune",
			observed: func(t *testing.T) model.Guild {
				g := converged(d)
				mutateChannel(&g, model.Text, "general", func(c *model.Channel) { c.Name = "general-chat" })
				return g
			},
			prune: true,
			wantActions: []string{
				"+ create text #general in The Collective",
				"- delete text #general-chat",
			},
			wantUnmanaged: []string{"text #general-chat"},
			check: func(t *testing.T, p Plan) {
				a := p.Actions[len(p.Actions)-1]
				if a.Kind != DeleteChannel || a.Channel.Name != "general-chat" || a.Channel.Type != model.Text {
					t.Errorf("last action = %+v", a)
				}
			},
		},
		{
			name: "a text and a voice channel of one name are different objects",
			observed: func(t *testing.T) model.Guild {
				g := converged(d)
				mutateChannel(&g, model.Text, "general", func(c *model.Channel) { c.Type = model.Voice })
				return g
			},
			wantActions:   []string{"+ create text #general in The Collective"},
			wantUnmanaged: []string{"voice general"},
		},
		{
			name: "a category and a text channel of one name are different objects",
			observed: func(t *testing.T) model.Guild {
				g := converged(d)
				mutateChannel(&g, model.Category, "Operations", func(c *model.Channel) { c.Type = model.Text })
				return g
			},
			wantActions:   []string{"+ create category Operations"},
			wantUnmanaged: []string{"text #Operations"},
		},
		{
			name: "managed role ignored",
			observed: func(t *testing.T) model.Guild {
				g := converged(d)
				g.Roles = append(g.Roles, model.Role{ID: "r-mee6", Name: "Mee6", Managed: true, Position: 3})
				return g
			},
			prune: true,
		},
		{
			name: "managed role just below the provisioner does not force a reorder",
			observed: func(t *testing.T) model.Guild {
				g := converged(d)
				mutateRole(&g, "Director", func(r *model.Role) { r.Position = 8 })
				mutateRole(&g, "Member", func(r *model.Role) { r.Position = 7 })
				mutateRole(&g, "Probation", func(r *model.Role) { r.Position = 6 })
				g.Roles = append(g.Roles, model.Role{ID: "r-mee6", Name: "Mee6", Managed: true, Position: 9})
				return g
			},
		},
		{
			name: "unmanaged role pruned",
			observed: func(t *testing.T) model.Guild {
				g := converged(d)
				g.Roles = append(g.Roles, model.Role{ID: "r-old", Name: "Old", Position: 1})
				return g
			},
			prune:         true,
			wantActions:   []string{"- delete role Old"},
			wantUnmanaged: []string{"role Old"},
			check: func(t *testing.T, p Plan) {
				if a := p.Actions[0]; a.Kind != DeleteRole || a.Role.Name != "Old" {
					t.Errorf("action = %+v", a)
				}
			},
		},
		{
			name: "unmanaged role reported but not deleted without prune",
			observed: func(t *testing.T) model.Guild {
				g := converged(d)
				g.Roles = append(g.Roles, model.Role{ID: "r-old", Name: "Old", Position: 1})
				return g
			},
			wantUnmanaged: []string{"role Old"},
		},
		{
			name: "unmanaged role between the spec roles and the provisioner forces a reorder",
			observed: func(t *testing.T) model.Guild {
				g := converged(d)
				mutateRole(&g, "Director", func(r *model.Role) { r.Position = 8 })
				mutateRole(&g, "Member", func(r *model.Role) { r.Position = 7 })
				mutateRole(&g, "Probation", func(r *model.Role) { r.Position = 6 })
				g.Roles = append(g.Roles, model.Role{ID: "r-old", Name: "Old", Position: 9})
				return g
			},
			wantActions:   []string{"~ reorder roles: Director, Member, Probation"},
			wantUnmanaged: []string{"role Old"},
		},
		{
			name: "order drift",
			observed: func(t *testing.T) model.Guild {
				g := converged(d)
				mutateRole(&g, "Member", func(r *model.Role) { r.Position = 9 })
				mutateRole(&g, "Director", func(r *model.Role) { r.Position = 8 })
				return g
			},
			wantActions: []string{"~ reorder roles: Director, Member, Probation"},
			check: func(t *testing.T, p Plan) {
				if a := p.Actions[0]; a.Kind != ReorderRoles || !reflect.DeepEqual(a.RoleOrder, []string{"Director", "Member", "Probation"}) {
					t.Errorf("action = %+v", a)
				}
			},
		},
		{
			name: "missing role is created and the roles reordered",
			observed: func(t *testing.T) model.Guild {
				g := converged(d)
				var roles []model.Role
				for _, r := range g.Roles {
					if r.Name != "Member" {
						roles = append(roles, r)
					}
				}
				g.Roles = roles
				return g
			},
			wantActions: []string{
				"+ create role Member",
				"~ reorder roles: Director, Member, Probation",
			},
		},
		{
			name: "prune deletes channels, then categories, then roles",
			observed: func(t *testing.T) model.Guild {
				g := converged(d)
				g.Roles = append(g.Roles, model.Role{ID: "r-old", Name: "Old", Position: 1})
				g.Channels = append(g.Channels,
					model.Channel{ID: "x1", Name: "Stale", Type: model.Category, Position: 9},
					model.Channel{ID: "x2", Name: "old", Type: model.Text, Parent: "Stale"},
					model.Channel{ID: "x3", Name: "Lounge", Type: model.Voice, Parent: "Stale"},
				)
				return g
			},
			prune: true,
			wantActions: []string{
				"- delete text #old",
				"- delete voice Lounge",
				"- delete category Stale",
				"- delete role Old",
			},
			wantUnmanaged: []string{"role Old", "category Stale", "text #old", "voice Lounge"},
			check: func(t *testing.T, p Plan) {
				// Deletes carry the observed objects.
				wantIDs := []string{"x2", "x3", "x1", "r-old"}
				for i, a := range p.Actions {
					id := a.Channel.ID
					if a.Kind == DeleteRole {
						id = a.Role.ID
					}
					if id != wantIDs[i] {
						t.Errorf("action %d (%s) carries ID %q, want %q", i, a, id, wantIDs[i])
					}
				}
			},
		},
		{
			name: "duplicate channel after the managed one is unmanaged",
			observed: func(t *testing.T) model.Guild {
				g := converged(d)
				g.Channels = append(g.Channels, model.Channel{ID: "dup-general", Name: "general", Type: model.Text, Parent: "The Collective", Position: 99})
				return g
			},
			wantUnmanaged: []string{"text #general (duplicate)"},
		},
		{
			name: "duplicate channel is pruned by ID",
			observed: func(t *testing.T) model.Guild {
				g := converged(d)
				g.Channels = append(g.Channels, model.Channel{ID: "dup-general", Name: "general", Type: model.Text, Parent: "The Collective", Position: 99})
				return g
			},
			prune:         true,
			wantActions:   []string{"- delete text #general"},
			wantUnmanaged: []string{"text #general (duplicate)"},
			check: func(t *testing.T, p Plan) {
				a := p.Actions[0]
				if a.Kind != DeleteChannel || a.Channel.ID != "dup-general" || a.Channel.ID == "c-general" {
					t.Errorf("action = %+v, want a delete of the duplicate (ID dup-general), not the managed c-general", a)
				}
			},
		},
		{
			name: "the first observed channel of a name is the managed one",
			observed: func(t *testing.T) model.Guild {
				g := converged(d)
				// A stray #general sits before the real one, in another category.
				first := model.Channel{ID: "first-general", Name: "general", Type: model.Text, Parent: "Operations", Position: 5, Overwrites: findChannel(t, d, model.Text, "general").Overwrites}
				g.Channels = append([]model.Channel{first}, g.Channels...)
				return g
			},
			prune:         true,
			wantActions:   []string{"~ update text #general (parent)", "- delete text #general"},
			wantUnmanaged: []string{"text #general (duplicate)"},
			check: func(t *testing.T, p Plan) {
				if a := p.Actions[1]; a.Channel.ID != "c-general" {
					t.Errorf("deleted ID = %q, want c-general (the later duplicate)", a.Channel.ID)
				}
			},
		},
		{
			name: "duplicate voice channel and category are labelled",
			observed: func(t *testing.T) model.Guild {
				g := converged(d)
				g.Channels = append(g.Channels,
					model.Channel{ID: "dup-comms", Name: "Comms", Type: model.Voice, Parent: "The Collective", Position: 99},
					model.Channel{ID: "dup-ops", Name: "Operations", Type: model.Category, Position: 99},
				)
				return g
			},
			wantUnmanaged: []string{"voice Comms (duplicate)", "category Operations (duplicate)"},
		},
		{
			name: "duplicate role is unmanaged and does not force a reorder",
			observed: func(t *testing.T) model.Guild {
				g := converged(d)
				mutateRole(&g, "Probation", func(r *model.Role) { r.Position = 6 })
				// Sits between Member and Probation: without the exclusion the
				// order check would see Director, Member, Member, Probation.
				g.Roles = append(g.Roles, model.Role{ID: "dup-member", Name: "Member", Position: 7})
				return g
			},
			wantUnmanaged: []string{"role Member (duplicate)"},
		},
		{
			name: "duplicate role is pruned by ID",
			observed: func(t *testing.T) model.Guild {
				g := converged(d)
				mutateRole(&g, "Probation", func(r *model.Role) { r.Position = 6 })
				g.Roles = append(g.Roles, model.Role{ID: "dup-member", Name: "Member", Position: 7})
				return g
			},
			prune:         true,
			wantActions:   []string{"- delete role Member"},
			wantUnmanaged: []string{"role Member (duplicate)"},
			check: func(t *testing.T, p Plan) {
				if a := p.Actions[0]; a.Kind != DeleteRole || a.Role.ID != "dup-member" {
					t.Errorf("action = %+v, want delete of dup-member, not r-Member", a)
				}
			},
		},
		{
			name: "the first observed role of a name is the managed one",
			observed: func(t *testing.T) model.Guild {
				g := converged(d)
				g.Roles = append([]model.Role{{ID: "first-member", Name: "Member", Position: 1, Permissions: 1}}, g.Roles...)
				return g
			},
			prune: true,
			wantActions: []string{
				"~ update role Member (color, hoist, permissions)",
				"~ reorder roles: Director, Member, Probation", // the managed Member sits at the bottom
				"- delete role Member",
			},
			wantUnmanaged: []string{"role Member (duplicate)"},
			check: func(t *testing.T, p Plan) {
				if a := p.Actions[2]; a.Role.ID != "r-Member" {
					t.Errorf("deleted role ID = %q, want r-Member (the later duplicate)", a.Role.ID)
				}
			},
		},
		{
			name: "two unwanted channels of one name are both unmanaged",
			observed: func(t *testing.T) model.Guild {
				g := converged(d)
				g.Channels = append(g.Channels,
					model.Channel{ID: "old1", Name: "old", Type: model.Text, Parent: "Operations", Position: 1},
					model.Channel{ID: "old2", Name: "old", Type: model.Text, Parent: "Operations", Position: 2},
				)
				return g
			},
			prune:         true,
			wantActions:   []string{"- delete text #old", "- delete text #old"},
			wantUnmanaged: []string{"text #old", "text #old"},
			check: func(t *testing.T, p Plan) {
				if p.Actions[0].Channel.ID != "old1" || p.Actions[1].Channel.ID != "old2" {
					t.Errorf("deleted IDs = %q, %q, want old1, old2", p.Actions[0].Channel.ID, p.Actions[1].Channel.ID)
				}
			},
		},
		{
			name: "an unmanaged stray before the managed channels does not shift their positions",
			observed: func(t *testing.T) model.Guild {
				g := converged(d)
				for i := range g.Channels {
					if g.Channels[i].Parent == "The Collective" {
						g.Channels[i].Position++
					}
				}
				stray := model.Channel{ID: "stray", Name: "stray", Type: model.Text, Parent: "The Collective", Position: 0}
				g.Channels = append([]model.Channel{stray}, g.Channels...)
				return g
			},
			wantUnmanaged: []string{"text #stray"},
		},
		{
			name: "an unmanaged category among the categories does not shift their positions",
			observed: func(t *testing.T) model.Guild {
				g := converged(d)
				for i := range g.Channels {
					if g.Channels[i].Type == model.Category {
						g.Channels[i].Position++
					}
				}
				stray := model.Channel{ID: "stray-cat", Name: "Stray", Type: model.Category, Position: 0}
				g.Channels = append([]model.Channel{stray}, g.Channels...)
				return g
			},
			wantUnmanaged: []string{"category Stray"},
		},
		{
			name: "swapped managed channels are position drift",
			observed: func(t *testing.T) model.Guild {
				g := converged(d)
				mutateChannel(&g, model.Text, "general", func(c *model.Channel) { c.Position = 2 })
				mutateChannel(&g, model.Text, "contributions", func(c *model.Channel) { c.Position = 1 })
				return g
			},
			wantActions: []string{
				"~ update text #general (position)",
				"~ update text #contributions (position)",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := Reconcile(d, tc.observed(t), tc.prune)
			if got := strs(p); !reflect.DeepEqual(got, tc.wantActions) {
				t.Errorf("actions =\n%q\nwant\n%q", got, tc.wantActions)
			}
			if !reflect.DeepEqual(p.Unmanaged, tc.wantUnmanaged) {
				t.Errorf("Unmanaged = %q, want %q", p.Unmanaged, tc.wantUnmanaged)
			}
			if tc.check != nil {
				tc.check(t, p)
			}
		})
	}
}

func TestReconcileDoesNotMutateInputs(t *testing.T) {
	d := mustDesired(t)
	g := converged(d)
	// Unsorted overwrites on one channel, plus plenty of drift.
	mutateChannel(&g, model.Text, "general", func(c *model.Channel) {
		o := c.Overwrites
		o[0], o[len(o)-1] = o[len(o)-1], o[0]
	})
	mutateChannel(&g, model.Text, "logistics", func(c *model.Channel) { c.Parent = "The Collective" })
	g.Roles = append(g.Roles, model.Role{ID: "r-old", Name: "Old", Position: 9}, model.Role{ID: "r-mee6", Name: "Mee6", Managed: true, Position: 3})
	g.EveryonePermissions = 0

	gBefore := cloneGuild(g)
	dBefore := mustDesired(t)

	for _, prune := range []bool{false, true} {
		Reconcile(d, g, prune)
	}
	if !reflect.DeepEqual(g, gBefore) {
		t.Errorf("observed guild was modified:\n%+v\nwant\n%+v", g, gBefore)
	}
	if !reflect.DeepEqual(d, dBefore) {
		t.Errorf("desired was modified")
	}
}

func TestReconcileIsDeterministic(t *testing.T) {
	d := mustDesired(t)
	g := model.Guild{Roles: []model.Role{{Name: "Provisioner", Position: 1}}}
	first := Reconcile(d, g, true)
	for i := 0; i < 20; i++ {
		if got := Reconcile(d, g, true); !reflect.DeepEqual(got, first) {
			t.Fatalf("run %d differs from the first", i)
		}
	}
}

func TestCheckMissingProvisioner(t *testing.T) {
	d := mustDesired(t)
	g := converged(d)
	var roles []model.Role
	for _, r := range g.Roles {
		if r.Name != d.ProvisionerRole {
			roles = append(roles, r)
		}
	}
	g.Roles = roles
	err := Check(g, d)
	if err == nil {
		t.Fatal("want error when the provisioner role does not exist")
	}
	if !strings.Contains(err.Error(), d.ProvisionerRole) || !strings.Contains(err.Error(), "drag the bot's role above") {
		t.Errorf("err = %v", err)
	}
}

func TestCheckProvisionerBelowManaged(t *testing.T) {
	d := mustDesired(t)
	g := converged(d)
	mutateRole(&g, "Director", func(r *model.Role) { r.Position = 11 })
	err := Check(g, d)
	if err == nil {
		t.Fatal("want error when a managed role sits above the provisioner")
	}
	if !strings.Contains(err.Error(), "drag the bot's role above") || !strings.Contains(err.Error(), "Director") {
		t.Errorf("err = %v", err)
	}
}

func TestCheckProvisionerEqualPosition(t *testing.T) {
	d := mustDesired(t)
	g := converged(d)
	mutateRole(&g, "Member", func(r *model.Role) { r.Position = 10 })
	if err := Check(g, d); err == nil || !strings.Contains(err.Error(), "Member") {
		t.Errorf("err = %v, want an error naming Member", err)
	}
}

func TestCheckOK(t *testing.T) {
	d := mustDesired(t)
	if err := Check(converged(d), d); err != nil {
		t.Errorf("Check(converged) = %v", err)
	}
	// Roles that don't exist yet are fine: they are created below the provisioner.
	g := model.Guild{Roles: []model.Role{{Name: "Provisioner", Position: 1}}}
	if err := Check(g, d); err != nil {
		t.Errorf("Check(provisioner only) = %v", err)
	}
}

func TestActionString(t *testing.T) {
	tests := []struct {
		a    Action
		want string
	}{
		{Action{Kind: UpdateEveryone, Permissions: 5}, "~ update @everyone permissions"},
		{Action{Kind: CreateRole, Role: model.Role{Name: "Director"}}, "+ create role Director"},
		{Action{Kind: UpdateRole, Role: model.Role{Name: "Member"}, Changes: []string{"permissions"}}, "~ update role Member (permissions)"},
		{Action{Kind: UpdateRole, Role: model.Role{Name: "Member"}, Changes: []string{"color", "hoist"}}, "~ update role Member (color, hoist)"},
		{Action{Kind: ReorderRoles, RoleOrder: []string{"Director", "Member", "Probation"}}, "~ reorder roles: Director, Member, Probation"},
		{Action{Kind: DeleteRole, Role: model.Role{Name: "Old"}}, "- delete role Old"},
		{Action{Kind: CreateChannel, Channel: model.Channel{Name: "The Collective", Type: model.Category}}, "+ create category The Collective"},
		{Action{Kind: CreateChannel, Channel: model.Channel{Name: "general", Type: model.Text, Parent: "The Collective"}}, "+ create text #general in The Collective"},
		{Action{Kind: CreateChannel, Channel: model.Channel{Name: "Comms", Type: model.Voice, Parent: "The Collective"}}, "+ create voice Comms in The Collective"},
		{Action{Kind: UpdateChannel, Channel: model.Channel{Name: "apply", Type: model.Text}, Changes: []string{"overwrites"}}, "~ update text #apply (overwrites)"},
		{Action{Kind: UpdateChannel, Channel: model.Channel{Name: "Comms", Type: model.Voice}, Changes: []string{"parent", "position"}}, "~ update voice Comms (parent, position)"},
		{Action{Kind: UpdateChannel, Channel: model.Channel{Name: "Operations", Type: model.Category}, Changes: []string{"overwrites"}}, "~ update category Operations (overwrites)"},
		{Action{Kind: DeleteChannel, Channel: model.Channel{Name: "old", Type: model.Text}}, "- delete text #old"},
		{Action{Kind: DeleteChannel, Channel: model.Channel{Name: "Lounge", Type: model.Voice}}, "- delete voice Lounge"},
		{Action{Kind: DeleteChannel, Channel: model.Channel{Name: "Stale", Type: model.Category}}, "- delete category Stale"},
		{Action{Kind: PostMessage, Target: "constitution", Index: 3, Total: 7}, "+ post message 3/7 in #constitution"},
		{Action{Kind: EditMessage, Target: "constitution", Index: 2, Total: 7}, "~ edit message 2/7 in #constitution"},
		{Action{Kind: DeleteMessage, Target: "constitution"}, "- delete message in #constitution"},
	}
	for _, tc := range tests {
		if got := tc.a.String(); got != tc.want {
			t.Errorf("String() = %q, want %q", got, tc.want)
		}
	}
}

func TestPlanString(t *testing.T) {
	p := Plan{
		Actions: []Action{
			{Kind: CreateRole, Role: model.Role{Name: "Director"}},
			{Kind: ReorderRoles, RoleOrder: []string{"Director"}},
		},
		Unmanaged: []string{"role Old", "text #old"},
	}
	want := "+ create role Director\n~ reorder roles: Director\nunmanaged:\n  role Old\n  text #old"
	if got := p.String(); got != want {
		t.Errorf("String() =\n%q\nwant\n%q", got, want)
	}

	if got := (Plan{Unmanaged: []string{"role Old"}}).String(); got != "unmanaged:\n  role Old" {
		t.Errorf("unmanaged only: %q", got)
	}
	if got := (Plan{Actions: []Action{{Kind: DeleteRole, Role: model.Role{Name: "Old"}}}}).String(); got != "- delete role Old" {
		t.Errorf("actions only: %q", got)
	}
	if got := (Plan{}).String(); got != "no changes" {
		t.Errorf("empty: %q", got)
	}
}
