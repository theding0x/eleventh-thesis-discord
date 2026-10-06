// Package reconcile turns the spec into a desired state and compares it with
// the observed guild.
package reconcile

import (
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/theding0x/eleventh-thesis-discord/internal/content"
	"github.com/theding0x/eleventh-thesis-discord/internal/model"
	"github.com/theding0x/eleventh-thesis-discord/internal/perms"
	"github.com/theding0x/eleventh-thesis-discord/internal/spec"
)

// Desired is the state the spec asks for.
type Desired struct {
	ProvisionerRole     string
	EveryonePermissions int64               // perms.Parse(spec.EveryonePermissions)
	Roles               []model.Role        // hierarchy order, highest first
	Channels            []model.Channel     // categories in order, then each category's channels in order
	Content             map[string][]string // text channel name → chunks
}

// FromSpec builds the desired state from a validated spec. Content paths are
// resolved against baseDir and read with readFile.
func FromSpec(s *spec.Spec, baseDir string, readFile func(string) ([]byte, error)) (Desired, error) {
	everyone, err := perms.Parse(s.EveryonePermissions)
	if err != nil {
		return Desired{}, fmt.Errorf("everyone_permissions: %w", err)
	}
	provisioner, err := perms.Parse([]string{"view_channel", "read_message_history", "send_messages", "manage_messages"})
	if err != nil {
		return Desired{}, err
	}
	send, err := perms.Parse([]string{"send_messages"})
	if err != nil {
		return Desired{}, err
	}

	d := Desired{
		ProvisionerRole:     s.ProvisionerRole,
		EveryonePermissions: everyone,
		Content:             map[string][]string{},
	}

	for _, r := range s.Roles {
		p, err := perms.Parse(r.Permissions)
		if err != nil {
			return Desired{}, fmt.Errorf("role %q: %w", r.Name, err)
		}
		color, err := strconv.ParseUint(strings.TrimPrefix(r.Color, "#"), 16, 32)
		if err != nil {
			return Desired{}, fmt.Errorf("role %q: color %q: %w", r.Name, r.Color, err)
		}
		d.Roles = append(d.Roles, model.Role{
			Name:        r.Name,
			Color:       int(color),
			Hoist:       r.Hoist,
			Mentionable: r.Mentionable,
			Permissions: p,
		})
	}

	// overwrites resolves an access map into the sorted overwrite list,
	// adding the provisioner's own.
	overwrites := func(where string, access map[string]string) ([]model.Overwrite, error) {
		out := make([]model.Overwrite, 0, len(access)+1)
		for target, levelName := range access {
			lvl, ok := s.AccessLevels[levelName]
			if !ok {
				return nil, fmt.Errorf("%s: access for %q uses undeclared access level %q", where, target, levelName)
			}
			allow, err := perms.Parse(lvl.Allow)
			if err != nil {
				return nil, fmt.Errorf("%s: access level %q allow: %w", where, levelName, err)
			}
			deny, err := perms.Parse(lvl.Deny)
			if err != nil {
				return nil, fmt.Errorf("%s: access level %q deny: %w", where, levelName, err)
			}
			out = append(out, model.Overwrite{Target: target, Allow: allow, Deny: deny})
		}
		out = append(out, model.Overwrite{Target: s.ProvisionerRole, Allow: provisioner})
		sort.Slice(out, func(i, j int) bool { return out[i].Target < out[j].Target })
		return out, nil
	}

	// open reports whether someone other than the provisioner can post.
	open := func(ows []model.Overwrite) bool {
		everyoneDenied := false
		for _, o := range ows {
			if o.Target == s.ProvisionerRole {
				continue
			}
			if o.Allow&send != 0 {
				return true
			}
			if o.Target == spec.Everyone && o.Deny&send != 0 {
				everyoneDenied = true
			}
		}
		return everyone&send != 0 && !everyoneDenied
	}

	var channels []model.Channel
	for i, c := range s.Categories {
		ows, err := overwrites(fmt.Sprintf("category %q", c.Name), c.Access)
		if err != nil {
			return Desired{}, err
		}
		d.Channels = append(d.Channels, model.Channel{
			Name:       c.Name,
			Type:       model.Category,
			Position:   i,
			Overwrites: ows,
		})

		for j, ch := range c.Channels {
			where := fmt.Sprintf("channel %q in category %q", ch.Name, c.Name)
			// The category's access is copied; each role the channel names
			// replaces the category's entry for that role.
			access := make(map[string]string, len(c.Access)+len(ch.Access))
			for role, level := range c.Access {
				access[role] = level
			}
			for role, level := range ch.Access {
				access[role] = level
			}
			ows, err := overwrites(where, access)
			if err != nil {
				return Desired{}, err
			}

			mc := model.Channel{
				Name:       ch.Name,
				Parent:     c.Name,
				Position:   j,
				Topic:      ch.Topic,
				Overwrites: ows,
			}
			switch ch.Type {
			case "", spec.TypeText:
				mc.Type = model.Text
			case spec.TypeVoice:
				mc.Type = model.Voice
			default:
				return Desired{}, fmt.Errorf("%s: unknown type %q", where, ch.Type)
			}

			if ch.Content != "" && mc.Type == model.Text {
				p := ch.Content
				if !filepath.IsAbs(p) {
					p = filepath.Join(baseDir, p)
				}
				raw, err := readFile(p)
				if err != nil {
					return Desired{}, fmt.Errorf("%s: read content: %w", where, err)
				}
				md := strings.ReplaceAll(string(raw), "\r\n", "\n")
				chunks := content.Chunk(md, content.Limit)
				if len(chunks) != 1 && open(ows) {
					return Desired{}, fmt.Errorf("channel %q: content must fit in one message because others can post there", ch.Name)
				}
				d.Content[ch.Name] = chunks
			}
			channels = append(channels, mc)
		}
	}
	d.Channels = append(d.Channels, channels...)
	return d, nil
}
