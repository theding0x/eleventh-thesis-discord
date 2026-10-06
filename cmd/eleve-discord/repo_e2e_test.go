package main

import (
	"bytes"
	"sort"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/theding0x/eleventh-thesis-discord/internal/discord"
	"github.com/theding0x/eleventh-thesis-discord/internal/model"
)

// brandNewServer returns a Fake that looks like a Discord server that was just
// created: the provisioner role, Discord's default @everyone permissions and its
// default channels (the two categories, #general under Text Channels and the
// voice channel General under Voice Channels).
func brandNewServer() *discord.Fake {
	f := discord.NewFake("Provisioner")
	f.Guild.Channels = append(f.Guild.Channels,
		model.Channel{ID: "c900", Name: "Text Channels", Type: model.Category, Position: 0},
		model.Channel{ID: "c901", Name: "Voice Channels", Type: model.Category, Position: 1},
		model.Channel{ID: "c902", Name: "general", Type: model.Text, Parent: "Text Channels", Position: 0},
		model.Channel{ID: "c903", Name: "General", Type: model.Voice, Parent: "Voice Channels", Position: 0},
	)
	return f
}

// runRepo runs the CLI against the repo's real server.yaml and content/ with a
// fixed fake token, and returns the exit code, stdout and stderr.
func runRepo(f *discord.Fake, args ...string) (code int, stdout, stderr string) {
	var out, errb bytes.Buffer
	getenv := func(k string) string {
		if k == tokenEnv {
			return "tok-e2e"
		}
		return ""
	}
	dial := func(token, guildID, provisionerRole string) (discord.Client, error) { return f, nil }
	args = append(append([]string(nil), args...), "-f", repoSpec)
	code = run(args, getenv, &out, &errb, dial)
	return code, out.String(), errb.String()
}

func findChannel(t *testing.T, f *discord.Fake, typ model.ChannelType, name string) model.Channel {
	t.Helper()
	var found []model.Channel
	for _, c := range f.Guild.Channels {
		if c.Type == typ && c.Name == name {
			found = append(found, c)
		}
	}
	if len(found) != 1 {
		t.Fatalf("want exactly one channel %q of type %d, found %d", name, typ, len(found))
	}
	return found[0]
}

// applyRepoToNewServer applies the repo's spec to a brand-new server.
func applyRepoToNewServer(t *testing.T) *discord.Fake {
	t.Helper()
	f := brandNewServer()
	code, stdout, stderr := runRepo(f, "apply")
	if code != 0 || stderr != "" {
		t.Fatalf("apply: code=%d stderr=%q stdout=%q", code, stderr, stdout)
	}
	return f
}

func TestRepoEndToEndFromNewServer(t *testing.T) {
	f := brandNewServer()

	code, stdout, stderr := runRepo(f, "plan")
	if code != 2 || stderr != "" {
		t.Fatalf("first plan: code=%d stderr=%q, want 2 and no error\n%s", code, stderr, stdout)
	}
	if !strings.Contains(stdout, "~ update @everyone permissions") {
		t.Errorf("first plan does not start by updating @everyone:\n%s", stdout)
	}

	code, stdout, stderr = runRepo(f, "apply")
	if code != 0 || stderr != "" {
		t.Fatalf("apply: code=%d stderr=%q\n%s", code, stderr, stdout)
	}

	// The converged server: plan reports nothing to do and exit 0. Only the
	// objects Discord created by default are left, as unmanaged.
	code, stdout, stderr = runRepo(f, "plan")
	if code != 0 || stderr != "" {
		t.Fatalf("second plan: code=%d stderr=%q, want 0\n%s", code, stderr, stdout)
	}
	if want := "unmanaged:\n  category Text Channels\n  category Voice Channels\n  voice General\n"; stdout != want {
		t.Errorf("second plan stdout = %q, want %q", stdout, want)
	}
	for _, line := range strings.Split(strings.TrimSpace(stdout), "\n") {
		if strings.HasPrefix(line, "+ ") || strings.HasPrefix(line, "~ ") || strings.HasPrefix(line, "- ") {
			t.Errorf("second plan has an action line: %q", line)
		}
	}

	// Discord's default #general was matched by name and moved, not duplicated.
	general := findChannel(t, f, model.Text, "general")
	if general.ID != "c902" || general.Parent != "The Collective" {
		t.Errorf("#general = ID %q under %q, want the original c902 under The Collective", general.ID, general.Parent)
	}

	if n := len(f.Guild.BotMessages["apply"]); n != 1 {
		t.Errorf("#apply has %d bot messages, want exactly 1", n)
	}
	if n := len(f.Guild.BotMessages["constitution"]); n < 2 {
		t.Errorf("#constitution has %d bot messages, want at least 2", n)
	}
	var names []string
	for name, msgs := range f.Guild.BotMessages {
		names = append(names, name)
		for i, m := range msgs {
			if n := utf8.RuneCountInString(m.Content); n > 1900 {
				t.Errorf("#%s message %d has %d code points, limit 1900", name, i+1, n)
			}
		}
	}
	sort.Strings(names)
	if want := []string{"apply", "constitution", "open-ledger", "welcome"}; strings.Join(names, ",") != strings.Join(want, ",") {
		t.Errorf("channels with bot messages = %v, want %v", names, want)
	}
}

