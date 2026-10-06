package spec

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func loadValid(t *testing.T) *Spec {
	t.Helper()
	s, err := Load(filepath.Join("testdata", "valid.yaml"))
	if err != nil {
		t.Fatalf("Load(valid.yaml): %v", err)
	}
	return s
}

func TestLoadValid(t *testing.T) {
	s := loadValid(t)
	if len(s.Roles) != 3 {
		t.Errorf("roles = %d, want 3", len(s.Roles))
	}
	if len(s.Categories) != 4 {
		t.Errorf("categories = %d, want 4", len(s.Categories))
	}
	if s.GuildID != "1" || s.ProvisionerRole != "Provisioner" {
		t.Errorf("guild/provisioner = %q/%q", s.GuildID, s.ProvisionerRole)
	}
	if len(s.EveryonePermissions) != 2 {
		t.Errorf("everyone_permissions = %v, want 2 entries", s.EveryonePermissions)
	}
	if got := s.AccessLevels["post"].Allow; len(got) != 6 {
		t.Errorf("post allow = %v, want 6 entries", got)
	}
	if got := s.AccessLevels["none"].Deny; len(got) != 1 || got[0] != "view_channel" {
		t.Errorf("none deny = %v", got)
	}
	if !s.Roles[0].Mentionable || !s.Roles[0].Hoist || s.Roles[0].Color != "#B22222" {
		t.Errorf("Director = %+v", s.Roles[0])
	}
}

