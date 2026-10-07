# Eleventh Thesis: Discord Server, Design Spec

*Drafted 6 October 2026. Status: **approved** 6 October 2026 (revision 4). Implementation plan: `docs/superpowers/plans/2026-10-06-discord-provisioner.md`. The corp's founding plan deferred Discord ("Discord later"); this spec ends that deferral.*

*Revision 3 (6 October 2026): the provisioner manages @everyone's server-wide permissions, because Discord's defaults (Send Messages and others) would otherwise get past every `read` and `none` access level (§1.1, §2.2, §2.3). The bot's permission set grows from 14 to 16 (§2.6). The chunker keeps headings of every level attached to the text that follows, and wraps tables in code blocks because Discord doesn't render Markdown tables (§2.4). New launch step for system messages (§4).*

*Revision 4 (6 October 2026): the launch checklist gains an @everyone step, a bot-naming note and an unmanaged-objects note after the final review (§4).*

## 0. Intent

| | |
|---|---|
| **Outcome** | One Discord server that carries the whole path from first contact to membership. Visitors get a public front door, then a probation tier, a member tier and a director tier, each unlocked by a role. |
| **For whom** | Recruits arriving from the forum, r/evejobs and Local; probationers; members; directors. |
| **Success** | (1) The server's structure is fully described by a public repository, and changes reach the server only through that repository. (2) Permissions mirror the constitution (probation limits, Art. II.2 and X.1; open books, Art. III.4). (3) No text on the server claims anything the ledger can't show. |
| **Decided** | Layered server; Go provisioner in a new repo; roles assigned by hand in v1; declarative plan/apply; GitHub Actions in v1; `#amendments` is for discussion only. |
| **Out of scope (v1)** | EVE SSO verification and automatic role sync; posting ledger events from a bot; Discord Community and Onboarding features; Kubernetes deployment. All of these belong to v2. |

---

## 1. Server structure

### 1.1 Roles (hierarchy, highest first)

| Role | Colour | Hoisted | Guild permissions | Who |
|---|---|---|---|---|
| `Provisioner` (bot) | none | no | see §2.6 | The bot user. Discord creates this role when the bot is invited; it must stay at the top. |
| `Director` | `#B22222` | yes | Manage Roles, Manage Messages, Kick Members, Ban Members, Moderate Members | CEO and directors |
| `Member` | `#C0C0C0` | yes | none beyond @everyone's | Members who have finished probation |
| `Probation` | `#808080` | yes | none beyond @everyone's | Members on their 14-day probation (Art. II.2) |
| `@everyone` | n/a | n/a | Change Nickname, Use Voice Activity | Everyone, including visitors |

- **@everyone's server-wide permissions are managed too.** Discord gives a new server's @everyone Send Messages, Create Threads, Mention @everyone, Create Invite and more. A channel role setting only adds or removes the permissions it names, so those defaults would get past every `read` and `none` level: visitors could post in `#welcome`, and probationers in `#announcements`. The provisioner therefore reduces @everyone to the two permissions above, and everything else comes from the channel access levels (§1.2). Change Nickname is needed because `#apply` asks recruits to set their nickname; Use Voice Activity keeps `Comms` from being push-to-talk only.
- Directors do **not** get Administrator or Manage Channels. Channels change only through the repo; Manage Roles is there only so directors can assign `Probation` and `Member` by hand.
- One Discord account per person. Alts don't get separate accounts; the server nickname is the person's main character's name.

### 1.2 Categories and channels

Access levels: `none` (explicit deny of View Channel), `read` (view and read history), `post` (read plus send messages, add reactions, attach files, embed links), `voice` (view, connect, speak). A channel inherits its category's access. Where a channel sets an access level for a role, that level **replaces** the category's level for that role; other roles keep the category's level. Because @everyone's server-wide permissions are reduced to Change Nickname and Use Voice Activity (§1.1), a role's access level is the whole of what it can do in a channel.

