package perms

import (
	"strings"
	"testing"

	"github.com/bwmarrin/discordgo"
)

func TestParse(t *testing.T) {
	got, err := Parse([]string{"view_channel", "send_messages"})
	if err != nil {
		t.Fatal(err)
	}
	if want := int64(discordgo.PermissionViewChannel | discordgo.PermissionSendMessages); got != want {
		t.Fatalf("got %d want %d", got, want)
	}
}

func TestParseUnknown(t *testing.T) {
	_, err := Parse([]string{"administrator"})
	if err == nil || !strings.Contains(err.Error(), "administrator") {
		t.Fatalf("err = %v", err)
	}
}

func TestBotIsUnionOfAllNames(t *testing.T) {
	if n := len(Names()); n != 16 {
		t.Fatalf("names = %d", n)
	}
	all, _ := Parse(Names())
	if all != Bot {
		t.Fatal("Bot != union of Names")
	}
}

func TestBotExcludesAdministrator(t *testing.T) {
	if Bot&discordgo.PermissionAdministrator != 0 {
		t.Fatal("Bot includes Administrator")
	}
}