func TestLoadRejectsUnknownField(t *testing.T) {
	p := filepath.Join(t.TempDir(), "server.yaml")
	if err := os.WriteFile(p, []byte("guild_id: \"1\"\nbogus: true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(p); err == nil || !strings.Contains(err.Error(), "bogus") {
		t.Fatalf("Load = %v, want error naming bogus", err)
	}
}

func TestLoadMissingFile(t *testing.T) {
	if _, err := Load(filepath.Join("testdata", "nope.yaml")); err == nil {
		t.Fatal("Load of missing file succeeded")
	}
}

// category returns the named category of s.
func category(t *testing.T, s *Spec, name string) *Category {
	t.Helper()
	for i := range s.Categories {
		if s.Categories[i].Name == name {
			return &s.Categories[i]
		}
	}
	t.Fatalf("no category %q", name)
	return nil
}

// channel returns the named channel of c.
func channel(t *testing.T, c *Category, name string) *Channel {
	t.Helper()
	for i := range c.Channels {
		if c.Channels[i].Name == name {
			return &c.Channels[i]
		}
	}
	t.Fatalf("no channel %q in %q", name, c.Name)
	return nil
}

func TestValidate(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(t *testing.T, s *Spec)
		want   []string
	}{
		{"missing guild", func(t *testing.T, s *Spec) { s.GuildID = "" }, []string{"guild_id"}},
		{"non-numeric guild", func(t *testing.T, s *Spec) { s.GuildID = "abc" }, []string{"guild_id"}},
		{"missing provisioner", func(t *testing.T, s *Spec) { s.ProvisionerRole = "" }, []string{"provisioner_role"}},
		{"provisioner declared as role", func(t *testing.T, s *Spec) {
			s.Roles = append(s.Roles, Role{Name: "Provisioner", Color: "#000000"})
		}, []string{"provisioner_role"}},
		{"duplicate role", func(t *testing.T, s *Spec) {
			s.Roles = append(s.Roles, Role{Name: "Member", Color: "#000000"})
		}, []string{`duplicate role "Member"`}},
		{"role named @everyone", func(t *testing.T, s *Spec) { s.Roles[1].Name = "@everyone" }, []string{"@everyone"}},
		{"bad colour", func(t *testing.T, s *Spec) { s.Roles[0].Color = "red" }, []string{"color"}},
		{"unknown permission", func(t *testing.T, s *Spec) {
			s.Roles[0].Permissions = []string{"administrator"}
		}, []string{"administrator"}},
		{"unknown permission in level def", func(t *testing.T, s *Spec) {
			s.AccessLevels["x"] = AccessLevel{Allow: []string{"fly"}}
		}, []string{"fly"}},
		{"unknown @everyone permission", func(t *testing.T, s *Spec) {
			s.EveryonePermissions = []string{"mention_everyone"}
		}, []string{"mention_everyone"}},
		{"undeclared role in access", func(t *testing.T, s *Spec) {
			category(t, s, "Operations").Access["Pilot"] = "post"
		}, []string{`"Pilot"`}},
		{"undeclared level", func(t *testing.T, s *Spec) {
			category(t, s, "Operations").Access["Member"] = "shout"
		}, []string{`"shout"`}},
		{"undeclared role in channel access", func(t *testing.T, s *Spec) {
			channel(t, category(t, s, "Operations"), "logistics").Access = map[string]string{"Pilot": "post"}
		}, []string{`"Pilot"`}},
		{"provisioner is not an access key", func(t *testing.T, s *Spec) {
			category(t, s, "Operations").Access["Provisioner"] = "post"
		}, []string{`"Provisioner"`}},
		{"duplicate category", func(t *testing.T, s *Spec) {
			s.Categories = append(s.Categories, Category{Name: "Operations"})
		}, []string{"duplicate category"}},
		{"duplicate channel", func(t *testing.T, s *Spec) {
			c := category(t, s, "Operations")
			c.Channels = append(c.Channels, Channel{Name: "general"})
		}, []string{`duplicate text channel "general"`}},
		{"bad text name", func(t *testing.T, s *Spec) {
			c := category(t, s, "Operations")
			c.Channels = append(c.Channels, Channel{Name: "General Chat"})
		}, []string{"General Chat"}},
		{"bad type", func(t *testing.T, s *Spec) {
			channel(t, category(t, s, "Operations"), "logistics").Type = "stage"
		}, []string{"stage"}},
		{"content on voice", func(t *testing.T, s *Spec) {
			channel(t, category(t, s, "The Collective"), "Comms").Content = "content/welcome.md"
		}, []string{"content"}},
		{"topic on voice", func(t *testing.T, s *Spec) {
			channel(t, category(t, s, "The Collective"), "Comms").Topic = "hello"
		}, []string{"topic"}},
		{"missing content file", func(t *testing.T, s *Spec) {
			channel(t, category(t, s, "Front Door"), "welcome").Content = "content/nope.md"
		}, []string{"nope.md"}},
		{"topic too long", func(t *testing.T, s *Spec) {
			channel(t, category(t, s, "Operations"), "logistics").Topic = strings.Repeat("x", 1025)
		}, []string{"topic"}},
		{"text channel after voice channel", func(t *testing.T, s *Spec) {
			c := category(t, s, "The Collective") // Comms (voice) is its last channel
			c.Channels = append(c.Channels, Channel{Name: "late-text"})
		}, []string{`category "The Collective"`, `text channel "late-text"`, `listed after voice channel "Comms"`, "list voice channels last"}},
		{"voice channel listed before a text channel", func(t *testing.T, s *Spec) {
			c := category(t, s, "Operations")
			c.Channels = []Channel{{Name: "Lounge", Type: "voice"}, {Name: "logistics"}}
		}, []string{`category "Operations"`, `text channel "logistics"`, `listed after voice channel "Lounge"`}},
		{"several problems", func(t *testing.T, s *Spec) {
			s.GuildID = "abc"
			s.Roles[0].Color = "red"
		}, []string{"guild_id", "color"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := loadValid(t)
			tc.mutate(t, s)
			err := Validate(s, "testdata")
			if err == nil {
				t.Fatalf("Validate succeeded, want error containing %q", tc.want)
			}
			for _, w := range tc.want {
				if !strings.Contains(err.Error(), w) {
					t.Errorf("error %q does not contain %q", err, w)
				}
			}
		})
	}
}

func TestValidateAcceptsBoundaryValues(t *testing.T) {
	s := loadValid(t)
	c := category(t, s, "Operations")
	c.Channels = append(c.Channels,
		Channel{Name: strings.Repeat("a", 100), Topic: strings.Repeat("é", 1024)},
		Channel{Name: "Voice Room With Spaces", Type: "voice"},
		Channel{Name: "logistics", Type: "voice"}, // same name is fine across kinds
	)
	s.EveryonePermissions = nil
	if err := Validate(s, "testdata"); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestValidateLengthLimits(t *testing.T) {
	s := loadValid(t)
	c := category(t, s, "Operations")
	c.Channels = append(c.Channels, Channel{Name: strings.Repeat("a", 101)})
	c.Channels = append(c.Channels, Channel{Name: strings.Repeat("v", 101), Type: "voice"})
	s.Categories = append(s.Categories, Category{Name: ""})
	err := Validate(s, "testdata")
	if err == nil {
		t.Fatal("Validate succeeded")
	}
	if n := strings.Count(err.Error(), "\n") + 1; n != 3 {
		t.Errorf("got %d problems, want 3: %v", n, err)
	}
}
