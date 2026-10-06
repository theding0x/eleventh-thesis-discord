package discord

import (
	"encoding/json"
	"errors"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/bwmarrin/discordgo"

	"github.com/theding0x/eleventh-thesis-discord/internal/model"
)

var _ Client = (*Live)(nil)

const testGuild = "1000"

func TestToModelChannelEveryone(t *testing.T) {
	c := &discordgo.Channel{
		ID: "10", Name: "welcome", Type: discordgo.ChannelTypeGuildText,
		PermissionOverwrites: []*discordgo.PermissionOverwrite{
			{ID: testGuild, Type: discordgo.PermissionOverwriteTypeRole, Allow: 1, Deny: 2},
		},
	}
	got, ok := toModelChannel(c, testGuild, map[string]string{"5": "Member"}, nil)
	if !ok {
		t.Fatal("text channel was skipped")
	}
	want := []model.Overwrite{{Target: "@everyone", Allow: 1, Deny: 2}}
	if !reflect.DeepEqual(got.Overwrites, want) {
		t.Errorf("Overwrites = %+v, want %+v", got.Overwrites, want)
	}
}

func TestToModelChannelRoleOverwrites(t *testing.T) {
	c := &discordgo.Channel{
		ID: "10", Name: "chat", Type: discordgo.ChannelTypeGuildText,
		PermissionOverwrites: []*discordgo.PermissionOverwrite{
			{ID: "7", Type: discordgo.PermissionOverwriteTypeRole, Allow: 8},
			{ID: "5", Type: discordgo.PermissionOverwriteTypeRole, Allow: 4, Deny: 16},
		},
	}
	got, _ := toModelChannel(c, testGuild, map[string]string{"5": "Member"}, nil)
	want := []model.Overwrite{
		{Target: "Member", Allow: 4, Deny: 16}, // role name
		{Target: "role:7", Allow: 8},           // deleted/unknown role shows as drift
	}
	if !reflect.DeepEqual(got.Overwrites, want) {
		t.Errorf("Overwrites = %+v, want %+v", got.Overwrites, want)
	}
}

func TestToModelChannelMemberOverwrite(t *testing.T) {
	c := &discordgo.Channel{
		ID: "10", Name: "chat", Type: discordgo.ChannelTypeGuildText,
		PermissionOverwrites: []*discordgo.PermissionOverwrite{
			{ID: "42", Type: discordgo.PermissionOverwriteTypeMember, Allow: 1024},
		},
	}
	got, _ := toModelChannel(c, testGuild, nil, nil)
	want := []model.Overwrite{{Target: "member:42", Allow: 1024}}
	if !reflect.DeepEqual(got.Overwrites, want) {
		t.Errorf("Overwrites = %+v, want %+v", got.Overwrites, want)
	}
}

func TestToModelChannelIgnoresOtherTypes(t *testing.T) {
	for _, typ := range []discordgo.ChannelType{
		discordgo.ChannelTypeGuildNews,
		discordgo.ChannelTypeGuildStageVoice,
		discordgo.ChannelTypeGuildForum,
		discordgo.ChannelTypeGuildPublicThread,
	} {
		if _, ok := toModelChannel(&discordgo.Channel{ID: "1", Name: "x", Type: typ}, testGuild, nil, nil); ok {
			t.Errorf("type %d was not skipped", typ)
		}
	}
}

func TestToModelChannelParentName(t *testing.T) {
	c := &discordgo.Channel{ID: "10", Name: "chat", Type: discordgo.ChannelTypeGuildText, ParentID: "99"}
	got, _ := toModelChannel(c, testGuild, nil, map[string]string{"99": "Lobby"})
	if got.Parent != "Lobby" {
		t.Errorf("Parent = %q, want Lobby", got.Parent)
	}
	orphan := &discordgo.Channel{ID: "11", Name: "chat", Type: discordgo.ChannelTypeGuildText, ParentID: "98"}
	got, _ = toModelChannel(orphan, testGuild, nil, map[string]string{"99": "Lobby"})
	if got.Parent != "category:98" {
		t.Errorf("unknown parent: Parent = %q, want category:98 (visible as drift)", got.Parent)
	}
}