| Category / channel | @everyone | Probation | Member | Director | Notes |
|---|---|---|---|---|---|
| **Front Door** | read | read | read | read | |
| `#welcome` | read | read | read | read | content: `welcome.md` |
| `#constitution` | read | read | read | read | content: `constitution.md` |
| `#open-ledger` | read | read | read | read | content: `open-ledger.md` |
| `#apply` | post | post | post | post | content at the top: `apply.md` (must fit one message, §2.2) |
| `#public-chat` | post | post | post | post | |
| **The Collective** | none | post | post | post | |
| `#announcements` | none | read | read | post | Decisions and their reasons (Art. XII.2) |
| `#general` | none | post | post | post | |
| `#contributions` | none | post | post | post | Deliveries, consignment queue, ledger queries |
| `#production` | none | post | post | post | |
| `#pi-and-mining` | none | post | post | post | |
| `#fits-and-skills` | none | post | post | post | |
| `#amendments` | none | post | post | post | Discussion only, see §1.3 |
| 🔊 `Comms` (voice) | none | voice | voice | voice | |
| **Operations** | none | none | post | post | |
| `#logistics` | none | none | post | post | Hauling and movements of corp stock. The Discord counterpart of the probation hangar restriction (Art. X.1). |
| **Directorate** | none | none | none | post | |
| `#vetting` | none | none | none | post | Personal data and security only |
| `#directors` | none | none | none | post | |

Content channels (`#welcome`, `#constitution`, `#open-ledger`) contain only the bot's messages. In `#apply`, the bot's message comes first and visitors post below it.

### 1.3 Rules written into the structure

1. **Private spaces don't govern.** `#vetting` exists only for applicants' personal data and security findings. Every decision (accepting a recruit, granting a role, an amendment ruling) is announced in `#announcements` with its reasons. Data stays private; decisions are public.
2. **`#amendments` has no formal standing.** Art. XII.1 recognises proposals made "in the corporation channel or by in-game mail". Discussion happens in Discord; formal proposals still go in game, and the channel's topic says so.

---

## 2. The provisioner (`eleve-discord`)

### 2.1 Repository

```
server.yaml                     desired state
content/{welcome,apply,constitution,open-ledger}.md
cmd/eleve-discord/main.go       CLI
internal/spec/                  load + validate server.yaml
internal/content/               Markdown → message chunks
internal/reconcile/             pure: Reconcile(desired, observed) → []Action
internal/discord/               Client interface; discordgo implementation; in-memory fake
.github/workflows/discord.yaml
```

Language: Go (current stable). Discord library: `github.com/bwmarrin/discordgo`.

### 2.2 Schema (`server.yaml`)

```yaml
guild_id: "<snowflake>"           # not secret
provisioner_role: Provisioner     # the bot's managed role; never edited, must stay highest
everyone_permissions: [change_nickname, use_voice_activity]   # @everyone's server-wide permissions, set exactly

access_levels:                    # named bundles → Discord permission flags
  none:  {deny:  [view_channel]}
  read:  {allow: [view_channel, read_message_history]}
  post:  {allow: [view_channel, read_message_history, send_messages, add_reactions, attach_files, embed_links]}
  voice: {allow: [view_channel, connect, speak]}

roles:                            # order = hierarchy, highest first, all below provisioner_role
  - {name: Director,  color: "#B22222", hoist: true, mentionable: true,
     permissions: [manage_roles, manage_messages, kick_members, ban_members, moderate_members]}
  - {name: Member,    color: "#C0C0C0", hoist: true}
  - {name: Probation, color: "#808080", hoist: true}

categories:                       # order = position
  - name: Front Door
    access: {"@everyone": read}
    channels:
      - {name: welcome,       content: content/welcome.md}
      - {name: constitution,  content: content/constitution.md}
      - {name: open-ledger,   content: content/open-ledger.md}
      - {name: apply,         content: content/apply.md, access: {"@everyone": post}}
      - {name: public-chat,   access: {"@everyone": post}}
  - name: The Collective
    access: {"@everyone": none, Probation: post, Member: post, Director: post}
    channels:
      - {name: announcements, access: {Probation: read, Member: read}}
      - {name: general}
      - {name: contributions,  topic: "Deliveries, consignment queue and ledger questions."}
      - {name: production}
      - {name: pi-and-mining}
      - {name: fits-and-skills}
      - {name: amendments,     topic: "Discussion only. Formal proposals in game: corp channel or mail to the CEO (Art. XII.1)."}
      - {name: Comms, type: voice, access: {Probation: voice, Member: voice, Director: voice}}
  - name: Operations
    access: {"@everyone": none, Member: post, Director: post}
    channels:
      - {name: logistics}
  - name: Directorate
    access: {"@everyone": none, Director: post}
    channels:
      - {name: vetting,   topic: "Applicant data and security only. Decisions go to #announcements."}
      - {name: directors}
```

Fields: channel `type` is `text` (the default) or `voice`; `topic` and `content` are optional; `access` maps a role name (or `@everyone`) to an access level.

