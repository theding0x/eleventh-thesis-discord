package discord

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/bwmarrin/discordgo"

	"github.com/theding0x/eleventh-thesis-discord/internal/perms"
)

// permissionNames names Discord's documented permission bits, using discordgo's
// Permission* constants. Names follow the snake_case style of package perms.
// A bit that is not here is reported as "bit 1<<N".
var permissionNames = map[int64]string{
	discordgo.PermissionCreateInstantInvite:              "create_instant_invite",
	discordgo.PermissionKickMembers:                      "kick_members",
	discordgo.PermissionBanMembers:                       "ban_members",
	discordgo.PermissionAdministrator:                    "administrator",
	discordgo.PermissionManageChannels:                   "manage_channels",
	discordgo.PermissionManageGuild:                      "manage_guild",
	discordgo.PermissionAddReactions:                     "add_reactions",
	discordgo.PermissionViewAuditLogs:                    "view_audit_log",
	discordgo.PermissionVoicePrioritySpeaker:             "priority_speaker",
	discordgo.PermissionVoiceStreamVideo:                 "stream",
	discordgo.PermissionViewChannel:                      "view_channel",
	discordgo.PermissionSendMessages:                     "send_messages",
	discordgo.PermissionSendTTSMessages:                  "send_tts_messages",
	discordgo.PermissionManageMessages:                   "manage_messages",
	discordgo.PermissionEmbedLinks:                       "embed_links",
	discordgo.PermissionAttachFiles:                      "attach_files",
	discordgo.PermissionReadMessageHistory:               "read_message_history",
	discordgo.PermissionMentionEveryone:                  "mention_everyone",
	discordgo.PermissionUseExternalEmojis:                "use_external_emojis",
	discordgo.PermissionViewGuildInsights:                "view_guild_insights",
	discordgo.PermissionVoiceConnect:                     "connect",
	discordgo.PermissionVoiceSpeak:                       "speak",
	discordgo.PermissionVoiceMuteMembers:                 "mute_members",
	discordgo.PermissionVoiceDeafenMembers:               "deafen_members",
	discordgo.PermissionVoiceMoveMembers:                 "move_members",
	discordgo.PermissionVoiceUseVAD:                      "use_voice_activity",
	discordgo.PermissionChangeNickname:                   "change_nickname",
	discordgo.PermissionManageNicknames:                  "manage_nicknames",
	discordgo.PermissionManageRoles:                      "manage_roles",
	discordgo.PermissionManageWebhooks:                   "manage_webhooks",
	discordgo.PermissionManageGuildExpressions:           "manage_guild_expressions",
	discordgo.PermissionUseApplicationCommands:           "use_application_commands",
	discordgo.PermissionVoiceRequestToSpeak:              "request_to_speak",
	discordgo.PermissionManageEvents:                     "manage_events",
	discordgo.PermissionManageThreads:                    "manage_threads",
	discordgo.PermissionCreatePublicThreads:              "create_public_threads",
	discordgo.PermissionCreatePrivateThreads:             "create_private_threads",
	discordgo.PermissionUseExternalStickers:              "use_external_stickers",
	discordgo.PermissionSendMessagesInThreads:            "send_messages_in_threads",
	discordgo.PermissionUseEmbeddedActivities:            "use_embedded_activities",
	discordgo.PermissionModerateMembers:                  "moderate_members",
	discordgo.PermissionViewCreatorMonetizationAnalytics: "view_creator_monetization_analytics",
	discordgo.PermissionUseSoundboard:                    "use_soundboard",
	discordgo.PermissionCreateGuildExpressions:           "create_guild_expressions",
	discordgo.PermissionCreateEvents:                     "create_events",
	discordgo.PermissionUseExternalSounds:                "use_external_sounds",
	discordgo.PermissionSendVoiceMessages:                "send_voice_messages",
	discordgo.PermissionSendPolls:                        "send_polls",
	discordgo.PermissionUseExternalApps:                  "use_external_apps",
}

// describePermissionBits lists the permissions set in bits by name, in
// ascending bit order. A bit without a name is listed as "bit 1<<N".
func describePermissionBits(bits int64) []string {
	var out []string
	for n := 0; n < 63; n++ {
		bit := int64(1) << n
		if bits&bit == 0 {
			continue
		}
		if name, ok := permissionNames[bit]; ok {
			out = append(out, name)
		} else {
			out = append(out, fmt.Sprintf("bit 1<<%d", n))
		}
	}
	return out
}

// everyoneForbiddenMessage explains a 403 on the @everyone update. Discord does
// not let a bot change permissions it does not hold, so the offending bits are
// those that differ between observed and desired and are outside perms.Bot.
// known says whether observed came from an Observe.
func everyoneForbiddenMessage(observed, desired int64, known bool) string {
	const how = "Discord only lets the bot change permissions it holds; " +
		"switch the extra permissions off by hand in Server Settings → Roles → @everyone, " +
		"as the launch checklist says, then run again"
	if !known {
		return "cannot change @everyone permissions: " + how + " (run plan first to see which permissions differ)"
	}
	bits := describePermissionBits((observed ^ desired) &^ perms.Bot)
	if len(bits) == 0 {
		return "cannot change @everyone permissions (HTTP 403): every differing permission is one the bot holds, so check the bot's role in Server Settings → Roles"
	}
	return "cannot change @everyone permissions: " + how + "; the permissions to change are: " + strings.Join(bits, ", ")
}

// isForbidden reports whether err is a Discord REST error with HTTP status 403.
func isForbidden(err error) bool {
	var re *discordgo.RESTError
	return errors.As(err, &re) && re.Response != nil && re.Response.StatusCode == http.StatusForbidden
}