func TestToModelChannelFields(t *testing.T) {
	text, _ := toModelChannel(&discordgo.Channel{ID: "10", Name: "chat", Type: discordgo.ChannelTypeGuildText, Topic: "hello", Position: 7}, testGuild, nil, nil)
	want := model.Channel{ID: "10", Name: "chat", Type: model.Text, Topic: "hello", Position: 7}
	if !reflect.DeepEqual(text, want) {
		t.Errorf("text = %+v, want %+v", text, want)
	}
	voice, _ := toModelChannel(&discordgo.Channel{ID: "11", Name: "Talk", Type: discordgo.ChannelTypeGuildVoice, Topic: "ignored"}, testGuild, nil, nil)
	if voice.Type != model.Voice || voice.Topic != "" {
		t.Errorf("voice = %+v, want type Voice and no topic", voice)
	}
	cat, _ := toModelChannel(&discordgo.Channel{ID: "12", Name: "Lobby", Type: discordgo.ChannelTypeGuildCategory, ParentID: "5"}, testGuild, nil, map[string]string{"5": "X"})
	if cat.Type != model.Category || cat.Parent != "" {
		t.Errorf("category = %+v, want type Category and no parent", cat)
	}
}

func ch(id, parent string, typ model.ChannelType, pos int) model.Channel {
	return model.Channel{ID: id, Name: "n" + id, Type: typ, Parent: parent, Position: pos}
}

func positions(chs []model.Channel) map[string]int {
	m := map[string]int{}
	for _, c := range chs {
		m[c.ID] = c.Position
	}
	return m
}

func TestNormalizePositions(t *testing.T) {
	chs := []model.Channel{
		ch("1", "A", model.Text, 5),
		ch("2", "A", model.Text, 2),
		ch("3", "A", model.Text, 9),
		ch("4", "B", model.Text, 40), // another parent: its own numbering
	}
	normalizePositions(chs)
	want := map[string]int{"1": 1, "2": 0, "3": 2, "4": 0}
	if got := positions(chs); !reflect.DeepEqual(got, want) {
		t.Errorf("positions = %v, want %v", got, want)
	}
}

func TestNormalizePositionsVoiceAfterText(t *testing.T) {
	chs := []model.Channel{
		ch("1", "A", model.Voice, 0),
		ch("2", "A", model.Text, 1),
		ch("3", "A", model.Text, 2),
	}
	normalizePositions(chs)
	want := map[string]int{"2": 0, "3": 1, "1": 2}
	if got := positions(chs); !reflect.DeepEqual(got, want) {
		t.Errorf("positions = %v, want %v", got, want)
	}
}

func TestNormalizePositionsTiesAndCategories(t *testing.T) {
	chs := []model.Channel{
		ch("10", "A", model.Text, 3), // tie: numeric ID order, so 9 before 10
		ch("9", "A", model.Text, 3),
		ch("20", "", model.Category, 4),
		ch("21", "", model.Category, 1),
		ch("30", "", model.Text, 0), // uncategorised text is not a category sibling
	}
	normalizePositions(chs)
	want := map[string]int{"9": 0, "10": 1, "21": 0, "20": 1, "30": 0}
	if got := positions(chs); !reflect.DeepEqual(got, want) {
		t.Errorf("positions = %v, want %v", got, want)
	}
}

func TestObserveChannelsSortsLikeTheFake(t *testing.T) {
	in := []*discordgo.Channel{
		{ID: "30", Name: "talk", Type: discordgo.ChannelTypeGuildVoice, ParentID: "20", Position: 0},
		{ID: "31", Name: "chat", Type: discordgo.ChannelTypeGuildText, ParentID: "20", Position: 1},
		{ID: "20", Name: "Lobby", Type: discordgo.ChannelTypeGuildCategory, Position: 8},
		{ID: "21", Name: "Archive", Type: discordgo.ChannelTypeGuildCategory, Position: 2},
		{ID: "40", Name: "news", Type: discordgo.ChannelTypeGuildNews, Position: 0},
	}
	got := observeChannels(in, testGuild, nil)
	var order []string
	for _, c := range got {
		order = append(order, c.ID)
	}
	// Sorted by (Parent name, Position, ID): categories first ("" parent), then Lobby's children.
	want := []string{"21", "20", "31", "30"}
	if !reflect.DeepEqual(order, want) {
		t.Errorf("order = %v, want %v", order, want)
	}
	if got[2].Parent != "Lobby" || got[2].Position != 0 || got[3].Position != 1 {
		t.Errorf("children = %+v / %+v", got[2], got[3])
	}
}

