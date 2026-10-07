package discord_test

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/bwmarrin/discordgo"
	"github.com/theding0x/eleventh-thesis-discord/internal/discord"
	"github.com/theding0x/eleventh-thesis-discord/internal/model"
)

var ctx = context.Background()

func mustObserve(t *testing.T, f *discord.Fake, content ...string) model.Guild {
	t.Helper()
	g, err := f.Observe(ctx, content)
	if err != nil {
		t.Fatalf("Observe: %v", err)
	}
	return g
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func roleNames(g model.Guild) []string {
	var out []string
	for _, r := range g.Roles {
		out = append(out, r.ID+":"+r.Name)
	}
	return out
}

func channelNames(g model.Guild) []string {
	var out []string
	for _, c := range g.Channels {
		out = append(out, c.ID+":"+c.Name)
	}
	return out
}

func TestNewFake(t *testing.T) {
	f := discord.NewFake("Provisioner")
	g := mustObserve(t, f)
	want := []model.Role{{ID: "r0", Name: "Provisioner", Position: 10, Managed: true}}
	if !reflect.DeepEqual(g.Roles, want) {
		t.Errorf("roles = %+v, want %+v", g.Roles, want)
	}
	if len(g.Channels) != 0 {
		t.Errorf("channels = %+v, want none", g.Channels)
	}
	if g.EveryonePermissions != discord.DefaultEveryone {
		t.Errorf("everyone = %d, want %d", g.EveryonePermissions, discord.DefaultEveryone)
	}
}

func TestDefaultEveryoneValue(t *testing.T) {
	want := int64(discordgo.PermissionViewChannel | discordgo.PermissionSendMessages |
		discordgo.PermissionReadMessageHistory | discordgo.PermissionAddReactions |
		discordgo.PermissionVoiceConnect | discordgo.PermissionVoiceSpeak)
	if discord.DefaultEveryone != want {
		t.Errorf("DefaultEveryone = %d, want %d", discord.DefaultEveryone, want)
	}
}

func TestIDsAreDeterministic(t *testing.T) {
	f := discord.NewFake("Provisioner")
	must(t, f.CreateRole(ctx, model.Role{Name: "A"}))
	must(t, f.CreateRole(ctx, model.Role{Name: "B"}))
	must(t, f.CreateChannel(ctx, model.Channel{Name: "Cat", Type: model.Category}))
	must(t, f.CreateChannel(ctx, model.Channel{Name: "chat", Type: model.Text, Parent: "Cat"}))
	must(t, f.PostMessage(ctx, "chat", "one"))
	must(t, f.PostMessage(ctx, "chat", "two"))

	g := mustObserve(t, f, "chat")
	roles := map[string]string{}
	for _, r := range g.Roles {
		roles[r.Name] = r.ID
	}
	if roles["A"] != "r1" || roles["B"] != "r2" || roles["Provisioner"] != "r0" {
		t.Errorf("role IDs = %v", roles)
	}
	chans := map[string]string{}
	for _, c := range g.Channels {
		chans[c.Name] = c.ID
	}
	if chans["Cat"] != "c1" || chans["chat"] != "c2" {
		t.Errorf("channel IDs = %v", chans)
	}
	if want := []model.Message{{ID: "m1", Content: "one"}, {ID: "m2", Content: "two"}}; !reflect.DeepEqual(g.BotMessages["chat"], want) {
		t.Errorf("messages = %+v, want %+v", g.BotMessages["chat"], want)
	}

	// IDs are never reused after a delete.
	must(t, f.DeleteRole(ctx, model.Role{ID: "r2"}))
	must(t, f.CreateRole(ctx, model.Role{Name: "C"}))
	for _, r := range mustObserve(t, f).Roles {
		if r.Name == "C" && r.ID != "r3" {
			t.Errorf("new role ID = %s, want r3", r.ID)
		}
	}
}

func TestNewIDSkipsIDsAlreadyInUse(t *testing.T) {
	f := discord.NewFake("Provisioner")
	f.Guild.Channels = append(f.Guild.Channels, model.Channel{ID: "c1", Name: "stray", Type: model.Text})
	must(t, f.CreateChannel(ctx, model.Channel{Name: "new", Type: model.Text}))
	for _, c := range mustObserve(t, f).Channels {
		if c.Name == "new" && c.ID != "c2" {
			t.Errorf("new channel ID = %s, want c2 (c1 is taken)", c.ID)
		}
	}
}

func TestObserveReturnsDeepCopy(t *testing.T) {
	f := discord.NewFake("Provisioner")
	must(t, f.CreateChannel(ctx, model.Channel{
		Name: "chat", Type: model.Text,
		Overwrites: []model.Overwrite{{Target: "@everyone", Allow: 1}},
	}))
	must(t, f.PostMessage(ctx, "chat", "hello"))

	g := mustObserve(t, f, "chat")
	g.Roles[0].Name = "mutated"
	g.Roles = append(g.Roles, model.Role{Name: "extra"})
	g.Channels[0].Overwrites[0].Allow = 99
	g.Channels[0].Name = "mutated"
	g.BotMessages["chat"][0].Content = "mutated"
	g.BotMessages["other"] = []model.Message{{ID: "x"}}

	g2 := mustObserve(t, f, "chat")
	if len(g2.Roles) != 1 || g2.Roles[0].Name != "Provisioner" {
		t.Errorf("roles changed through the copy: %+v", g2.Roles)
	}
	if g2.Channels[0].Name != "chat" || g2.Channels[0].Overwrites[0].Allow != 1 {
		t.Errorf("channel changed through the copy: %+v", g2.Channels[0])
	}
	if got := g2.BotMessages["chat"][0].Content; got != "hello" {
		t.Errorf("message content = %q, want hello", got)
	}
	if _, ok := g2.BotMessages["other"]; ok {
		t.Error("BotMessages map is shared with the Fake")
	}
}

func TestObserveBotMessagesOnlyForRequestedTextChannels(t *testing.T) {
	f := discord.NewFake("Provisioner")
	must(t, f.CreateChannel(ctx, model.Channel{Name: "a", Type: model.Text}))
	must(t, f.CreateChannel(ctx, model.Channel{Name: "b", Type: model.Text}))
	must(t, f.CreateChannel(ctx, model.Channel{Name: "v", Type: model.Voice}))
	must(t, f.PostMessage(ctx, "a", "in a"))
	must(t, f.PostMessage(ctx, "b", "in b"))

	g := mustObserve(t, f, "a", "v", "missing")
	if len(g.BotMessages) != 1 || len(g.BotMessages["a"]) != 1 {
		t.Errorf("BotMessages = %+v, want only a", g.BotMessages)
	}
}

func TestObserveOrdering(t *testing.T) {
	f := discord.NewFake("Provisioner")
	f.Guild.Roles = append(f.Guild.Roles,
		model.Role{ID: "r9", Name: "Low", Position: 2},
		model.Role{ID: "r3", Name: "TieB", Position: 5},
		model.Role{ID: "r2", Name: "TieA", Position: 5},
		model.Role{ID: "r10", Name: "TieC", Position: 5},
	)
	g := mustObserve(t, f)
	want := []string{"r0:Provisioner", "r2:TieA", "r3:TieB", "r10:TieC", "r9:Low"}
	if got := roleNames(g); !reflect.DeepEqual(got, want) {
		t.Errorf("role order = %v, want %v", got, want)
	}

	f.Guild.Channels = []model.Channel{
		{ID: "c5", Name: "z-cat", Type: model.Category, Position: 1},
		{ID: "c4", Name: "in-z", Type: model.Text, Parent: "z-cat", Position: 0},
		{ID: "c3", Name: "in-a-late", Type: model.Text, Parent: "a-cat", Position: 1},
		{ID: "c2", Name: "in-a-dup", Type: model.Text, Parent: "a-cat", Position: 0},
		{ID: "c1", Name: "in-a", Type: model.Text, Parent: "a-cat", Position: 0},
		{ID: "c6", Name: "a-cat", Type: model.Category, Position: 0},
	}
	g = mustObserve(t, f)
	want = []string{"c6:a-cat", "c5:z-cat", "c1:in-a", "c2:in-a-dup", "c3:in-a-late", "c4:in-z"}
	if got := channelNames(g); !reflect.DeepEqual(got, want) {
		t.Errorf("channel order = %v, want %v", got, want)
	}
}

func TestMutatorsKeepGuildSorted(t *testing.T) {
	f := discord.NewFake("Provisioner")
	f.Guild.Channels = []model.Channel{
		{ID: "c2", Name: "b", Type: model.Text, Position: 1},
		{ID: "c1", Name: "a", Type: model.Text, Position: 0},
	}
	must(t, f.PostMessage(ctx, "a", "x")) // any mutation re-sorts the stored guild
	if got, want := channelNames(model.Guild{Channels: f.Guild.Channels}), []string{"c1:a", "c2:b"}; !reflect.DeepEqual(got, want) {
		t.Errorf("stored order = %v, want %v", got, want)
	}
}

func TestCreateAndUpdateRoleStoreFieldsExactly(t *testing.T) {
	f := discord.NewFake("Provisioner")
	must(t, f.CreateRole(ctx, model.Role{Name: "Director", Color: 0xB22222, Hoist: true, Mentionable: true, Permissions: 12345}))
	g := mustObserve(t, f)
	var got model.Role
	for _, r := range g.Roles {
		if r.Name == "Director" {
			got = r
		}
	}
	want := model.Role{ID: "r1", Name: "Director", Color: 0xB22222, Hoist: true, Mentionable: true, Permissions: 12345, Position: 1}
	if got != want {
		t.Errorf("role = %+v, want %+v", got, want)
	}

	must(t, f.UpdateRole(ctx, model.Role{Name: "Director", Color: 1, Hoist: false, Mentionable: false, Permissions: 7}))
	for _, r := range mustObserve(t, f).Roles {
		if r.Name == "Director" {
			want := model.Role{ID: "r1", Name: "Director", Color: 1, Permissions: 7, Position: 1}
			if r != want {
				t.Errorf("updated role = %+v, want %+v", r, want)
			}
		}
	}
}

func TestCreateRoleShiftsOthersUp(t *testing.T) {
	f := discord.NewFake("Provisioner")
	must(t, f.CreateRole(ctx, model.Role{Name: "A"}))
	must(t, f.CreateRole(ctx, model.Role{Name: "B"}))
	pos := map[string]int{}
	for _, r := range mustObserve(t, f).Roles {
		pos[r.Name] = r.Position
	}
	if pos["B"] != 1 || pos["A"] != 2 || pos["Provisioner"] != 12 {
		t.Errorf("positions = %v, want B=1 A=2 Provisioner=12", pos)
	}
}

func TestReorderRoles(t *testing.T) {
	f := discord.NewFake("Provisioner")
	for _, n := range []string{"A", "B", "C"} {
		must(t, f.CreateRole(ctx, model.Role{Name: n}))
	}
	f.Guild.Roles = append(f.Guild.Roles, model.Role{ID: "r50", Name: "Other", Position: 1})
	before := mustObserve(t, f)
	var provPos int
	for _, r := range before.Roles {
		if r.Name == "Provisioner" {
			provPos = r.Position
		}
	}

	must(t, f.ReorderRoles(ctx, []string{"C", "A", "B"}))
	pos := map[string]int{}
	for _, r := range mustObserve(t, f).Roles {
		pos[r.Name] = r.Position
	}
	if pos["Provisioner"] != provPos {
		t.Errorf("provisioner moved: %d -> %d", provPos, pos["Provisioner"])
	}
	if pos["C"] != provPos-1 || pos["A"] != provPos-2 || pos["B"] != provPos-3 {
		t.Errorf("positions = %v, want C,A,B at provisioner-1,-2,-3", pos)
	}
	if pos["Other"] != 1 {
		t.Errorf("Other moved to %d, want unchanged (1)", pos["Other"])
	}
}

func TestReorderRolesUnknownNameChangesNothing(t *testing.T) {
	f := discord.NewFake("Provisioner")
	must(t, f.CreateRole(ctx, model.Role{Name: "A"}))
	before := mustObserve(t, f)
	if err := f.ReorderRoles(ctx, []string{"A", "Nope"}); err == nil {
		t.Fatal("want an error for an unknown role")
	}
	if after := mustObserve(t, f); !reflect.DeepEqual(before, after) {
		t.Errorf("guild changed on error: %+v -> %+v", before, after)
	}
}

func TestCreateChannelStoresAsGivenAndSortsOverwrites(t *testing.T) {
	f := discord.NewFake("Provisioner")
	must(t, f.CreateChannel(ctx, model.Channel{Name: "Cat", Type: model.Category, Position: 3}))
	must(t, f.CreateChannel(ctx, model.Channel{
		Name: "chat", Type: model.Text, Parent: "Cat", Position: 2, Topic: "t",
		Overwrites: []model.Overwrite{{Target: "b", Allow: 2}, {Target: "a", Deny: 4}},
	}))
	var got model.Channel
	for _, c := range mustObserve(t, f).Channels {
		if c.Name == "chat" {
			got = c
		}
	}
	want := model.Channel{
		ID: "c2", Name: "chat", Type: model.Text, Parent: "Cat", Position: 2, Topic: "t",
		Overwrites: []model.Overwrite{{Target: "a", Deny: 4}, {Target: "b", Allow: 2}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("channel = %+v, want %+v", got, want)
	}
}

func TestUpdateChannelReplacesParentPositionTopicOverwrites(t *testing.T) {
	f := discord.NewFake("Provisioner")
	must(t, f.CreateChannel(ctx, model.Channel{Name: "One", Type: model.Category}))
	must(t, f.CreateChannel(ctx, model.Channel{Name: "Two", Type: model.Category, Position: 1}))
	must(t, f.CreateChannel(ctx, model.Channel{
		Name: "chat", Type: model.Text, Parent: "One", Position: 4, Topic: "old",
		Overwrites: []model.Overwrite{{Target: "a", Allow: 1}, {Target: "member:42", Allow: 1}},
	}))
	// A voice channel with the same name is not matched by a text update.
	must(t, f.CreateChannel(ctx, model.Channel{Name: "chat", Type: model.Voice, Parent: "One"}))

	must(t, f.UpdateChannel(ctx, model.Channel{
		Name: "chat", Type: model.Text, Parent: "Two", Position: 0, Topic: "new",
		Overwrites: []model.Overwrite{{Target: "z", Deny: 8}, {Target: "a", Allow: 2}},
	}))
	for _, c := range mustObserve(t, f).Channels {
		if c.Name != "chat" {
			continue
		}
		if c.Type == model.Voice {
			if c.Parent != "One" {
				t.Errorf("voice channel changed: %+v", c)
			}
			continue
		}
		want := model.Channel{
			ID: "c3", Name: "chat", Type: model.Text, Parent: "Two", Position: 0, Topic: "new",
			Overwrites: []model.Overwrite{{Target: "a", Allow: 2}, {Target: "z", Deny: 8}},
		}
		if !reflect.DeepEqual(c, want) {
			t.Errorf("channel = %+v, want %+v", c, want)
		}
	}
}

func TestUpdateChannelMatchesFirstInObserveOrder(t *testing.T) {
	f := discord.NewFake("Provisioner")
	f.Guild.Channels = []model.Channel{
		{ID: "c7", Name: "general", Type: model.Text, Topic: "dup"},
		{ID: "c3", Name: "general", Type: model.Text, Topic: "first"},
	}
	must(t, f.UpdateChannel(ctx, model.Channel{Name: "general", Type: model.Text, Topic: "updated"}))
	topics := map[string]string{}
	for _, c := range mustObserve(t, f).Channels {
		topics[c.ID] = c.Topic
	}
	if topics["c3"] != "updated" || topics["c7"] != "dup" {
		t.Errorf("topics = %v, want c3 updated and c7 untouched", topics)
	}
}

func TestDeleteByIDRemovesExactlyThatObject(t *testing.T) {
	f := discord.NewFake("Provisioner")
	f.Guild.Roles = append(f.Guild.Roles,
		model.Role{ID: "r1", Name: "Member", Position: 3},
		model.Role{ID: "r2", Name: "Member", Position: 2},
	)
	f.Guild.Channels = []model.Channel{
		{ID: "c1", Name: "general", Type: model.Text},
		{ID: "c2", Name: "general", Type: model.Text, Position: 1},
	}
	f.Guild.BotMessages = map[string][]model.Message{"general": {{ID: "m1", Content: "keep"}}}

	must(t, f.DeleteRole(ctx, model.Role{ID: "r2", Name: "Member"}))
	must(t, f.DeleteChannel(ctx, model.Channel{ID: "c2", Name: "general", Type: model.Text}))

	g := mustObserve(t, f, "general")
	if got, want := roleNames(g), []string{"r0:Provisioner", "r1:Member"}; !reflect.DeepEqual(got, want) {
		t.Errorf("roles = %v, want %v", got, want)
	}
	if got, want := channelNames(g), []string{"c1:general"}; !reflect.DeepEqual(got, want) {
		t.Errorf("channels = %v, want %v", got, want)
	}
	// The surviving #general still has its messages: deleting its duplicate
	// must not drop them.
	if len(g.BotMessages["general"]) != 1 {
		t.Errorf("messages = %+v, want the surviving channel's message", g.BotMessages["general"])
	}
}

func TestDeleteByNameWhenIDEmpty(t *testing.T) {
	f := discord.NewFake("Provisioner")
	must(t, f.CreateRole(ctx, model.Role{Name: "Old"}))
	must(t, f.CreateChannel(ctx, model.Channel{Name: "old", Type: model.Text}))
	must(t, f.CreateChannel(ctx, model.Channel{Name: "old", Type: model.Voice}))

	must(t, f.DeleteRole(ctx, model.Role{Name: "Old"}))
	must(t, f.DeleteChannel(ctx, model.Channel{Name: "old", Type: model.Voice}))

	g := mustObserve(t, f)
	if len(g.Roles) != 1 {
		t.Errorf("roles = %v, want only the provisioner", roleNames(g))
	}
	if got, want := channelNames(g), []string{"c1:old"}; !reflect.DeepEqual(got, want) {
		t.Errorf("channels = %v, want only the text channel", got)
	}
}

func TestDeleteChannelDropsItsMessages(t *testing.T) {
	f := discord.NewFake("Provisioner")
	must(t, f.CreateChannel(ctx, model.Channel{Name: "chat", Type: model.Text}))
	must(t, f.PostMessage(ctx, "chat", "hi"))
	must(t, f.DeleteChannel(ctx, model.Channel{Name: "chat", Type: model.Text}))
	if len(f.Guild.BotMessages["chat"]) != 0 {
		t.Errorf("BotMessages = %+v, want none", f.Guild.BotMessages)
	}
	// Recreating the channel starts empty.
	must(t, f.CreateChannel(ctx, model.Channel{Name: "chat", Type: model.Text}))
	if got := mustObserve(t, f, "chat").BotMessages["chat"]; len(got) != 0 {
		t.Errorf("messages = %+v, want none", got)
	}
}

func TestDeleteCategoryUncategorisesChildren(t *testing.T) {
	f := discord.NewFake("Provisioner")
	must(t, f.CreateChannel(ctx, model.Channel{Name: "Cat", Type: model.Category}))
	must(t, f.CreateChannel(ctx, model.Channel{Name: "chat", Type: model.Text, Parent: "Cat"}))
	must(t, f.DeleteChannel(ctx, model.Channel{Name: "Cat", Type: model.Category}))
	g := mustObserve(t, f)
	if len(g.Channels) != 1 || g.Channels[0].Parent != "" {
		t.Errorf("channels = %+v, want chat with no parent", g.Channels)
	}
}

func TestMessageLifecycle(t *testing.T) {
	f := discord.NewFake("Provisioner")
	must(t, f.CreateChannel(ctx, model.Channel{Name: "chat", Type: model.Text}))
	must(t, f.PostMessage(ctx, "chat", "one"))
	must(t, f.PostMessage(ctx, "chat", "two"))
	must(t, f.EditMessage(ctx, "chat", "m1", "ONE"))
	must(t, f.DeleteMessage(ctx, "chat", "m2"))
	want := []model.Message{{ID: "m1", Content: "ONE"}}
	if got := mustObserve(t, f, "chat").BotMessages["chat"]; !reflect.DeepEqual(got, want) {
		t.Errorf("messages = %+v, want %+v", got, want)
	}
}

func TestEveryonePermissions(t *testing.T) {
	f := discord.NewFake("Provisioner")
	must(t, f.UpdateEveryone(ctx, 1234))
	if g := mustObserve(t, f); g.EveryonePermissions != 1234 {
		t.Errorf("everyone = %d, want 1234", g.EveryonePermissions)
	}
}

func TestErrorPaths(t *testing.T) {
	cases := []struct {
		name string
		call func(f *discord.Fake) error
	}{
		{"CreateChannel unknown parent", func(f *discord.Fake) error {
			return f.CreateChannel(ctx, model.Channel{Name: "x", Type: model.Text, Parent: "Nope"})
		}},
		{"UpdateChannel unknown", func(f *discord.Fake) error {
			return f.UpdateChannel(ctx, model.Channel{Name: "nope", Type: model.Text})
		}},
		{"UpdateRole unknown", func(f *discord.Fake) error {
			return f.UpdateRole(ctx, model.Role{Name: "Nope"})
		}},
		{"DeleteRole unknown ID", func(f *discord.Fake) error {
			return f.DeleteRole(ctx, model.Role{ID: "r99", Name: "Provisioner"})
		}},
		{"DeleteRole unknown name", func(f *discord.Fake) error {
			return f.DeleteRole(ctx, model.Role{Name: "Nope"})
		}},
		{"DeleteChannel unknown ID", func(f *discord.Fake) error {
			return f.DeleteChannel(ctx, model.Channel{ID: "c99", Name: "chat", Type: model.Text})
		}},
		{"DeleteChannel unknown name", func(f *discord.Fake) error {
			return f.DeleteChannel(ctx, model.Channel{Name: "nope", Type: model.Text})
		}},
		{"PostMessage unknown channel", func(f *discord.Fake) error {
			return f.PostMessage(ctx, "nope", "x")
		}},
		{"PostMessage to a voice channel", func(f *discord.Fake) error {
			return f.PostMessage(ctx, "voice", "x")
		}},
		{"EditMessage unknown channel", func(f *discord.Fake) error {
			return f.EditMessage(ctx, "nope", "m1", "x")
		}},
		{"EditMessage unknown message", func(f *discord.Fake) error {
			return f.EditMessage(ctx, "chat", "m99", "x")
		}},
		{"DeleteMessage unknown channel", func(f *discord.Fake) error {
			return f.DeleteMessage(ctx, "nope", "m1")
		}},
		{"DeleteMessage unknown message", func(f *discord.Fake) error {
			return f.DeleteMessage(ctx, "chat", "m99")
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := discord.NewFake("Provisioner")
			must(t, f.CreateChannel(ctx, model.Channel{Name: "chat", Type: model.Text}))
			must(t, f.CreateChannel(ctx, model.Channel{Name: "voice", Type: model.Voice}))
			must(t, f.PostMessage(ctx, "chat", "hello"))
			before := mustObserve(t, f, "chat")
			if err := tc.call(f); err == nil {
				t.Fatal("want an error")
			}
			if after := mustObserve(t, f, "chat"); !reflect.DeepEqual(before, after) {
				t.Errorf("guild changed by a failing call:\nbefore %+v\nafter  %+v", before, after)
			}
		})
	}
}

func TestFailOnAndCalls(t *testing.T) {
	f := discord.NewFake("Provisioner")
	f.FailOn = "CreateRole"
	err := f.CreateRole(ctx, model.Role{Name: "A"})
	if err == nil || !strings.Contains(err.Error(), "CreateRole") {
		t.Fatalf("err = %v, want one naming CreateRole", err)
	}
	if n := len(mustObserve(t, f).Roles); n != 1 {
		t.Errorf("roles = %d, want 1 (failing call must not mutate)", n)
	}
	f.FailOn = ""
	must(t, f.CreateRole(ctx, model.Role{Name: "A"}))
	must(t, f.UpdateEveryone(ctx, 1))
	want := []string{"CreateRole", "Observe", "CreateRole", "UpdateEveryone"}
	if !reflect.DeepEqual(f.Calls, want) {
		t.Errorf("Calls = %v, want %v", f.Calls, want)
	}
}

func TestObserveCanFail(t *testing.T) {
	f := discord.NewFake("Provisioner")
	f.FailOn = "Observe"
	if _, err := f.Observe(ctx, nil); err == nil {
		t.Fatal("want an error")
	}
}