// matrixRow is one row of the access matrix in spec §1.2.
type matrixRow struct {
	typ  model.ChannelType
	name string
	// Effective access of @everyone, Probation, Member and Director.
	access [4]string
}

// specMatrix is the table in spec §1.2, in the same order.
var specMatrix = []matrixRow{
	{model.Category, "Front Door", [4]string{"read", "read", "read", "read"}},
	{model.Text, "welcome", [4]string{"read", "read", "read", "read"}},
	{model.Text, "constitution", [4]string{"read", "read", "read", "read"}},
	{model.Text, "open-ledger", [4]string{"read", "read", "read", "read"}},
	{model.Text, "apply", [4]string{"post", "post", "post", "post"}},
	{model.Text, "public-chat", [4]string{"post", "post", "post", "post"}},
	{model.Category, "The Collective", [4]string{"none", "post", "post", "post"}},
	{model.Text, "announcements", [4]string{"none", "read", "read", "post"}},
	{model.Text, "general", [4]string{"none", "post", "post", "post"}},
	{model.Text, "contributions", [4]string{"none", "post", "post", "post"}},
	{model.Text, "production", [4]string{"none", "post", "post", "post"}},
	{model.Text, "pi-and-mining", [4]string{"none", "post", "post", "post"}},
	{model.Text, "fits-and-skills", [4]string{"none", "post", "post", "post"}},
	{model.Text, "amendments", [4]string{"none", "post", "post", "post"}},
	{model.Voice, "Comms", [4]string{"none", "voice", "voice", "voice"}},
	{model.Category, "Operations", [4]string{"none", "none", "post", "post"}},
	{model.Text, "logistics", [4]string{"none", "none", "post", "post"}},
	{model.Category, "Directorate", [4]string{"none", "none", "none", "post"}},
	{model.Text, "vetting", [4]string{"none", "none", "none", "post"}},
	{model.Text, "directors", [4]string{"none", "none", "none", "post"}},
}

var matrixColumns = [4]string{"@everyone", "Probation", "Member", "Director"}

// effectiveAccess computes what a role can do in a channel with Discord's
// algorithm: the base is @everyone's server-wide permissions plus the role's
// own (none for @everyone itself); then the @everyone overwrite (deny, then
// allow); then the role's overwrite (deny, then allow). The result is named
// none (no View Channel), read, post or voice, or "other(<bits>)" for anything
// else among the channel permissions.
func effectiveAccess(t *testing.T, everyone int64, roles map[string]int64, ows []model.Overwrite, role string) string {
	t.Helper()
	perm := everyone
	if role != "@everyone" {
		perm |= roles[role]
	}
	apply := func(target string) {
		for _, o := range ows {
			if o.Target == target {
				perm &^= o.Deny
				perm |= o.Allow
			}
		}
	}
	apply("@everyone")
	if role != "@everyone" {
		apply(role)
	}

	view := repoPerms(t, "view_channel")
	history := repoPerms(t, "view_channel", "read_message_history")
	post := repoPerms(t, "view_channel", "read_message_history", "send_messages", "add_reactions", "attach_files", "embed_links")
	voice := repoPerms(t, "view_channel", "connect", "speak")
	// Only these bits say anything about access to a channel.
	channelBits := post | voice

	if perm&view == 0 {
		return "none"
	}
	switch got := perm & channelBits; got {
	case history:
		return "read"
	case post:
		return "post"
	case voice:
		return "voice"
	default:
		return "other(" + strings.TrimSpace(strings.Join(describeBits(t, got), " ")) + ")"
	}
}

// describeBits names the channel permission bits in got, for failure messages.
func describeBits(t *testing.T, got int64) []string {
	t.Helper()
	var out []string
	for _, name := range []string{"view_channel", "read_message_history", "send_messages", "add_reactions", "attach_files", "embed_links", "connect", "speak"} {
		if got&repoPerms(t, name) != 0 {
			out = append(out, name)
		}
	}
	return out
}

func TestRepoPermissionMatrix(t *testing.T) {
	f := applyRepoToNewServer(t)

	roles := map[string]int64{}
	for _, r := range f.Guild.Roles {
		roles[r.Name] = r.Permissions
	}
	for _, name := range matrixColumns[1:] {
		if _, ok := roles[name]; !ok {
			t.Fatalf("role %s was not created", name)
		}
	}
	if want := repoPerms(t, "change_nickname", "use_voice_activity"); f.Guild.EveryonePermissions != want {
		t.Fatalf("@everyone permissions = %d, want %d", f.Guild.EveryonePermissions, want)
	}
	if got := len(specMatrix); got != 20 {
		t.Fatalf("the matrix has %d rows, want 4 categories + 16 channels = 20", got)
	}

	for _, row := range specMatrix {
		ch := findChannel(t, f, row.typ, row.name)
		for i, role := range matrixColumns {
			got := effectiveAccess(t, f.Guild.EveryonePermissions, roles, ch.Overwrites, role)
			if got != row.access[i] {
				t.Errorf("%s %q: %s has %s, want %s", map[model.ChannelType]string{model.Text: "text", model.Voice: "voice", model.Category: "category"}[row.typ], row.name, role, got, row.access[i])
			}
		}
	}
}