func TestRolesFromDiscord(t *testing.T) {
	in := []*discordgo.Role{
		{ID: "1", Name: "Member", Color: 0xff0000, Hoist: true, Mentionable: true, Permissions: 6, Position: 3},
		{ID: testGuild, Name: "@everyone", Permissions: 1049600, Position: 0},
		{ID: "2", Name: "Bot", Managed: true, Position: 9},
		nil,
	}
	got, everyone := rolesFromDiscord(in, testGuild)
	want := []model.Role{
		{ID: "1", Name: "Member", Color: 0xff0000, Hoist: true, Mentionable: true, Permissions: 6, Position: 3},
		{ID: "2", Name: "Bot", Managed: true, Position: 9},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("roles = %+v, want %+v", got, want)
	}
	if everyone != 1049600 {
		t.Errorf("everyone = %d, want 1049600", everyone)
	}
}

func TestToRoleParamsSendsZeroValues(t *testing.T) {
	p := toRoleParams(model.Role{Name: "Member"})
	if p.Name != "Member" {
		t.Errorf("Name = %q", p.Name)
	}
	if p.Permissions == nil || *p.Permissions != 0 {
		t.Errorf("Permissions = %v, want pointer to 0", p.Permissions)
	}
	if p.Color == nil || *p.Color != 0 {
		t.Errorf("Color = %v, want pointer to 0", p.Color)
	}
	if p.Hoist == nil || *p.Hoist {
		t.Errorf("Hoist = %v, want pointer to false", p.Hoist)
	}
	if p.Mentionable == nil || *p.Mentionable {
		t.Errorf("Mentionable = %v, want pointer to false", p.Mentionable)
	}
	body, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	var wire map[string]any
	if err := json.Unmarshal(body, &wire); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"name": "Member", "color": float64(0), "hoist": false, "mentionable": false, "permissions": "0"}
	if !reflect.DeepEqual(wire, want) {
		t.Errorf("wire = %v, want %v", wire, want)
	}
}

func TestToRoleParamsValues(t *testing.T) {
	p := toRoleParams(model.Role{Name: "Director", Color: 255, Hoist: true, Mentionable: true, Permissions: 1 << 40})
	body, _ := json.Marshal(p)
	var wire map[string]any
	_ = json.Unmarshal(body, &wire)
	want := map[string]any{"name": "Director", "color": float64(255), "hoist": true, "mentionable": true, "permissions": "1099511627776"}
	if !reflect.DeepEqual(wire, want) {
		t.Errorf("wire = %v, want %v", wire, want)
	}
}

func TestToDiscordOverwrites(t *testing.T) {
	roleIDs := map[string]string{"Member": "5", "Director": "6"}
	got, err := toDiscordOverwrites([]model.Overwrite{
		{Target: "@everyone", Allow: 1, Deny: 2},
		{Target: "Member", Allow: 4, Deny: 8},
		{Target: "member:42", Allow: 16, Deny: 32},
	}, roleIDs, testGuild)
	if err != nil {
		t.Fatal(err)
	}
	want := []*discordgo.PermissionOverwrite{
		{ID: testGuild, Type: discordgo.PermissionOverwriteTypeRole, Allow: 1, Deny: 2},
		{ID: "5", Type: discordgo.PermissionOverwriteTypeRole, Allow: 4, Deny: 8},
		{ID: "42", Type: discordgo.PermissionOverwriteTypeMember, Allow: 16, Deny: 32},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}

	_, err = toDiscordOverwrites([]model.Overwrite{{Target: "Ghost"}}, roleIDs, testGuild)
	if err == nil || !strings.Contains(err.Error(), `"Ghost"`) {
		t.Errorf("unknown role: err = %v, want one naming Ghost", err)
	}

	empty, err := toDiscordOverwrites(nil, roleIDs, testGuild)
	if err != nil || empty == nil || len(empty) != 0 {
		t.Errorf("no overwrites: got %v, %v; want an empty non-nil slice", empty, err)
	}
}

func TestScrubToken(t *testing.T) {
	const token = "abc.DEF-secret_123"
	in := errors.New("Get \"https://x/y\": Authorization: Bot " + token + " failed; again " + token)
	got := scrubToken(in, token)
	if got == nil {
		t.Fatal("got nil")
	}
	if strings.Contains(got.Error(), token) {
		t.Errorf("token leaked: %q", got)
	}
	if want := "Get \"https://x/y\": Authorization: Bot *** failed; again ***"; got.Error() != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if scrubToken(nil, token) != nil {
		t.Error("nil error must stay nil")
	}
	if got := scrubToken(errors.New("plain"), ""); got == nil || got.Error() != "plain" {
		t.Errorf("empty token: got %v, want plain", got)
	}
}

