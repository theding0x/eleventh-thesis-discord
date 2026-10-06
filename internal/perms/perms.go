// Package perms provides permission names and Discord bit flag mapping.
package perms

import (
	"fmt"
	"sort"

	"github.com/bwmarrin/discordgo"
)

// permMap is the mapping from permission names to Discord bit flags.
// The 16 permissions comprise the bot's full permission set.
var permMap = map[string]int64{
	"view_channel":           int64(discordgo.PermissionViewChannel),
	"read_message_history":   int64(discordgo.PermissionReadMessageHistory),
	"send_messages":          int64(discordgo.PermissionSendMessages),
	"add_reactions":          int64(discordgo.PermissionAddReactions),
	"attach_files":           int64(discordgo.PermissionAttachFiles),
	"embed_links":            int64(discordgo.PermissionEmbedLinks),
	"connect":                int64(discordgo.PermissionVoiceConnect),
	"speak":                  int64(discordgo.PermissionVoiceSpeak),
	"manage_roles":           int64(discordgo.PermissionManageRoles),
	"manage_channels":        int64(discordgo.PermissionManageChannels),
	"manage_messages":        int64(discordgo.PermissionManageMessages),
	"kick_members":           int64(discordgo.PermissionKickMembers),
	"ban_members":            int64(discordgo.PermissionBanMembers),
	"moderate_members":       int64(discordgo.PermissionModerateMembers),
	"change_nickname":        int64(discordgo.PermissionChangeNickname),
	"use_voice_activity":     int64(discordgo.PermissionVoiceUseVAD),
}

// Bot is the union of all 16 bot permissions.
var Bot int64

func init() {
	for _, perm := range permMap {
		Bot |= perm
	}
}

// Parse ORs the bit flags for each permission name.
// An unknown name returns an error naming it.
func Parse(names []string) (int64, error) {
	var result int64
	for _, name := range names {
		perm, ok := permMap[name]
		if !ok {
			return 0, fmt.Errorf("unknown permission: %s", name)
		}
		result |= perm
	}
	return result, nil
}

// Names returns the 16 permission names, sorted.
func Names() []string {
	names := make([]string, 0, len(permMap))
	for name := range permMap {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
