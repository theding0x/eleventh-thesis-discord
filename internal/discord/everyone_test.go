package discord

import (
	"context"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/bwmarrin/discordgo"

	"github.com/theding0x/eleventh-thesis-discord/internal/perms"
)

func TestDescribePermissionBits(t *testing.T) {
	tests := []struct {
		name string
		bits int64
		want []string
	}{
		{"empty", 0, nil},
		{"one named bit", discordgo.PermissionMentionEveryone, []string{"mention_everyone"}},
		{
			"named bits come out in ascending bit order whatever the input order",
			discordgo.PermissionSendPolls | discordgo.PermissionCreateInstantInvite | discordgo.PermissionUseExternalEmojis | discordgo.PermissionMentionEveryone,
			[]string{"create_instant_invite", "mention_everyone", "use_external_emojis", "send_polls"},
		},
		{"an unnamed bit", 1 << 47, []string{"bit 1<<47"}},
		{
			"named and unnamed bits mixed",
			discordgo.PermissionUseApplicationCommands | 1<<47 | discordgo.PermissionManageThreads,
			[]string{"use_application_commands", "manage_threads", "bit 1<<47"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := describePermissionBits(tt.bits)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("describePermissionBits(%#x) = %q, want %q", tt.bits, got, tt.want)
			}
			// Deterministic: a second call gives the same answer.
			if again := describePermissionBits(tt.bits); !reflect.DeepEqual(again, got) {
				t.Fatalf("second call = %q, first = %q", again, got)
			}
		})
	}
}

func TestPermissionNamesAreUniqueAndCoverTheBot(t *testing.T) {
	seen := map[string]int64{}
	for bit, name := range permissionNames {
		if other, dup := seen[name]; dup {
			t.Errorf("name %q used for bits %#x and %#x", name, other, bit)
		}
		seen[name] = bit
	}
	// Every bit the bot holds has a name, so none is ever reported as "bit 1<<N".
	for _, name := range describePermissionBits(perms.Bot) {
		if strings.HasPrefix(name, "bit ") {
			t.Errorf("a bot permission has no name: %s", name)
		}
	}
	for _, n := range perms.Names() {
		if _, ok := seen[n]; !ok {
			t.Errorf("perms name %q is missing from permissionNames", n)
		}
	}
}

func TestEveryoneForbiddenMessage(t *testing.T) {
	const (
		heldByBot = int64(discordgo.PermissionChangeNickname | discordgo.PermissionVoiceUseVAD | discordgo.PermissionSendMessages)
		notHeld   = int64(discordgo.PermissionMentionEveryone | discordgo.PermissionCreateInstantInvite | 1<<47)
	)
	desired := int64(discordgo.PermissionChangeNickname | discordgo.PermissionVoiceUseVAD)
	observed := heldByBot | notHeld

	msg := everyoneForbiddenMessage(observed, desired, true)
	for _, want := range []string{
		"cannot change @everyone permissions",
		"only lets the bot change permissions it holds",
		"Server Settings → Roles → @everyone",
		"mention_everyone", "create_instant_invite", "bit 1<<47",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("message %q does not contain %q", msg, want)
		}
	}
	// send_messages differs too, but the bot holds it, so it is not the problem.
	for _, bad := range []string{"send_messages", "change_nickname", "use_voice_activity"} {
		if strings.Contains(msg, bad) {
			t.Errorf("message %q must not name %q (the bot holds it)", msg, bad)
		}
	}
	if msg != everyoneForbiddenMessage(observed, desired, true) {
		t.Error("message is not deterministic")
	}

	// Unknown observed value: tell the user to run plan first, name no bits.
	unknown := everyoneForbiddenMessage(0, desired, false)
	if !strings.Contains(unknown, "run plan first") || strings.Contains(unknown, "mention_everyone") {
		t.Errorf("unknown-state message = %q", unknown)
	}

	// Every differing bit is one the bot holds: no bit list.
	held := everyoneForbiddenMessage(heldByBot, desired, true)
	if !strings.Contains(held, "HTTP 403") || strings.Contains(held, "the permissions to change are") {
		t.Errorf("message with no offending bits = %q", held)
	}
}

func TestIsForbidden(t *testing.T) {
	forbidden := &discordgo.RESTError{Response: &http.Response{StatusCode: http.StatusForbidden}}
	other := &discordgo.RESTError{Response: &http.Response{StatusCode: http.StatusBadRequest}}
	if !isForbidden(forbidden) {
		t.Error("a 403 RESTError is not forbidden")
	}
	if isForbidden(other) {
		t.Error("a 400 RESTError is forbidden")
	}
	if isForbidden(&discordgo.RESTError{}) || isForbidden(nil) || isForbidden(io.EOF) {
		t.Error("an error without a 403 response is forbidden")
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// A 403 from Discord on the @everyone edit becomes the diagnostic message, with
// no token in it. Other statuses keep the generic scrubbed error.
func TestUpdateEveryoneForbidden(t *testing.T) {
	const token = "MTIzNDU2Nzg5MDEyMzQ1Njc4OQ" + ".GabCdE.abcdefghijklmnopqrstuvwxyz0" // fake
	status := http.StatusForbidden
	s, err := discordgo.New("Bot " + token)
	if err != nil {
		t.Fatal(err)
	}
	s.Client = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: status,
			Status:     http.StatusText(status),
			Header:     http.Header{},
			Body:       io.NopCloser(strings.NewReader(`{"message": "Missing Permissions", "code": 50013}`)),
			Request:    r,
		}, nil
	})}
	l := &Live{s: s, token: token, guildID: "123", everyone: discordgo.PermissionMentionEveryone | discordgo.PermissionChangeNickname, everyoneKnown: true}

	err = l.UpdateEveryone(context.Background(), discordgo.PermissionChangeNickname)
	if err == nil {
		t.Fatal("want an error")
	}
	if !strings.Contains(err.Error(), "cannot change @everyone permissions") || !strings.Contains(err.Error(), "mention_everyone") {
		t.Errorf("err = %v", err)
	}
	if strings.Contains(err.Error(), token) {
		t.Errorf("err leaks the token: %v", err)
	}

	status = http.StatusBadRequest
	err = l.UpdateEveryone(context.Background(), discordgo.PermissionChangeNickname)
	if err == nil || strings.Contains(err.Error(), "cannot change @everyone") || strings.Contains(err.Error(), token) {
		t.Errorf("a 400 must stay a generic, scrubbed error: %v", err)
	}
}