**Validation** (`eleve-discord validate`, offline):
- Names are unique per kind; text channel names match `^[a-z0-9-]{1,100}$`.
- Every role named in `access` is declared or is `@everyone`.
- Every access level is declared; permission names come from a fixed list in the code.
- `content` files exist; `content` and `topic` are allowed only on text channels.
- No role, access level or `everyone_permissions` entry grants a permission the bot doesn't hold (§2.6).
- **Single-message rule.** A content channel where anyone other than the bot can send messages (e.g. `#apply`) must chunk to exactly one message, so its content is only ever edited in place and stays above visitors' posts. "Can send" counts both channel access levels and `everyone_permissions` (when @everyone's level in that channel doesn't deny Send Messages).

### 2.3 Reconciliation

**Precheck.** Before planning, the tool checks that `provisioner_role` exists and sits above every role the spec manages. If not, it stops with an error telling you to drag the bot's role to the top, rather than failing partway through.

`Reconcile(desired, observed)` is a pure function, and its output is deterministic in order:

1. **@everyone:** set its server-wide permissions to exactly `everyone_permissions` where they differ. This comes first so that the defaults are closed off before any channel exists.
2. **Roles:** create missing ones; update colour, hoist, mentionable and permissions where they differ; set positions in spec order directly below `provisioner_role`. Every field is sent explicitly, zero values included, because Discord gives a role created without permissions a copy of @everyone's.
3. **Categories:** create, set positions, set permission overwrites.
4. **Channels:** create, or update parent, position, topic and permission overwrites. A channel is matched by (type, name), and moved if its parent differs. Positions are compared as each channel's index among its siblings in the order Discord displays them, which puts voice channels after text channels within a category.
5. **Content:** for each channel with `content`, compare the chunks (§2.4) with the bot's own messages in that channel, oldest first: edit where the text differs, post new chunks after them, delete extra bot messages. Messages from other users are never touched.
6. **Unmanaged objects:** roles, categories and channels in the guild but not in the spec are listed as `unmanaged`. They are deleted only with `--prune`, last and in reverse dependency order. `@everyone`, `provisioner_role` and other bots' roles are never pruned.

**Overwrites.** Every managed category and channel gets an extra overwrite for `provisioner_role`: allow view_channel, read_message_history, send_messages and manage_messages. Without it, an `@everyone: none` deny would also lock out the bot. Overwrites are compared as exact sets, so an overwrite added by hand (including one for a single member) is reported as drift and removed on `apply`.

**Matching is by name.** A rename in `server.yaml` shows in the plan as a create plus an unmanaged object, never as a silent change.

### 2.4 Content chunking

- Discord's message limit is 2,000 characters; the chunker's target is ≤1,900 characters (Unicode code points).
- Split order: at `#`/`##` headings, then at blank lines, then at line breaks. A single line over the limit is split hard. A heading of any level (`#` to `######`) never ends a chunk, including when a long block is split at line breaks.
- Fenced code blocks and tables are never split in the middle. A chunk is closed before a block that wouldn't fit.
- Discord doesn't render Markdown tables, so each table is wrapped in a ```` ``` ```` code block, which keeps its columns aligned. The source file is unchanged.
- Each chunk is trimmed of leading and trailing whitespace, which Discord would strip anyway; otherwise every run would see a difference.
- Output is deterministic, so an unchanged file produces no edits.

### 2.5 CLI

| Command | Effect | Exit code |
|---|---|---|
| `eleve-discord validate [-f server.yaml]` | Offline checks (§2.2) | 0 ok, 1 invalid |
| `eleve-discord plan [-f …] [--prune]` | Reads the guild and prints actions plus unmanaged objects (with `--prune`, also the deletions) | 0 no changes, 2 changes, 1 error |
| `eleve-discord apply [-f …] [--prune]` | Plans, then executes in order; stops at the first failed action and reports what was done | 0 ok, 1 error |
| `eleve-discord invite-url -client-id <id>` | Prints the OAuth2 URL that invites the bot with exactly the permissions in §2.6 | 0 |

Reads `DISCORD_BOT_TOKEN` from the environment and nowhere else, and never prints it. Rate limits are handled by discordgo's built-in limiter.

### 2.6 Bot permissions

