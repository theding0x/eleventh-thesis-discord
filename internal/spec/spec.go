// Package spec loads and validates server.yaml, the desired state of the
// Discord server.
package spec

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"unicode/utf8"

	"gopkg.in/yaml.v3"

	"github.com/theding0x/eleventh-thesis-discord/internal/perms"
)

// Everyone is the name that stands for the @everyone role in access maps.
const Everyone = "@everyone"

// Channel kinds, as written in the Type field ("" means text).
const (
	TypeText  = "text"
	TypeVoice = "voice"
)

const (
	maxName  = 100
	maxTopic = 1024
)

var (
	guildIDRe  = regexp.MustCompile(`^[0-9]+$`)
	colorRe    = regexp.MustCompile(`^#[0-9A-Fa-f]{6}$`)
	textNameRe = regexp.MustCompile(`^[a-z0-9-]{1,100}$`)
)

// Spec is the contents of server.yaml.
type Spec struct {
	GuildID             string                 `yaml:"guild_id"`
	ProvisionerRole     string                 `yaml:"provisioner_role"`
	EveryonePermissions []string               `yaml:"everyone_permissions"` // @everyone's server-wide permissions, set exactly
	AccessLevels        map[string]AccessLevel `yaml:"access_levels"`
	Roles               []Role                 `yaml:"roles"`
	Categories          []Category             `yaml:"categories"`
}

// AccessLevel is a named bundle of permission names. Fields decode from the
// yaml keys allow and deny.
type AccessLevel struct{ Allow, Deny []string }

// Role is a role the provisioner manages. Roles are listed highest first.
type Role struct {
	Name, Color        string
	Hoist, Mentionable bool
	Permissions        []string
}

// Category groups channels. Access maps a role name or "@everyone" to an
// access level name.
type Category struct {
	Name     string
	Access   map[string]string
	Channels []Channel
}

// Channel is a text or voice channel. Type is "", "text" or "voice", and ""
// means text.
type Channel struct {
	Name, Type, Topic, Content string
	Access                     map[string]string
}

// Load reads and decodes the spec at path, then validates it with content
// paths resolved against the directory that holds the file.
func Load(path string) (*Spec, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open spec: %w", err)
	}
	defer f.Close()

	dec := yaml.NewDecoder(f)
	dec.KnownFields(true)
	var s Spec
	if err := dec.Decode(&s); err != nil {
		return nil, fmt.Errorf("decode %s: %w", path, err)
	}
	if err := Validate(&s, filepath.Dir(path)); err != nil {
		return nil, err
	}
	return &s, nil
}