func TestBuildCachesFirstWins(t *testing.T) {
	roles := []model.Role{
		{ID: "9", Name: "Member", Position: 4},
		{ID: "3", Name: "Member", Position: 2},
		{ID: "4", Name: "Other", Position: 1},
	}
	if got, want := buildRoleIDs(roles), map[string]string{"Member": "9", "Other": "4"}; !reflect.DeepEqual(got, want) {
		t.Errorf("roles = %v, want %v", got, want)
	}
	chans := []model.Channel{
		{ID: "7", Name: "chat", Type: model.Text},
		{ID: "8", Name: "chat", Type: model.Text},
		{ID: "9", Name: "chat", Type: model.Voice},
		{ID: "6", Name: "chat", Type: model.Category},
	}
	want := map[chanKey]string{
		{model.Text, "chat"}:     "7",
		{model.Voice, "chat"}:    "9",
		{model.Category, "chat"}: "6",
	}
	if got := buildChannelIDs(chans); !reflect.DeepEqual(got, want) {
		t.Errorf("channels = %v, want %v", got, want)
	}
}

func discordCh(id string, typ discordgo.ChannelType, parent string, pos int) *discordgo.Channel {
	return &discordgo.Channel{ID: id, Name: "n" + id, Type: typ, ParentID: parent, Position: pos}
}

func payloadPairs(p []*discordgo.Channel) [][2]string {
	var out [][2]string
	for _, c := range p {
		out = append(out, [2]string{c.ID, strconv.Itoa(c.Position)})
	}
	return out
}

func TestSiblingPayloadNumbersInDisplayOrder(t *testing.T) {
	text, voice := discordgo.ChannelTypeGuildText, discordgo.ChannelTypeGuildVoice
	sibs := []*discordgo.Channel{
		discordCh("5", voice, "P", 0), // voice at Discord position 0 still shows after text
		discordCh("6", text, "P", 7),
		discordCh("7", text, "P", 3),
		discordCh("8", voice, "P", 4),
	}
	// Target 6 (text) goes to index 0; the others keep their display order: 7, then voice 5, 8.
	got, err := siblingPayload(sibs, "6", 0)
	if err != nil {
		t.Fatal(err)
	}
	want := [][2]string{{"6", "0"}, {"7", "1"}, {"5", "2"}, {"8", "3"}}
	if g := payloadPairs(got); !reflect.DeepEqual(g, want) {
		t.Errorf("payload = %v, want %v", g, want)
	}
}

func TestSiblingPayloadClampsIndex(t *testing.T) {
	text := discordgo.ChannelTypeGuildText
	sibs := []*discordgo.Channel{
		discordCh("1", text, "P", 0),
		discordCh("2", text, "P", 1),
		discordCh("3", text, "P", 2),
	}
	got, err := siblingPayload(sibs, "1", 99)
	if err != nil {
		t.Fatal(err)
	}
	want := [][2]string{{"2", "0"}, {"3", "1"}, {"1", "2"}}
	if g := payloadPairs(got); !reflect.DeepEqual(g, want) {
		t.Errorf("payload = %v, want %v", g, want)
	}
	got, _ = siblingPayload(sibs, "3", -4)
	want = [][2]string{{"3", "0"}, {"1", "1"}, {"2", "2"}}
	if g := payloadPairs(got); !reflect.DeepEqual(g, want) {
		t.Errorf("negative index: payload = %v, want %v", g, want)
	}
	if _, err := siblingPayload(sibs, "404", 0); err == nil {
		t.Error("missing target: want an error")
	}
}

func TestSiblingPayloadOnlyIDAndPositionAreSerialised(t *testing.T) {
	// GuildChannelsReorder (discordgo v0.29.0 restapi.go:1067) copies only ID and
	// Position into the request body, so the payload can never carry parent_id.
	text := discordgo.ChannelTypeGuildText
	got, _ := siblingPayload([]*discordgo.Channel{discordCh("1", text, "P", 0)}, "1", 0)
	if got[0].ParentID != "" {
		t.Errorf("ParentID = %q, want empty (never sent)", got[0].ParentID)
	}
}

