package main

import (
	"bytes"
	"errors"
	"os"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/theding0x/eleventh-thesis-discord/internal/content"
	"github.com/theding0x/eleventh-thesis-discord/internal/discord"
	"github.com/theding0x/eleventh-thesis-discord/internal/model"
	"github.com/theding0x/eleventh-thesis-discord/internal/perms"
	"github.com/theding0x/eleventh-thesis-discord/internal/reconcile"
	"github.com/theding0x/eleventh-thesis-discord/internal/spec"
)

const repoSpec = "../../server.yaml"

// repoChannel returns the named non-category channel of the desired state.
func repoChannel(t *testing.T, d reconcile.Desired, name string) model.Channel {
	t.Helper()
	for _, c := range d.Channels {
		if c.Type != model.Category && c.Name == name {
			return c
		}
	}
	t.Fatalf("channel %q not in the desired state", name)
	return model.Channel{}
}

// repoOverwrite returns the overwrite for target on ch.
func repoOverwrite(t *testing.T, ch model.Channel, target string) model.Overwrite {
	t.Helper()
	for _, o := range ch.Overwrites {
		if o.Target == target {
			return o
		}
	}
	t.Fatalf("channel %q has no overwrite for %q", ch.Name, target)
	return model.Overwrite{}
}

func repoPerms(t *testing.T, names ...string) int64 {
	t.Helper()
	p, err := perms.Parse(names)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestRepoServerYAML(t *testing.T) {
	s, err := spec.Load(repoSpec)
	if err != nil {
		t.Fatalf("spec.Load: %v", err)
	}
	d, err := reconcile.FromSpec(s, "../..", os.ReadFile)
	if err != nil {
		t.Fatalf("FromSpec: %v", err)
	}

	if want := repoPerms(t, "change_nickname", "use_voice_activity"); d.EveryonePermissions != want {
		t.Errorf("EveryonePermissions = %d, want %d", d.EveryonePermissions, want)
	}

	if got := len(d.Content["apply"]); got != 1 {
		t.Errorf("apply content chunks = %d, want 1", got)
	}
	if got := len(d.Content["constitution"]); got < 2 {
		t.Errorf("constitution content chunks = %d, want at least 2", got)
	}
	for name, chunks := range d.Content {
		for i, c := range chunks {
			if n := utf8.RuneCountInString(c); n > content.Limit {
				t.Errorf("%s chunk %d has %d code points, limit %d", name, i, n, content.Limit)
			}
		}
	}

	// Walk the desired channels: 4 categories, 16 channels under them.
	categories := 0
	perCategory := map[string]int{}
	for _, c := range d.Channels {
		if c.Type == model.Category {
			categories++
			continue
		}
		perCategory[c.Parent]++
	}
	if categories != 4 {
		t.Errorf("categories = %d, want 4", categories)
	}
	wantPer := map[string]int{"Front Door": 5, "The Collective": 8, "Operations": 1, "Directorate": 2}
	total := 0
	for name, want := range wantPer {
		if got := perCategory[name]; got != want {
			t.Errorf("category %q has %d channels, want %d", name, got, want)
		}
		total += want
	}
	if got := len(d.Channels) - categories; got != total || got != 16 {
		t.Errorf("non-category channels = %d, want 16", got)
	}
	if len(perCategory) != len(wantPer) {
		t.Errorf("channels sit in %d categories, want %d: %v", len(perCategory), len(wantPer), perCategory)
	}

	// The read-only content channels deny nothing but never allow posting;
	// #apply is the one content channel visitors can post in.
	send := repoPerms(t, "send_messages")
	if o := repoOverwrite(t, repoChannel(t, d, "apply"), spec.Everyone); o.Allow&send == 0 {
		t.Errorf("apply: @everyone must be allowed send_messages, got allow=%d", o.Allow)
	}
	for _, name := range []string{"welcome", "constitution", "open-ledger"} {
		if o := repoOverwrite(t, repoChannel(t, d, name), spec.Everyone); o.Allow&send != 0 {
			t.Errorf("%s: @everyone must not be allowed send_messages, got allow=%d", name, o.Allow)
		}
	}

	// #announcements: Probation reads but cannot post; Director can post.
	ann := repoChannel(t, d, "announcements")
	read := repoPerms(t, "view_channel", "read_message_history")
	prob := repoOverwrite(t, ann, "Probation")
	if prob.Allow&read != read {
		t.Errorf("announcements: Probation allow=%d, want view_channel|read_history", prob.Allow)
	}
	if prob.Allow&send != 0 {
		t.Errorf("announcements: Probation must not be allowed send_messages, got allow=%d", prob.Allow)
	}
	post := repoPerms(t, "view_channel", "read_message_history", "send_messages", "add_reactions", "attach_files", "embed_links")
	if dir := repoOverwrite(t, ann, "Director"); dir.Allow&post != post {
		t.Errorf("announcements: Director allow=%d, want the post bits %d", dir.Allow, post)
	}
}

func TestContentFilesAreLFWithTrailingNewline(t *testing.T) {
	cases := []struct{ path, heading string }{
		{"../../content/welcome.md", "# Eleventh Thesis [ELEVE]"},
		{"../../content/apply.md", "# How to join"},
		{"../../content/open-ledger.md", "# The open ledger"},
	}
	for _, c := range cases {
		raw, err := os.ReadFile(c.path)
		if err != nil {
			t.Errorf("%s: %v", c.path, err)
			continue
		}
		s := string(raw)
		if strings.Contains(s, "\r") {
			t.Errorf("%s contains a carriage return", c.path)
		}
		if !strings.HasSuffix(s, "\n") || strings.HasSuffix(s, "\n\n") {
			t.Errorf("%s must end with exactly one newline", c.path)
		}
		first, _, _ := strings.Cut(s, "\n")
		if first != c.heading {
			t.Errorf("%s starts with %q, want %q", c.path, first, c.heading)
		}
	}
}

func TestRunValidateOnRepoServerYAML(t *testing.T) {
	var out, errb bytes.Buffer
	noDial := func(token, guildID, provisionerRole string) (discord.Client, error) {
		return nil, errors.New("validate must not connect")
	}
	code := run([]string{"validate", "-f", repoSpec}, func(string) string { return "" }, &out, &errb, noDial)
	if code != 0 {
		t.Fatalf("exit %d, stderr: %s", code, errb.String())
	}
	if out.String() != "ok\n" {
		t.Errorf("stdout = %q, want %q", out.String(), "ok\n")
	}
}
