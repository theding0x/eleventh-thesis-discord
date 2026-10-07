# eleventh-thesis-discord

`eleve-discord` is a Go tool that makes the Eleventh Thesis [ELEVE] Discord server match [`server.yaml`](server.yaml): roles, categories, channels, permissions, and the messages posted from [`content/`](content/). Design: [`docs/superpowers/specs/2026-10-06-discord-server-design.md`](docs/superpowers/specs/2026-10-06-discord-server-design.md). Implementation plan: [`docs/superpowers/plans/2026-10-06-discord-provisioner.md`](docs/superpowers/plans/2026-10-06-discord-provisioner.md).

## Commands

Build: `go build -o eleve-discord ./cmd/eleve-discord`

| Command | Effect | Exit code |
|---|---|---|
| `eleve-discord validate [-f server.yaml]` | Offline checks | 0 ok, 1 invalid |
| `eleve-discord plan [-f …] [--prune]` | Reads the guild and prints actions plus unmanaged objects (with `--prune`, also the deletions) | 0 no changes, 2 changes, 1 error |
| `eleve-discord apply [-f …] [--prune]` | Plans, then executes in order; stops at the first failed action and reports what was done | 0 ok, 1 error |
| `eleve-discord invite-url -client-id <id>` | Prints the OAuth2 URL that invites the bot with exactly the permissions in spec §2.6 | 0 ok, 1 error |

`plan` and `apply` read the bot token from the environment variable `DISCORD_BOT_TOKEN` and nowhere else. It is never a flag and is never printed.

## Changing the server

Edit `server.yaml` or a file in `content/`, open a pull request, read the plan in the job summary, and merge to apply. `content/constitution.md` and the in-game corp bulletin change together, in the same change.

## Launch checklist

1. Create the server with Aaron's own Discord account, which stays its owner. Settings: verification level *Medium*; 2FA required for moderation.
2. Create the Discord application and bot (Developer Portal). **Name the application `Provisioner`:** Discord names a bot's managed role after the bot, and `server.yaml` expects that role to be called `Provisioner` (if you pick another name, set `provisioner_role` in `server.yaml` to the role's exact name). Put the token in the GitHub environment `discord` and in the local shell only.
3. Put the server's ID in `guild_id`; open the URL from `eleve-discord invite-url`; drag the bot's role to the top.
4. In Server Settings → Roles → @everyone, turn off every permission except Change Nickname and Use Voice Activity. Discord's defaults include permissions the bot does not hold (Create Invite, Mention @everyone, Use External Emojis, …), and a bot cannot change a permission it doesn't have. If `apply` stops at its first action with a 403 on @everyone, this step was skipped.
5. Run `plan` locally and review it, then `apply`, then `plan` again, which must report no changes. Objects Discord created by default (its `Text Channels` and `Voice Channels` categories and the voice channel `General`) are listed as `unmanaged:` — that is not a change (exit code 0); delete them by hand or with `apply --prune`.
6. System messages: Discord's default `#general` is matched by name and moved into The Collective, where visitors can't see it, but it still receives Discord's join notices. In Server Settings → Overview, set the System Messages channel to `#public-chat` or turn it off.
7. Manual check: as a visitor ("View Server As Role" or a second account), confirm only the Front Door is visible and that only `#apply` and `#public-chat` accept messages; repeat for Probation (can read but not post in `#announcements`) and Member.
8. Assign `Director` to Aaron's account.
9. Create a permanent invite to `#welcome`. (Create Invite is no longer an @everyone permission, so only the owner and the bot can make invites.)
10. Update the corp's existing texts (forum post, welcome mail, public channel MOTD) to mention Discord. Hand the invite out from the in-game public channel until the server has people to answer recruits.
11. Log the launch in the corp's founding plan.

## Pruning is manual

Deleting roles or channels is a deliberate local act: run `eleve-discord apply --prune` from your own shell. CI never prunes.

## Known limits (v1)

- Roles are assigned by hand.
- Unmanaged objects are reported, never deleted without `--prune`.
- Matching is by name, so a rename shows as a create plus an unmanaged object.
- Channel order is checked only among managed channels. An unmanaged channel inside a managed category can sit anywhere among them.
- With unmanaged channels inside a managed category, `apply` may need more than one run to restore the order; `plan` always shows what is left.
- EVE SSO verification, the ledger feed and Discord Community/Onboarding are v2 (spec §5).
