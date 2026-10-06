package discord

import (
	"sort"

	"github.com/theding0x/eleventh-thesis-discord/internal/model"
)

// The ordering rules below are shared by the Fake and the live client, so both
// show the provisioner the same "first object with a given name".

// idLess orders IDs of one kind numerically: a Discord snowflake, or a Fake's
// prefix and counter. Shorter IDs are smaller; equal lengths compare as text.
func idLess(a, b string) bool {
	if len(a) != len(b) {
		return len(a) < len(b)
	}
	return a < b
}

// displayLess reports whether channel a is shown before channel b among
// siblings: text-like channels before voice-like ones, then by Discord
// position, then by ID.
func displayLess(aVoice bool, aPos int, aID string, bVoice bool, bPos int, bID string) bool {
	if aVoice != bVoice {
		return !aVoice
	}
	if aPos != bPos {
		return aPos < bPos
	}
	return idLess(aID, bID)
}

// sortRoles sorts roles by (Position descending, ID ascending).
func sortRoles(roles []model.Role) {
	sort.SliceStable(roles, func(i, j int) bool {
		if roles[i].Position != roles[j].Position {
			return roles[i].Position > roles[j].Position
		}
		return idLess(roles[i].ID, roles[j].ID)
	})
}

// sortChannels sorts channels by (Parent, Position, ID ascending).
func sortChannels(chans []model.Channel) {
	sort.SliceStable(chans, func(i, j int) bool {
		if chans[i].Parent != chans[j].Parent {
			return chans[i].Parent < chans[j].Parent
		}
		if chans[i].Position != chans[j].Position {
			return chans[i].Position < chans[j].Position
		}
		return idLess(chans[i].ID, chans[j].ID)
	})
}