Discord won't let a bot grant a permission it doesn't hold, so the bot's role carries the union of what it manages (16 permissions): Manage Roles, Manage Channels, Manage Messages, Kick Members, Ban Members, Moderate Members, View Channel, Read Message History, Send Messages, Add Reactions, Attach Files, Embed Links, Connect, Speak, Change Nickname, Use Voice Activity. No Administrator. `invite-url` computes this integer from the code; validation fails if `server.yaml` asks for anything outside it.

### 2.7 CI (`.github/workflows/discord.yaml`)

- **Every pull request (forks included):** `go test ./...`, `go vet`, `eleve-discord validate`.
- **Pull requests from branches of this repo:** `plan`, with the output in the job summary (forks have no secret and skip it).
- **Push to `main`:** `apply` (never `--prune`), in the GitHub environment `discord` that holds `DISCORD_BOT_TOKEN`. The concurrency group `discord-apply` serialises runs.
- Pruning is a deliberate local act: `eleve-discord apply --prune`.

### 2.8 Testing

- `internal/reconcile`: table-driven tests from (spec, observed) to actions: empty guild; converged; drifted @everyone permissions; drifted role permissions; a hand-added overwrite; a moved channel; a rename; unmanaged objects with and without prune; changed, longer and shorter content; the precheck.
- `internal/content`: boundaries; determinism; trimming; the constitution at its real length; no code block or table split; tables wrapped in code blocks; no heading of any level at the end of a chunk.
- `internal/spec`: one test per validation rule.
- `apply` against the in-memory fake: from an empty guild, apply then plan gives no actions; a failure partway through stops, and a re-run converges.
- One manual check against the real server before launch (§4).

---

## 3. Content

All texts follow one rule: **no claim the ledger can't show.**

### 3.1 `content/welcome.md`

```markdown
# Eleventh Thesis [ELEVE]
An industrial collective in **Nonni** (Caldari high-sec, 5 jumps from Jita).
The corp belongs to whoever does the work, and the books are public so you can check that it does.

## The deal
1. **Contribute.** Bring P1/P2 planetary goods, high-sec ore or minerals to Nonni VI.
2. **Earn shares.** 1 share per 10M ISK of value, at Jita buy on the day of delivery.
3. **Get paid when it sells.** The corp hauls and sells; you receive 90% of the net sale price.
4. **Share the surplus.** Each month: 40% reserve · 20% reinvestment · 40% dividends, paid only on shares earned by work. Founder capital earns nothing.

## Where we are, honestly
We are new. No dividend has been paid yet. Payment on delivery starts once the corp holds 500M ISK in liquidity, and the ledger shows how far off that is.

📖 Rules: #constitution · 📒 Books: https://eleve.spaceship-moon.com · ✋ Join: #apply
0% tax · Not war-eligible · New and returning pilots welcome
```

### 3.2 `content/apply.md`

```markdown
# How to join
1. **Apply in game:** search for *Eleventh Thesis* in the corporation finder, or mail **Kal'dor Aubaris** (CEO).
2. **Set your server nickname** to your main character's name so the directors can match you.
3. **Post below:** your character, what you do (PI, mining, hauling, missions) and your timezone.

When you join in game you get **Probation** here, and after 14 days, **Member** (Art. II.2).
You can contribute from day one. Your contributions are recorded at once and your shares are issued when probation ends.
Background checks stay private; decisions are announced in #announcements.
```

### 3.3 `content/constitution.md`

Already committed: a status line followed by the full constitution text (v1, amendments 1.1 and 1.2 proposed). When an amendment is decided, this file and the in-game corp bulletin are updated in the same change.

### 3.4 `content/open-ledger.md`

```markdown
# The open ledger
Every contribution, sale and payout is a public entry: **https://eleve.spaceship-moon.com**
Each entry is a Git commit in https://github.com/theding0x/eleventh-thesis-ledger, so its history can't be quietly rewritten.

**Reading it:** the liquidity figure counts the reserve, founder loans and undistributed surplus, each shown separately (Art. VII.6), so you can see how much of it is owed back to the founder.

If an entry looks wrong, say so in #contributions. That is what the ledger is for.
```

---

## 4. Launch checklist

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

---

## 5. v2 (not designed here)

- EVE SSO verification: link a Discord user to a character; sync `Probation`/`Member` from corp membership and join date.
- Ledger feed: post contributions, sales and payouts to `#open-ledger` when `ledger.json` changes.
- Hosting: a Kubernetes Deployment once something long-running exists.
- Possible constitutional amendment: Discord as a recognised venue for proposals (Art. XII.1).