// Validate checks every rule of spec §2.2 that can be checked offline and
// returns all problems found, joined, or nil. Relative content paths are
// resolved against baseDir.
//
// The rule that nothing may grant a permission the bot lacks needs no code of
// its own: perms.Parse only knows the bot's 16 permission names, so any
// permission name outside that set is rejected as unknown.
//
// The single-message rule for content channels is checked later, when the
// content is chunked (it needs the chunker).
func Validate(s *Spec, baseDir string) error {
	var errs []error
	add := func(format string, args ...any) {
		errs = append(errs, fmt.Errorf(format, args...))
	}
	checkPerms := func(where string, names []string) {
		if _, err := perms.Parse(names); err != nil {
			add("%s: %v", where, err)
		}
	}

	if !guildIDRe.MatchString(s.GuildID) {
		add("guild_id %q must be a numeric snowflake", s.GuildID)
	}
	if s.ProvisionerRole == "" {
		add("provisioner_role is required")
	}
	checkPerms("everyone_permissions", s.EveryonePermissions)

	levels := make([]string, 0, len(s.AccessLevels))
	for name := range s.AccessLevels {
		levels = append(levels, name)
	}
	sort.Strings(levels)
	for _, name := range levels {
		lvl := s.AccessLevels[name]
		checkPerms(fmt.Sprintf("access level %q allow", name), lvl.Allow)
		checkPerms(fmt.Sprintf("access level %q deny", name), lvl.Deny)
	}

	roles := map[string]bool{}
	for _, r := range s.Roles {
		switch {
		case r.Name == "":
			add("role name is required")
		case r.Name == Everyone:
			add("role name %q is reserved for the built-in role; use everyone_permissions", Everyone)
		case r.Name == s.ProvisionerRole:
			add("role %q is the provisioner_role; it must not be declared in roles", r.Name)
		case roles[r.Name]:
			add("duplicate role %q", r.Name)
		}
		roles[r.Name] = true
		if n := utf8.RuneCountInString(r.Name); n > maxName {
			add("role %q: name is %d characters, the limit is %d", r.Name, n, maxName)
		}
		if !colorRe.MatchString(r.Color) {
			add("role %q: color %q must look like #RRGGBB", r.Name, r.Color)
		}
		checkPerms(fmt.Sprintf("role %q permissions", r.Name), r.Permissions)
	}

	checkAccess := func(where string, access map[string]string) {
		keys := make([]string, 0, len(access))
		for k := range access {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, role := range keys {
			switch {
			case role == s.ProvisionerRole && role != "":
				add("%s: access names the provisioner role %q, which gets its overwrite automatically", where, role)
			case role != Everyone && !roles[role]:
				add("%s: access names undeclared role %q", where, role)
			}
			if _, ok := s.AccessLevels[access[role]]; !ok {
				add("%s: access for %q uses undeclared access level %q", where, role, access[role])
			}
		}
	}

	categories := map[string]bool{}
	channels := map[string]bool{} // kind + "\x00" + name
	for _, c := range s.Categories {
		if categories[c.Name] {
			add("duplicate category %q", c.Name)
		}
		categories[c.Name] = true
		if n := utf8.RuneCountInString(c.Name); n < 1 || n > maxName {
			add("category %q: name must be 1-%d characters, got %d", c.Name, maxName, n)
		}
		checkAccess(fmt.Sprintf("category %q", c.Name), c.Access)

		firstVoice := "" // the first voice channel listed in this category
		for _, ch := range c.Channels {
			where := fmt.Sprintf("channel %q in category %q", ch.Name, c.Name)
			kind := ch.Type
			switch kind {
			case "", TypeText:
				kind = TypeText
			case TypeVoice:
			default:
				add("%s: type %q must be %q or %q", where, ch.Type, TypeText, TypeVoice)
			}

			key := kind + "\x00" + ch.Name
			if channels[key] {
				add("duplicate %s channel %q", kind, ch.Name)
			}
			channels[key] = true

			switch kind {
			case TypeText:
				if !textNameRe.MatchString(ch.Name) {
					add("%s: text channel names must match %s", where, textNameRe)
				}
				// Discord always shows text channels before voice channels in a
				// category, so a text channel listed after a voice channel could
				// never reach its position and would show as drift on every run.
				if firstVoice != "" {
					add("category %q: text channel %q is listed after voice channel %q; Discord shows text channels first, so list voice channels last", c.Name, ch.Name, firstVoice)
				}
			case TypeVoice:
				if firstVoice == "" {
					firstVoice = ch.Name
				}
				if n := utf8.RuneCountInString(ch.Name); n < 1 || n > maxName {
					add("%s: name must be 1-%d characters, got %d", where, maxName, n)
				}
				if ch.Content != "" {
					add("%s: content is only allowed on text channels", where)
				}
				if ch.Topic != "" {
					add("%s: topic is only allowed on text channels", where)
				}
			}

			if n := utf8.RuneCountInString(ch.Topic); n > maxTopic {
				add("%s: topic is %d characters, the limit is %d", where, n, maxTopic)
			}
			if ch.Content != "" && kind == TypeText {
				p := ch.Content
				if !filepath.IsAbs(p) {
					p = filepath.Join(baseDir, p)
				}
				if st, err := os.Stat(p); err != nil {
					add("%s: content file: %v", where, err)
				} else if !st.Mode().IsRegular() {
					add("%s: content %s is not a regular file", where, ch.Content)
				}
			}
			checkAccess(where, ch.Access)
		}
	}

	return errors.Join(errs...)
}
