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

// TestPermsBotGolden pins the permissions integer in the invite URL. It is the sum
// of the 16 bits below; change it only together with spec §2.6.
//
//	bit  1<<1   kick_members
//	bit  1<<2   ban_members
//	bit  1<<4   manage_channels
//	bit  1<<6   add_reactions
//	bit  1<<10  view_channel
//	bit  1<<11  send_messages
//	bit  1<<13  manage_messages
//	bit  1<<14  embed_links
//	bit  1<<15  attach_files
//	bit  1<<16  read_message_history
//	bit  1<<20  connect
//	bit  1<<21  speak
//	bit  1<<25  use_voice_activity
//	bit  1<<26  change_nickname
//	bit  1<<28  manage_roles
//	bit  1<<40  moderate_members
func TestPermsBotGolden(t *testing.T) {
	if Bot != 1099883998294 {
		t.Fatalf("Bot = %d, want 1099883998294", Bot)
	}
}

func TestBotExcludesAdministrator(t *testing.T) {
	if Bot&discordgo.PermissionAdministrator != 0 {
		t.Fatal("Bot includes Administrator")
	}
}