func TestPositionsDiffer(t *testing.T) {
	text := discordgo.ChannelTypeGuildText
	sibs := []*discordgo.Channel{discordCh("1", text, "P", 0), discordCh("2", text, "P", 1)}
	same, _ := siblingPayload(sibs, "1", 0)
	if positionsDiffer(sibs, same) {
		t.Error("identical positions reported as different")
	}
	moved, _ := siblingPayload(sibs, "1", 1)
	if !positionsDiffer(sibs, moved) {
		t.Error("moved channel reported as unchanged")
	}
}

func TestChannelPatchBody(t *testing.T) {
	ows := []*discordgo.PermissionOverwrite{{ID: "1", Type: discordgo.PermissionOverwriteTypeRole, Allow: 1}}
	wire := func(c model.Channel, parentID string, ows []*discordgo.PermissionOverwrite) map[string]any {
		b, err := json.Marshal(channelPatchBody(c, parentID, ows))
		if err != nil {
			t.Fatal(err)
		}
		var m map[string]any
		_ = json.Unmarshal(b, &m)
		return m
	}

	// A text channel always sends topic, parent_id and the overwrites, so that an
	// empty topic or no parent clears the old value (ChannelEdit's omitempty can't).
	got := wire(model.Channel{Type: model.Text}, "", []*discordgo.PermissionOverwrite{})
	want := map[string]any{"topic": "", "parent_id": nil, "permission_overwrites": []any{}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("empty text = %v, want %v", got, want)
	}

	got = wire(model.Channel{Type: model.Text, Topic: "hi"}, "55", ows)
	if got["topic"] != "hi" || got["parent_id"] != "55" {
		t.Errorf("text = %v", got)
	}
	if o, _ := got["permission_overwrites"].([]any); len(o) != 1 {
		t.Errorf("overwrites = %v", got["permission_overwrites"])
	}

	// Voice has no topic; a category has neither topic nor parent.
	got = wire(model.Channel{Type: model.Voice}, "55", ows)
	if _, has := got["topic"]; has || got["parent_id"] != "55" {
		t.Errorf("voice = %v", got)
	}
	got = wire(model.Channel{Type: model.Category}, "", ows)
	if _, has := got["topic"]; has {
		t.Errorf("category has topic: %v", got)
	}
	if _, has := got["parent_id"]; has {
		t.Errorf("category has parent_id: %v", got)
	}
}

func TestRolePositions(t *testing.T) {
	got, err := rolePositions(10, []string{"a", "b", "c"})
	if err != nil {
		t.Fatal(err)
	}
	want := []rolePosition{{"a", 9}, {"b", 8}, {"c", 7}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
	b, _ := json.Marshal(got)
	if string(b) != `[{"id":"a","position":9},{"id":"b","position":8},{"id":"c","position":7}]` {
		t.Errorf("wire = %s", b)
	}
	if _, err := rolePositions(3, []string{"a", "b", "c"}); err == nil {
		t.Error("positions below 1 (the @everyone slot): want an error")
	}
	if _, err := rolePositions(4, []string{"a", "b", "c"}); err != nil {
		t.Errorf("lowest position 1 is fine: %v", err)
	}
}

func TestNoMentionsWire(t *testing.T) {
	b, err := json.Marshal(noMentions())
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != `{"parse":[],"replied_user":false}` {
		t.Errorf("wire = %s, want parse:[] so nothing is parsed", b)
	}
}

func TestOwnMessages(t *testing.T) {
	bot := &discordgo.User{ID: "bot"}
	other := &discordgo.User{ID: "someone"}
	in := []*discordgo.Message{
		{ID: "4", Content: "d", Author: bot},
		{ID: "3", Content: "visitor", Author: other},
		{ID: "2", Content: "pin", Author: bot, Type: discordgo.MessageTypeChannelPinnedMessage},
		{ID: "1", Content: "a", Author: bot},
		{ID: "0", Content: "no author"},
		nil,
	}
	got := ownMessages(in, "bot")
	want := []model.Message{{ID: "4", Content: "d"}, {ID: "1", Content: "a"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestLiveStringHidesToken(t *testing.T) {
	l := &Live{token: "sekrit", guildID: "1"}
	for _, s := range []string{l.String(), l.GoString()} {
		if strings.Contains(s, "sekrit") {
			t.Errorf("token in %q", s)
		}
	}
}
