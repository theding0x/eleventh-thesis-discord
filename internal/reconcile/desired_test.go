package reconcile

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/theding0x/eleventh-thesis-discord/internal/content"
	"github.com/theding0x/eleventh-thesis-discord/internal/model"
	"github.com/theding0x/eleventh-thesis-discord/internal/perms"
	"github.com/theding0x/eleventh-thesis-discord/internal/spec"
)

const testdata = "../spec/testdata"

func mustParse(t *testing.T, names ...string) int64 {
	t.Helper()
	v, err := perms.Parse(names)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func loadSpec(t *testing.T) *spec.Spec {
	t.Helper()
	s, err := spec.Load(filepath.Join(testdata, "valid.yaml"))
	if err != nil {
		t.Fatalf("spec.Load: %v", err)
	}
	return s
}

func fromSpec(t *testing.T, s *spec.Spec, read func(string) ([]byte, error)) (Desired, error) {
	t.Helper()
	if read == nil {
		read = os.ReadFile
	}
	return FromSpec(s, testdata, read)
}

func mustDesired(t *testing.T) Desired {
	t.Helper()
	d, err := fromSpec(t, loadSpec(t), nil)
	if err != nil {
		t.Fatalf("FromSpec: %v", err)
	}
	return d
}

func findChannel(t *testing.T, d Desired, typ model.ChannelType, name string) model.Channel {
	t.Helper()
	for _, c := range d.Channels {
		if c.Type == typ && c.Name == name {
			return c
		}
	}
	t.Fatalf("channel %q (type %d) not found", name, typ)
	return model.Channel{}
}

func overwrite(t *testing.T, c model.Channel, target string) model.Overwrite {
	t.Helper()
	for _, o := range c.Overwrites {
		if o.Target == target {
			return o
		}
	}
	t.Fatalf("channel %q has no overwrite for %q: %+v", c.Name, target, c.Overwrites)
	return model.Overwrite{}
}

// threeThousand is ~3,000 characters of paragraphs: more than one chunk.
func threeThousand() string {
	return strings.Repeat(strings.Repeat("word ", 100)+"\n\n", 6)
}

// replaceFile returns a readFile that serves body for the named file and
// reads everything else from disk.
func replaceFile(name, body string) func(string) ([]byte, error) {
	return func(p string) ([]byte, error) {
		if filepath.Base(p) == name {
			return []byte(body), nil
		}
		return os.ReadFile(p)
	}
}

func TestFromSpecCategoryOverwrites(t *testing.T) {
	d := mustDesired(t)
	c := findChannel(t, d, model.Category, "The Collective")
	post := mustParse(t, "view_channel", "read_message_history", "send_messages", "add_reactions", "attach_files", "embed_links")
	want := []model.Overwrite{
		{Target: "@everyone", Deny: mustParse(t, "view_channel")},
		{Target: "Director", Allow: post},
		{Target: "Member", Allow: post},
		{Target: "Probation", Allow: post},
		{Target: "Provisioner", Allow: mustParse(t, "view_channel", "read_message_history", "send_messages", "manage_messages")},
	}
	if !reflect.DeepEqual(c.Overwrites, want) {
		t.Errorf("overwrites =\n%+v\nwant\n%+v", c.Overwrites, want)
	}
}

func TestFromSpecChannelReplacesCategoryLevel(t *testing.T) {
	d := mustDesired(t)
	c := findChannel(t, d, model.Text, "announcements")
	post := mustParse(t, "view_channel", "read_message_history", "send_messages", "add_reactions", "attach_files", "embed_links")
	read := mustParse(t, "view_channel", "read_message_history")
	want := []model.Overwrite{
		{Target: "@everyone", Deny: mustParse(t, "view_channel")},
		{Target: "Director", Allow: post},
		{Target: "Member", Allow: read},
		{Target: "Probation", Allow: read},
		{Target: "Provisioner", Allow: mustParse(t, "view_channel", "read_message_history", "send_messages", "manage_messages")},
	}
	if !reflect.DeepEqual(c.Overwrites, want) {
		t.Errorf("overwrites =\n%+v\nwant\n%+v", c.Overwrites, want)
	}
}

func TestFromSpecVoice(t *testing.T) {
	d := mustDesired(t)
	c := findChannel(t, d, model.Voice, "Comms")
	if c.Parent != "The Collective" {
		t.Errorf("Parent = %q", c.Parent)
	}
	voice := mustParse(t, "view_channel", "connect", "speak")
	want := []model.Overwrite{
		{Target: "@everyone", Deny: mustParse(t, "view_channel")},
		{Target: "Director", Allow: voice},
		{Target: "Member", Allow: voice},
		{Target: "Probation", Allow: voice},
		{Target: "Provisioner", Allow: mustParse(t, "view_channel", "read_message_history", "send_messages", "manage_messages")},
	}
	if !reflect.DeepEqual(c.Overwrites, want) {
		t.Errorf("overwrites =\n%+v\nwant\n%+v", c.Overwrites, want)
	}
}

func TestFromSpecPositions(t *testing.T) {
	d := mustDesired(t)
	for i, name := range []string{"Front Door", "The Collective", "Operations", "Directorate"} {
		c := findChannel(t, d, model.Category, name)
		if c.Position != i || c.Parent != "" {
			t.Errorf("category %q: Position %d Parent %q, want %d and empty", name, c.Position, c.Parent, i)
		}
		if d.Channels[i].Name != name {
			t.Errorf("Channels[%d] = %q, want %q (categories come first, in order)", i, d.Channels[i].Name, name)
		}
	}
	pc := findChannel(t, d, model.Text, "public-chat")
	if pc.Parent != "Front Door" || pc.Position != 4 {
		t.Errorf("public-chat: Parent %q Position %d, want Front Door and 4", pc.Parent, pc.Position)
	}
	if d.Channels[4].Name != "welcome" || d.Channels[4].Position != 0 {
		t.Errorf("Channels[4] = %+v, want welcome at position 0", d.Channels[4])
	}
	if got := findChannel(t, d, model.Voice, "Comms").Position; got != 7 {
		t.Errorf("Comms Position = %d, want 7", got)
	}
	if got := findChannel(t, d, model.Text, "logistics").Position; got != 0 {
		t.Errorf("logistics Position = %d, want 0 (positions restart per category)", got)
	}
	if n := len(d.Channels); n != 4+5+8+1+2 {
		t.Errorf("len(Channels) = %d, want 20", n)
	}
}

func TestFromSpecTopic(t *testing.T) {
	d := mustDesired(t)
	if c := findChannel(t, d, model.Text, "contributions"); c.Topic != "Deliveries, consignment queue and ledger questions." {
		t.Errorf("topic = %q", c.Topic)
	}
}

func TestFromSpecRoleColor(t *testing.T) {
	d := mustDesired(t)
	if len(d.Roles) != 3 {
		t.Fatalf("roles = %d, want 3 (the provisioner role is not desired)", len(d.Roles))
	}
	dir := d.Roles[0]
	if dir.Name != "Director" || dir.Color != 0xB22222 || !dir.Hoist || !dir.Mentionable {
		t.Errorf("Director = %+v", dir)
	}
	if want := mustParse(t, "manage_roles", "manage_messages", "kick_members", "ban_members", "moderate_members"); dir.Permissions != want {
		t.Errorf("Director permissions = %b, want %b", dir.Permissions, want)
	}
	if d.Roles[1].Name != "Member" || d.Roles[1].Color != 0xC0C0C0 || d.Roles[2].Name != "Probation" {
		t.Errorf("order/colours wrong: %+v", d.Roles)
	}
	if d.ProvisionerRole != "Provisioner" {
		t.Errorf("ProvisionerRole = %q", d.ProvisionerRole)
	}
}

func TestFromSpecContentChunks(t *testing.T) {
	d := mustDesired(t)
	raw, err := os.ReadFile(filepath.Join(testdata, "content", "welcome.md"))
	if err != nil {
		t.Fatal(err)
	}
	want := content.Chunk(strings.ReplaceAll(string(raw), "\r\n", "\n"), content.Limit)
	if got := d.Content["welcome"]; len(got) == 0 || !reflect.DeepEqual(got, want) {
		t.Errorf("Content[welcome] = %q, want %q", got, want)
	}
	if n := len(d.Content["constitution"]); n < 2 {
		t.Errorf("constitution chunks = %d, want >= 2", n)
	}
	if _, ok := d.Content["general"]; ok {
		t.Errorf("channel without content has a Content entry")
	}
}

func TestFromSpecContentCRLF(t *testing.T) {
	read := func(p string) ([]byte, error) {
		b, err := os.ReadFile(p)
		if err != nil {
			return nil, err
		}
		return []byte(strings.ReplaceAll(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n", "\r\n")), nil
	}
	crlf, err := fromSpec(t, loadSpec(t), read)
	if err != nil {
		t.Fatal(err)
	}
	want := mustDesired(t).Content
	if !reflect.DeepEqual(crlf.Content, want) {
		t.Errorf("CRLF input chunked differently from LF input")
	}
}

func TestFromSpecOpenContentChannelMustBeOneMessage(t *testing.T) {
	_, err := fromSpec(t, loadSpec(t), replaceFile("apply.md", threeThousand()))
	if err == nil || !strings.Contains(err.Error(), "one message") || !strings.Contains(err.Error(), `"apply"`) {
		t.Fatalf("err = %v, want one-message error naming apply", err)
	}
}

func TestFromSpecEveryonePermissions(t *testing.T) {
	d := mustDesired(t)
	if want := mustParse(t, "change_nickname", "use_voice_activity"); d.EveryonePermissions != want {
		t.Errorf("EveryonePermissions = %b, want %b", d.EveryonePermissions, want)
	}
}

func TestFromSpecEveryoneBaseCountsAsOpen(t *testing.T) {
	s := loadSpec(t)
	s.EveryonePermissions = []string{"send_messages"}
	_, err := fromSpec(t, s, replaceFile("welcome.md", threeThousand()))
	if err == nil || !strings.Contains(err.Error(), "one message") {
		t.Fatalf("err = %v, want one-message error", err)
	}
}

func TestFromSpecEveryoneDenyKeepsChannelClosed(t *testing.T) {
	// With send_messages in the @everyone base, a multi-chunk channel is fine
	// as long as its @everyone overwrite denies sending and nobody else can post.
	s := loadSpec(t)
	s.EveryonePermissions = []string{"send_messages"}
	s.AccessLevels["quiet"] = spec.AccessLevel{
		Allow: []string{"view_channel", "read_message_history"},
		Deny:  []string{"send_messages"},
	}
	s.Categories[0].Access["@everyone"] = "quiet"
	s.Categories[0].Channels[3].Access["@everyone"] = "quiet" // apply: nobody posts any more
	if _, err := fromSpec(t, s, replaceFile("welcome.md", threeThousand())); err != nil {
		t.Fatalf("FromSpec: %v", err)
	}
}

func TestFromSpecReadFileError(t *testing.T) {
	read := func(string) ([]byte, error) { return nil, os.ErrNotExist }
	if _, err := fromSpec(t, loadSpec(t), read); err == nil {
		t.Fatal("want error when a content file cannot be read")
	}
}
