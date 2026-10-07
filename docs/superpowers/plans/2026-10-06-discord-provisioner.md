# Eleventh Thesis Discord Provisioner Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build `eleve-discord`, a Go CLI that makes the Eleventh Thesis Discord server match `server.yaml` (`validate` / `plan` / `apply` / `invite-url`), together with the repo's content files and CI.

**Architecture:** `spec.Load` turns YAML into a validated `Spec`. `reconcile.FromSpec` turns that into a `Desired` state. `discord.Client.Observe` reads the live guild into a `model.Guild`. `reconcile.Reconcile(Desired, Guild, prune)` is a pure function that returns a `Plan`, and `apply.Execute` runs the plan against a `Client`. The `Client` has a live discordgo implementation and an in-memory `Fake`. Everything except the live client's network calls is unit-tested.

**Tech Stack:** Go (the latest stable release, whatever `go mod init` writes), `github.com/bwmarrin/discordgo`, `gopkg.in/yaml.v3`, the standard library's `flag` and `testing`, GitHub Actions.

**Spec:** `docs/superpowers/specs/2026-10-06-discord-server-design.md` (already committed, revision 3). Section references (§) below point to it.

**Revision 3 changes to this plan:** @everyone's server-wide permissions are managed (Tasks 1, 2, 4, 5, 7, 8, 10) and the bot set grows to 16; roles are always created and updated with every field sent explicitly (Task 8); positions put voice channels after text ones and are written in bulk (Task 8); headings of every level stay attached to the text that follows, and tables are wrapped in code blocks (Task 3); CI runs a built binary, not `go run` (Task 11); there is a new launch step for system messages (README, Task 10).

## Global Constraints

- Module path `github.com/theding0x/eleventh-thesis-discord`; binary `eleve-discord`; repo `theding0x/eleventh-thesis-discord` (public).
- The token is read only from the env var `DISCORD_BOT_TOKEN`. It is never accepted as a flag, never written to a file and never printed.
- Message chunk limit: **1,900** Unicode code points (`content.Limit`).
- No Administrator permission anywhere. The bot permission set is exactly these 16 (§2.6): view_channel, read_message_history, send_messages, add_reactions, attach_files, embed_links, connect, speak, manage_roles, manage_channels, manage_messages, kick_members, ban_members, moderate_members, change_nickname, use_voice_activity.
- @everyone's server-wide permissions are set to exactly `everyone_permissions` (§1.1). Channel access comes only from the access levels.
- `apply` deletes roles or channels only with `--prune`; CI never passes `--prune`.
- Exit codes: `validate` 0 ok / 1 invalid. `plan` 0 no changes / 2 changes / 1 error. `apply` 0 / 1. `invite-url` 0 / 1.
- `reconcile` is pure: no I/O and no clock, and output order is deterministic.
- Names match exactly and case-sensitively. A rename is a create plus an unmanaged object.
- Line endings are LF (`.gitattributes` is committed); the chunker must not see CRLF.

## Review Focus

1. **A visitor posts in `#apply` and `apply.md` later grows.** A new chunk would land below the visitor's post. Expected: impossible by construction, because a content channel others can post in must fit one message (§2.2), so it is only ever edited. Pinned in Task 4 (`TestFromSpecOpenContentChannelMustBeOneMessage`).
2. **The bot's role isn't at the top**, for example on the first run before it has been dragged there. Expected: `plan`/`apply` stop before any change with a message telling you to move the role, instead of a 403 partway through. Pinned in Task 5 (`TestCheck*`) and Task 9 (`TestPlanFailsPrecheck`).
3. **Discord strips trailing whitespace from messages.** Expected: an unchanged file never causes an edit. Pinned in Task 3 (`TestChunkTrimsWhitespace`) and Task 7 (the convergence test).
4. **A director gives one member access to a channel by hand.** Expected: `plan` reports drift on that channel, and `apply` removes the overwrite. Pinned in Task 5 (`member overwrite is drift` case).
5. **The token is missing or wrong, or Discord is down.** Expected: exit 1 with a clear message, no panic, and the token never appears in output. Pinned in Task 9 (`TestMissingToken`, `TestTokenNeverPrinted`).
6. **Discord's default @everyone permissions get past a `read` level.** A new server gives @everyone Send Messages, so without intervention visitors could post in `#welcome` and probationers in `#announcements`. Expected: `apply` reduces @everyone to `everyone_permissions` before anything else, and the single-message rule counts @everyone's server-wide Send Messages. Pinned in Task 4 (`TestFromSpecEveryoneBaseCountsAsOpen`), Task 5 (`everyone drift` case) and Task 7 (the convergence test starts from Discord-like defaults).

---

## File structure

| Path | Responsibility |
|---|---|
| `internal/perms/perms.go` | Permission names ↔ Discord bit flags; the bot permission set |
| `internal/spec/spec.go` | YAML types, `Load`, `Validate` |
| `internal/content/chunk.go` | Markdown → message chunks |
| `internal/model/model.go` | Name-based guild state shared by the desired and observed sides |
| `internal/reconcile/desired.go` | `Desired`, `FromSpec` |
| `internal/reconcile/reconcile.go` | `Action`, `Plan`, `Check`, `Reconcile` |
| `internal/discord/client.go` | `Client` interface |
| `internal/discord/fake.go` | In-memory `Fake` |
| `internal/discord/live.go` | discordgo implementation |
| `internal/apply/apply.go` | `Execute` |
| `cmd/eleve-discord/main.go` | CLI (`run` + `main`) |
| `server.yaml`, `content/*.md`, `README.md` | Repo content |
| `.github/workflows/discord.yaml` | CI |

---

### Task 1: Module scaffold and permission vocabulary

**Files:**
- Create: `go.mod`, `internal/perms/perms.go`
- Test: `internal/perms/perms_test.go`

**Interfaces:**
- Produces:
  - `func perms.Parse(names []string) (int64, error)`: ORs the bit flags; an unknown name is an error naming it
  - `func perms.Names() []string`: the 16 names, sorted
  - `var perms.Bot int64`: the union of all 16

- [ ] **Step 1:** `go mod init github.com/theding0x/eleventh-thesis-discord`; `go get github.com/bwmarrin/discordgo gopkg.in/yaml.v3`.
- [ ] **Step 2: Write the failing tests**

```go
func TestParse(t *testing.T) {
	got, err := perms.Parse([]string{"view_channel", "send_messages"})
	if err != nil { t.Fatal(err) }
	if want := int64(discordgo.PermissionViewChannel | discordgo.PermissionSendMessages); got != want {
		t.Fatalf("got %d want %d", got, want)
	}
}
func TestParseUnknown(t *testing.T) {
	_, err := perms.Parse([]string{"administrator"})
	if err == nil || !strings.Contains(err.Error(), "administrator") { t.Fatalf("err = %v", err) }
}
func TestBotIsUnionOfAllNames(t *testing.T) {
	if n := len(perms.Names()); n != 16 { t.Fatalf("names = %d", n) }
	all, _ := perms.Parse(perms.Names())
	if all != perms.Bot { t.Fatal("Bot != union of Names") }
}
func TestBotExcludesAdministrator(t *testing.T) {
	if perms.Bot&discordgo.PermissionAdministrator != 0 { t.Fatal("Bot includes Administrator") }
}
```

- [ ] **Step 3:** Run `go test ./internal/perms/`. Expected: FAIL (package doesn't exist).
- [ ] **Step 4:** Implement `perms.go`: a `map[string]int64` built from discordgo constants (`connect` → `PermissionVoiceConnect`, `speak` → `PermissionVoiceSpeak`, `use_voice_activity` → `PermissionVoiceUseVAD`, `change_nickname` → `PermissionChangeNickname`, `moderate_members` → `PermissionModerateMembers`; the rest by the obvious name).
- [ ] **Step 5:** Run `go test ./internal/perms/`. Expected: PASS.
- [ ] **Step 6:** Commit `feat: permission vocabulary and module scaffold`.

---

### Task 2: Spec loading and validation

**Files:**
- Create: `internal/spec/spec.go`; `internal/spec/testdata/valid.yaml` (the §2.2 example, including `everyone_permissions`, with `guild_id: "1"` and every `content:` pointing to `testdata/content/*.md`); `internal/spec/testdata/content/{welcome,apply,constitution,open-ledger}.md`. All are short placeholder text except `constitution.md`, which is three paragraphs of about 1,500 characters each so that it chunks to at least 2 messages for the Task 6 tests.
- Test: `internal/spec/spec_test.go`

**Interfaces:**
- Consumes: `perms.Parse`
- Produces:

```go
type Spec struct {
	GuildID             string                 `yaml:"guild_id"`
	ProvisionerRole     string                 `yaml:"provisioner_role"`
	EveryonePermissions []string               `yaml:"everyone_permissions"` // @everyone's server-wide permissions, set exactly
	AccessLevels        map[string]AccessLevel `yaml:"access_levels"`
	Roles           []Role                 `yaml:"roles"`
	Categories      []Category             `yaml:"categories"`
}
type AccessLevel struct{ Allow, Deny []string }            // yaml: allow, deny
type Role struct {
	Name, Color        string
	Hoist, Mentionable bool
	Permissions        []string
}
type Category struct {
	Name     string
	Access   map[string]string // role name or "@everyone" → access level name
	Channels []Channel
}
type Channel struct {
	Name, Type, Topic, Content string // Type: "", "text" or "voice"; "" means text
	Access                     map[string]string
}
func Load(path string) (*Spec, error)             // decode with KnownFields(true), then Validate(s, filepath.Dir(path))
func Validate(s *Spec, baseDir string) error      // errors.Join of every problem found
```

- [ ] **Step 1: Write the failing tests.** Add `TestLoadValid` (loads `testdata/valid.yaml`, no error, 3 roles, 4 categories). Add a table test `TestValidate` that loads `valid.yaml`, applies one mutation per case, and asserts that `Validate` returns an error containing the substring:

| Case | Mutation | Error contains |
|---|---|---|
| missing guild | `GuildID = ""` | `guild_id` |
| non-numeric guild | `GuildID = "abc"` | `guild_id` |
| missing provisioner | `ProvisionerRole = ""` | `provisioner_role` |
| provisioner declared as role | add role named `Provisioner` | `provisioner_role` |
| duplicate role | duplicate `Member` | `duplicate role "Member"` |
| role named @everyone | rename a role to `@everyone` | `@everyone` |
| bad colour | `Color = "red"` | `color` |
| unknown permission | role permission `administrator` | `administrator` |
| unknown permission in level def | `AccessLevels["x"] = {Allow: [fly]}` | `fly` |
| unknown @everyone permission | `EveryonePermissions = [mention_everyone]` | `mention_everyone` |
| undeclared role in access | category access `Pilot: post` | `"Pilot"` |
| undeclared level | category access `Member: shout` | `"shout"` |
| duplicate category | duplicate `Operations` | `duplicate category` |
| duplicate channel | second text `general` in another category | `duplicate text channel "general"` |
| bad text name | channel `General Chat` | `General Chat` |
| bad type | `Type = "stage"` | `stage` |
| content on voice | voice channel with `Content` | `content` |
| topic on voice | voice channel with `Topic` | `topic` |
| missing content file | `Content = "content/nope.md"` | `nope.md` |
| topic too long | 1,025-character topic | `topic` |
| several problems | two mutations | both substrings (errors are joined) |

- [ ] **Step 2:** Run `go test ./internal/spec/`. Expected: FAIL.
- [ ] **Step 3:** Implement `Load` and `Validate`. Rules: guild_id `^[0-9]+$`; colour `^#[0-9A-Fa-f]{6}$`; text names `^[a-z0-9-]{1,100}$`; voice and category names 1–100 characters; topic ≤ 1,024 characters; content paths resolved against `baseDir`. An empty or absent `everyone_permissions` is valid and means @everyone gets no server-wide permissions. The bot-permission subset rule (§2.2) holds automatically, because `perms.Parse` only knows the bot's 16 names. Say so in a comment.
- [ ] **Step 4:** Run `go test ./internal/spec/`. Expected: PASS.
- [ ] **Step 5:** Commit `feat: spec loading and validation`.

---

### Task 3: Content chunker

**Files:**
- Create: `internal/content/chunk.go`
- Test: `internal/content/chunk_test.go`

**Interfaces:**
- Produces: `const content.Limit = 1900`; `func content.Chunk(md string, limit int) []string`

Algorithm (§2.4):
1. Split the input into blocks. A fenced block (from a ```` ``` ```` line to the next ```` ``` ```` line) is one block, and so is a run of consecutive lines starting with `|` (a table). Discord doesn't render Markdown tables, so a table block is wrapped in fences (```` "```\n" + table + "\n```" ````) at this step and is a fenced block from then on; its length includes the fences. Everything else splits into paragraphs at blank lines.
2. A heading paragraph (a line matching `^#{1,6} `) is glued to the block that follows it.
3. Pack greedily: join blocks with `"\n\n"`, and start a new chunk when the next block would push the current one past `limit`.
4. A block that is itself over `limit` is split at line breaks. A fenced block split this way gets each piece re-wrapped in fences. A single line over `limit` is split hard at `limit` code points. A piece never ends on a heading line: if it would, the heading moves to the start of the next piece.
5. `strings.TrimSpace` every chunk and drop empty ones. Length is counted with `utf8.RuneCountInString`.

- [ ] **Step 1: Write the failing tests**
  - `TestChunkShortReturnsOne`: `Chunk("# A\n\nbody", 1900)` → `[]string{"# A\n\nbody"}`
  - `TestChunkTrimsWhitespace`: `Chunk("\n\n  text  \n\n", 1900)` → `[]string{"text"}`
  - `TestChunkRespectsLimit`: 50 paragraphs of 100 `a`s; every chunk ≤ 1,900 code points; `strings.Join(chunks, "\n\n")` equals the paragraphs joined by `"\n\n"`
  - `TestChunkHeadingNotOrphaned`: input where a `## H` falls right at a boundary; no chunk's last line starts with `#`
  - `TestChunkThirdLevelHeadingGlued`: the same with `### H`; no chunk's last line starts with `#`
  - `TestChunkLongBlockHeadingNotLast`: a single 3,000-character block with no blank lines, made of 40-character lines, with a `### H` line placed so that it would be the last line of the first piece; no chunk's last line starts with `#`, and every chunk is ≤ 1,900
  - `TestChunkWrapsTableInFence`: `Chunk("text\n\n| a | b |\n|---|---|\n| 1 | 2 |", 1900)` → `[]string{"text\n\n```\n| a | b |\n|---|---|\n| 1 | 2 |\n```"}`
  - `TestChunkKeepsTableWhole`: 1,800 characters of paragraphs followed by a 10-row table; the fenced table appears intact in exactly one chunk
  - `TestChunkKeepsFenceWhole`: the same with a fenced block
  - `TestChunkHardSplitsLongLine`: one 5,000-character line → 3 chunks, each ≤ 1,900
  - `TestChunkCountsRunes`: `strings.Repeat("é", 1900)` → 1 chunk
  - `TestChunkDeterministic`: two calls on the same input give `reflect.DeepEqual` results
  - `TestChunkRealConstitution`: `content/constitution.md` from the repo root → ≥ 2 chunks, each ≤ 1,900, none ending with a heading line; every line starting with `|` is inside a fence (Appendices A and B)
- [ ] **Step 2:** Run `go test ./internal/content/`. Expected: FAIL.
- [ ] **Step 3:** Implement `Chunk`.
- [ ] **Step 4:** Run `go test ./internal/content/`. Expected: PASS.
- [ ] **Step 5:** Commit `feat: markdown chunker`.

---

### Task 4: Model and desired state

**Files:**
- Create: `internal/model/model.go`, `internal/reconcile/desired.go`
- Test: `internal/reconcile/desired_test.go` (uses `../spec/testdata/valid.yaml`)

**Interfaces:**
- Consumes: `spec.Spec`, `perms.Parse`, `content.Chunk`, `content.Limit`
- Produces:

```go
package model
type ChannelType int
const ( Text ChannelType = iota; Voice; Category )
type Role struct {
	ID          string // empty on the desired side
	Name        string
	Color       int
	Hoist       bool
	Mentionable bool
	Permissions int64
	Position    int  // observed: Discord position (higher = higher in the hierarchy); desired: unused
	Managed     bool // integration/bot role (observed only)
}
type Overwrite struct {
	Target      string // role name, "@everyone", or "member:<user id>"
	Allow, Deny int64
}
type Channel struct {
	ID         string
	Name       string
	Type       ChannelType
	Parent     string      // category name; "" for categories
	Position   int         // 0-based index among siblings (same Parent; categories among categories)
	Topic      string
	Overwrites []Overwrite // sorted by Target
}
type Message struct{ ID, Content string }
type Guild struct {
	EveryonePermissions int64        // @everyone's server-wide permissions
	Roles       []Role               // excludes @everyone
	Channels    []Channel            // categories and channels; other types are omitted
	BotMessages map[string][]Message // text channel name → the bot's own messages, oldest first
}
```

```go
package reconcile
type Desired struct {
	ProvisionerRole     string
	EveryonePermissions int64             // perms.Parse(spec.EveryonePermissions)
	Roles           []model.Role          // hierarchy order, highest first
	Channels        []model.Channel       // categories in order, then each category's channels in order
	Content         map[string][]string   // text channel name → chunks
}
func FromSpec(s *spec.Spec, baseDir string, readFile func(string) ([]byte, error)) (Desired, error)
```

Rules:
- Overwrites for a category come from its `access`. For a channel, the category's `access` is copied and then each role named in the channel's own `access` is **replaced** (§1.2).
- Access level → `Overwrite{Allow: Parse(level.Allow), Deny: Parse(level.Deny)}`.
- Every category and channel also gets `Overwrite{Target: ProvisionerRole, Allow: view_channel|read_message_history|send_messages|manage_messages}`.
- Sort overwrites by `Target`.
- The colour `"#B22222"` becomes `0xB22222`.
- **Single-message rule:** a text channel with content is *open* if any overwrite other than the provisioner's allows `send_messages`, or if `EveryonePermissions` includes `send_messages` and the channel's `@everyone` overwrite doesn't deny it. If an open channel has `len(chunks) != 1`, return the error `channel "apply": content must fit in one message because others can post there`.

- [ ] **Step 1: Write the failing tests**
  - `TestFromSpecCategoryOverwrites`: The Collective's overwrites equal exactly the following, sorted: `@everyone` {Deny: view}, `Director`/`Member`/`Probation` {Allow: post bits}, and `Provisioner` {Allow: view|history|send|manage_messages}
  - `TestFromSpecChannelReplacesCategoryLevel`: `#announcements` Probation Allow == view|history (no send_messages); Director keeps the post bits
  - `TestFromSpecVoice`: Comms Probation Allow == view|connect|speak
  - `TestFromSpecPositions`: the 4 categories get positions 0–3; `#public-chat` has Parent `Front Door` and Position 4
  - `TestFromSpecRoleColor`: Director Color == 0xB22222
  - `TestFromSpecContentChunks`: `Content["welcome"]` == `content.Chunk(<file>, content.Limit)`; `len(Content["constitution"]) >= 2`
  - `TestFromSpecOpenContentChannelMustBeOneMessage`: `apply.md` replaced by 3,000 characters of paragraphs → error containing `one message`
  - `TestFromSpecEveryonePermissions`: `EveryonePermissions == change_nickname|use_voice_activity`
  - `TestFromSpecEveryoneBaseCountsAsOpen`: `EveryonePermissions` set to `[send_messages]` and `welcome.md` replaced by 3,000 characters of paragraphs → error containing `one message` (Front Door's `read` level doesn't deny sending)
- [ ] **Step 2:** Run `go test ./internal/reconcile/`. Expected: FAIL.
- [ ] **Step 3:** Implement the model types and `FromSpec`.
- [ ] **Step 4:** Run `go test ./internal/reconcile/`. Expected: PASS.
- [ ] **Step 5:** Commit `feat: desired state from spec`.

---

### Task 5: Reconcile roles, channels, unmanaged objects and the precheck

**Files:**
- Create: `internal/reconcile/reconcile.go`
- Test: `internal/reconcile/reconcile_test.go`

**Interfaces:**
- Consumes: `Desired`, `model.*`
- Produces:

```go
type Kind string
const (
	UpdateEveryone Kind = "update-everyone"
	CreateRole = "create-role"; UpdateRole = "update-role"; ReorderRoles = "reorder-roles"; DeleteRole = "delete-role"
	CreateChannel = "create-channel"; UpdateChannel = "update-channel"; DeleteChannel = "delete-channel"
	PostMessage = "post-message"; EditMessage = "edit-message"; DeleteMessage = "delete-message"
)
type Action struct {
	Kind      Kind
	Permissions int64        // UpdateEveryone: desired @everyone permissions
	Role      model.Role     // Create/UpdateRole: desired values; DeleteRole: Name
	RoleOrder []string       // ReorderRoles: managed role names, highest first
	Channel   model.Channel  // Create/Update/DeleteChannel: desired values (Delete: Name, Type)
	Message   model.Message  // Edit/DeleteMessage: ID; Post/Edit: Content
	Target    string         // message actions: channel name
	Index     int            // message actions: 1-based chunk number
	Total     int            // message actions: total chunks
	Changes   []string       // Update*: changed fields, e.g. "permissions", "overwrites"
}
func (a Action) String() string
type Plan struct {
	Actions   []Action
	Unmanaged []string // "role X", "category X", "text #x", "voice X"
}
func (p Plan) String() string
func Check(g model.Guild, d Desired) error
func Reconcile(d Desired, g model.Guild, prune bool) Plan
```

`Action.String` formats (the tests assert these exactly): `~ update @everyone permissions`, `+ create role Director`, `~ update role Member (permissions)`, `~ reorder roles: Director, Member, Probation`, `+ create category The Collective`, `+ create text #general in The Collective`, `+ create voice Comms in The Collective`, `~ update text #apply (overwrites)`, `- delete text #old`, `- delete role Old`, `+ post message 3/7 in #constitution`, `~ edit message 2/7 in #constitution`, `- delete message in #constitution`.
`Plan.String`: one action per line, then `unmanaged:` with one indented line per object (omitted if empty), or `no changes` when both are empty.

Rules:
- **Check** errors if no observed role is named `d.ProvisionerRole`, or if any observed role named in `d.Roles` has `Position >= provisioner.Position`. The error text contains `drag the bot's role above`.
- **@everyone:** if `g.EveryonePermissions != d.EveryonePermissions`, the first action is `UpdateEveryone` with `Permissions: d.EveryonePermissions` (§2.3 step 1).
- **Roles:** create those missing, in spec order. Update where Color, Hoist, Mentionable or Permissions differ, with `Changes` listing the field names. Emit `ReorderRoles` (with spec order) when any role was created, or when, among observed roles sorted by Position descending, the roles after the provisioner don't start with exactly the spec's names in order.
- **Channels:** matched by (Type, Name). Create those missing. Update where Parent, Position, Topic or Overwrites (compared exactly, after sorting) differ. Categories come before channels.
- **Unmanaged:** observed roles not in spec, excluding the provisioner and `Managed` roles; observed channels not in spec.
- **Prune** appends deletes: non-category channels, then categories, then roles.

- [ ] **Step 1: Write the failing tests.** Add a helper `converged(d Desired) model.Guild` that builds the observed state an applied guild would have: `EveryonePermissions` equal to `d.EveryonePermissions`, a provisioner role at Position 10, the managed roles at 9, 8, 7, the channels as desired, and `BotMessages` equal to the chunks. Then add a table test `TestReconcile` with these cases:

| Case | Observed | Prune | Expect |
|---|---|---|---|
| empty guild | provisioner role only; `EveryonePermissions` = view_channel\|send_messages\|read_message_history (Discord-like defaults) | no | first 5 actions: update @everyone, create Director, Member, Probation, reorder; then 4 create-category, then 16 create-channel (message actions follow from Task 6) |
| converged | `converged(d)` | no | `Actions` empty, `Unmanaged` empty, `String()` == `no changes` |
| everyone drift | `converged(d)` with `send_messages` added to `EveryonePermissions` | no | exactly one action, `~ update @everyone permissions`, with `Permissions == d.EveryonePermissions` |
| drifted role permissions | Member gets `manage_messages` | no | one `update-role` Member, `Changes == ["permissions"]` |
| member overwrite is drift | `#general` gets an extra `member:42` overwrite | no | one `update-channel` `#general`, `Changes == ["overwrites"]` |
| moved channel | `#logistics` Parent `The Collective` | no | one `update-channel` with `"parent"` in Changes |
| rename | observed `general-chat` instead of `general` | no | `create-channel general`; Unmanaged == `["text #general-chat"]`; no delete |
| prune | same as rename | yes | last action `- delete text #general-chat` |
| managed role ignored | extra role `Mee6`, `Managed: true` | yes | no action and not in Unmanaged |
| unmanaged role pruned | extra role `Old` | yes | last action `- delete role Old` |
| order drift | Member above Director | no | one `reorder-roles` |

Also add `TestCheckMissingProvisioner`, `TestCheckProvisionerBelowManaged` (Director at 11 > provisioner 10 → error containing `drag the bot's role above`) and `TestCheckOK`.
- [ ] **Step 2:** Run `go test ./internal/reconcile/`. Expected: FAIL.
- [ ] **Step 3:** Implement `Check`, `Reconcile` (roles, channels, unmanaged, prune) and the `String` methods. Leave a call to `reconcileContent` (Task 6) as a stub that returns nil.
- [ ] **Step 4:** Run `go test ./internal/reconcile/`. Expected: PASS.
- [ ] **Step 5:** Commit `feat: reconcile roles and channels`.

---

### Task 6: Reconcile content messages

**Files:**
- Modify: `internal/reconcile/reconcile.go` (`reconcileContent`)
- Test: `internal/reconcile/content_test.go`

**Interfaces:**
- Produces: `func reconcileContent(d Desired, g model.Guild) []Action`, appended after the channel actions and before the prune deletes.

Rule: for each channel in `d.Content`, in `d.Channels` order: observed = `g.BotMessages[name]` (nil if the channel is new). For i < min(len): `EditMessage` where the content differs. Extra desired chunks → `PostMessage` in order. Extra observed messages → `DeleteMessage`. `Index` is 1-based and `Total = len(chunks)`.

- [ ] **Step 1: Write the failing tests** (starting from `converged(d)` each time)
  - `TestContentConverged`: no message actions
  - `TestContentChanged`: chunk 2 of constitution differs → exactly one `~ edit message 2/N in #constitution`
  - `TestContentLonger`: observed is missing the last chunk → one `+ post message N/N in #constitution`
  - `TestContentShorter`: observed has an extra trailing message → one `- delete message in #constitution`
  - `TestContentNewChannel`: `#welcome` absent from the guild → a `create-channel` action, then `post-message` actions for each chunk, in order, after it
  - Extend the Task 5 "empty guild" case: after the 16 create-channel actions, one post-message per content chunk
- [ ] **Step 2:** Run `go test ./internal/reconcile/`. Expected: FAIL.
- [ ] **Step 3:** Implement `reconcileContent`.
- [ ] **Step 4:** Run `go test ./...`. Expected: PASS.
- [ ] **Step 5:** Commit `feat: reconcile content messages`.

---

### Task 7: Client interface, fake and executor

**Files:**
- Create: `internal/discord/client.go`, `internal/discord/fake.go`, `internal/apply/apply.go`
- Test: `internal/apply/apply_test.go`

**Interfaces:**
- Produces:

```go
package discord
type Client interface {
	Observe(ctx context.Context, contentChannels []string) (model.Guild, error)
	UpdateEveryone(ctx context.Context, permissions int64) error         // @everyone's server-wide permissions
	CreateRole(ctx context.Context, r model.Role) error
	UpdateRole(ctx context.Context, r model.Role) error                 // matched by Name
	ReorderRoles(ctx context.Context, namesHighestFirst []string) error  // directly below the provisioner role
	DeleteRole(ctx context.Context, name string) error
	CreateChannel(ctx context.Context, c model.Channel) error           // Parent resolved by name
	UpdateChannel(ctx context.Context, c model.Channel) error           // matched by (Type, Name); replaces overwrites
	DeleteChannel(ctx context.Context, t model.ChannelType, name string) error
	PostMessage(ctx context.Context, channel, content string) error
	EditMessage(ctx context.Context, channel, messageID, content string) error
	DeleteMessage(ctx context.Context, channel, messageID string) error
}
type Fake struct {
	Guild  model.Guild
	FailOn string   // method name; that method returns an error
	Calls  []string // method names, in order
}
const DefaultEveryone int64 = /* view_channel|send_messages|read_message_history|add_reactions|connect|speak, built from discordgo constants */
func NewFake(provisionerRole string) *Fake // guild containing only the provisioner role (Managed, Position 10), EveryonePermissions = DefaultEveryone
```

```go
package apply
func Execute(ctx context.Context, c discord.Client, actions []reconcile.Action, out io.Writer) error
```

Fake semantics: name-based mutations of `Guild`. `UpdateEveryone` sets `EveryonePermissions`. A created role gets Position 1, with every other role shifted up. `ReorderRoles` assigns provisioner−1, −2, … in order. A created channel is appended. A posted message gets ID `m<n>`. `Observe` returns a deep copy. `Execute` prints `done: <action>` after each success. On failure it returns `fmt.Errorf("action %d/%d (%s): %w", i+1, len, action, err)` and doesn't run the remaining actions.

- [ ] **Step 1: Write the failing tests**
  - `TestApplyFromEmptyConverges`: `FromSpec(valid.yaml)`; `f := NewFake("Provisioner")`; `Observe` → `Check` → `Reconcile` → `Execute`; then `Observe` → `Reconcile` again gives an empty `Plan`, and `f.Guild.EveryonePermissions == d.EveryonePermissions`
  - `TestApplyStopsAtFirstFailure`: `f.FailOn = "CreateChannel"` → error containing `action`; no `PostMessage` in `f.Calls`; clearing `FailOn` and re-running converges
  - `TestApplyReportsProgress`: `out` contains a `done: + create role Director` line
- [ ] **Step 2:** Run `go test ./internal/apply/`. Expected: FAIL.
- [ ] **Step 3:** Implement `client.go`, `fake.go` and `apply.go`.
- [ ] **Step 4:** Run `go test ./...`. Expected: PASS.
- [ ] **Step 5:** Commit `feat: client interface, fake and executor`.

---

### Task 8: Live discordgo client

**Files:**
- Create: `internal/discord/live.go`
- Test: `internal/discord/live_test.go` (pure converters only)

**Interfaces:**
- Consumes: `Client`, `model.*`
- Produces:
  - `func NewLive(token, guildID string) (*Live, error)`: `discordgo.New("Bot " + token)`; fetches `@me` for the bot's user ID; the error never includes the token
  - `func toModelChannel(c *discordgo.Channel, guildID string, roleNames, categoryNames map[string]string) (model.Channel, bool)`: `false` for types other than text, voice and category
  - `func normalizePositions(chs []model.Channel)`: rewrites `Position` as the 0-based sibling index in the order Discord displays them: text channels before voice channels within a parent, each group sorted by Discord position, then ID (categories among categories by Discord position, then ID). Needs each channel's raw Discord position and ID, which `toModelChannel` fills in before normalising.
  - `func rolesFromDiscord(roles []*discordgo.Role, guildID string) (managed []model.Role, everyone int64)`: the role whose ID equals the guild ID is @everyone; it's left out of `managed` and its permissions are returned separately. `Observe` uses this, so the mapping is testable without the network.
  - `func toRoleParams(r model.Role) *discordgo.RoleParams`: sets **every** field (Name, Color, Hoist, Mentionable, Permissions) as a non-nil pointer, zero values included. Discord gives a role created without explicit permissions a copy of @everyone's (§2.3 step 2), and a nil field on update leaves drift in place.

Implementation notes:
- `Observe`: `GuildRoles`, where the role whose ID equals the guild ID is `@everyone` (named in overwrites, excluded from `Roles`, and its `Permissions` go into `Guild.EveryonePermissions`).
- `UpdateEveryone`: `GuildRoleEdit(guildID, guildID, &discordgo.RoleParams{Permissions: &p})` (the @everyone role's ID is the guild ID).
- `CreateRole`/`UpdateRole`: always through `toRoleParams`. `GuildChannels` goes through `toModelChannel`, then `normalizePositions`. For each content channel, page through `ChannelMessages(id, 100, before, "", "")` until empty, keep messages whose author is the bot, then reverse to oldest first. It caches name → ID maps for writes.
- Overwrite targets: a role overwrite gets the role's name; a member overwrite gets `member:<id>`.
- `ReorderRoles`: `GuildRoleReorder`, giving the named roles positions provisioner−1, −2, ….
- `CreateChannel`/`UpdateChannel`: `GuildChannelCreateComplex` / `ChannelEditComplex` with `ParentID`, `Topic` (text only) and the full `PermissionOverwrites` set, which replaces whatever was there. Write `Position` and parent through the bulk endpoint `GuildChannelsReorder` (`[]*discordgo.Channel{{ID, Position, ParentID}}`) after the create or edit, not through `ChannelEditComplex`: the single-channel edit can renumber siblings, which would make positions flap between runs. The written Discord position is the desired sibling index. Whether this converges is confirmed against the real server in launch step 4.
- After each create, update the cached maps.

- [ ] **Step 1: Write the failing tests**
  - `TestToModelChannelEveryone`: a role overwrite whose ID equals the guild ID → Target `@everyone`
  - `TestToModelChannelMemberOverwrite`: a member overwrite with ID 42 → Target `member:42`
  - `TestToModelChannelIgnoresOtherTypes`: `ChannelTypeGuildNews` → `false`
  - `TestToModelChannelParentName`: ParentID mapped to the category name
  - `TestNormalizePositions`: Discord positions 5, 2, 9 among siblings → 1, 0, 2
  - `TestNormalizePositionsVoiceAfterText`: in one category, a voice channel at Discord position 0 and text channels at 1 and 2 → text 0, text 1, voice 2
  - `TestRolesFromDiscord`: given roles including one whose ID equals the guild ID, `rolesFromDiscord` returns the other roles and that role's permissions as the @everyone permissions
  - `TestToRoleParamsSendsZeroValues`: `toRoleParams(model.Role{Name: "Member"})` has non-nil `Permissions` (0), `Color` (0), `Hoist` (false) and `Mentionable` (false)
- [ ] **Step 2:** Run `go test ./internal/discord/`. Expected: FAIL.
- [ ] **Step 3:** Implement `live.go`.
- [ ] **Step 4:** Run `go test ./... && go vet ./...`. Expected: PASS, no vet findings. (Network behaviour is checked against the real server in launch step 4.)
- [ ] **Step 5:** Commit `feat: live discordgo client`.

---

### Task 9: CLI

**Files:**
- Create: `cmd/eleve-discord/main.go`
- Test: `cmd/eleve-discord/main_test.go` (uses `internal/spec/testdata/valid.yaml` and `discord.Fake`)

**Interfaces:**
- Consumes: `spec.Load`, `reconcile.FromSpec/Check/Reconcile`, `apply.Execute`, `discord.NewLive`, `perms.Bot`
- Produces:

```go
type dialer func(token, guildID string) (discord.Client, error)
func run(args []string, getenv func(string) string, stdout, stderr io.Writer, dial dialer) int
```

`main` calls `os.Exit(run(os.Args[1:], os.Getenv, os.Stdout, os.Stderr, live))`, where `live` wraps `discord.NewLive`.
- `validate [-f server.yaml]`: `Load`, then `FromSpec` (which enforces the single-message rule); prints `ok`.
- `plan [-f …] [--prune]`: validate; the token must be set (otherwise `DISCORD_BOT_TOKEN is not set`, exit 1, and `dial` isn't called); `dial`, `Observe(content channel names)`, `Check`, `Reconcile`; prints `Plan.String()`; exit 2 if there are actions, otherwise 0.
- `apply [-f …] [--prune]`: as `plan`, then `Execute`.
- `invite-url -client-id <id>`: prints `https://discord.com/oauth2/authorize?client_id=<id>&permissions=<perms.Bot>&scope=bot`; a missing `-client-id` is exit 1.
- An unknown or missing subcommand prints usage to stderr and exits 1.

- [ ] **Step 1: Write the failing tests**
  - `TestValidateOK` → 0, stdout `ok`; `TestValidateInvalid` (temporary YAML with a bad colour) → 1
  - `TestMissingToken`: `plan` with an empty env → 1, stderr contains `DISCORD_BOT_TOKEN is not set`, dial not called
  - `TestTokenNeverPrinted`: token `sekrit-token`, dial returns `errors.New("boom")` → 1; neither stdout nor stderr contains `sekrit`
  - `TestPlanFailsPrecheck`: the fake's Director role sits above the provisioner → 1, stderr contains `drag the bot's role above`
  - `TestPlanEmptyThenApplyThenPlan`: plan → 2; apply → 0; plan → 0 with `no changes`
  - `TestInviteURL`: contains `permissions=` + `strconv.FormatInt(perms.Bot, 10)` and `scope=bot`
  - `TestUnknownCommand` → 1
- [ ] **Step 2:** Run `go test ./cmd/...`. Expected: FAIL.
- [ ] **Step 3:** Implement `main.go` using `flag.NewFlagSet` per subcommand.
- [ ] **Step 4:** Run `go test ./...`. Expected: PASS.
- [ ] **Step 5:** Commit `feat: eleve-discord CLI`.

---

### Task 10: Repo content and README

**Files:**
- Create: `server.yaml` (exactly as in §2.2, with `guild_id: "0"` until launch), `content/welcome.md`, `content/apply.md`, `content/open-ledger.md` (verbatim from §3.1, §3.2 and §3.4), `README.md`
- Already present, do not edit: `content/constitution.md`
- Test: `cmd/eleve-discord/repo_test.go`, pointing at `../../server.yaml`

README sections: what this is (one paragraph pointing to the spec); commands and exit codes (§2.5); "Changing the server" (edit `server.yaml` or `content/`, open a PR, read the plan in the job summary, merge to apply); the launch checklist (§4 revision 3, all 10 steps, copied); "Pruning is manual: `eleve-discord apply --prune`".

- [ ] **Step 1: Write the failing test.** `TestRepoServerYAML`: `spec.Load("../../server.yaml")` and `reconcile.FromSpec` succeed; `EveryonePermissions == change_nickname|use_voice_activity`; `len(Content["apply"]) == 1`; `len(Content["constitution"]) >= 2`, each chunk ≤ `content.Limit`; 4 categories; 16 non-category channels (Front Door 5, The Collective 8, Operations 1, Directorate 2).
- [ ] **Step 2:** Run `go test ./...`. Expected: FAIL (files missing).
- [ ] **Step 3:** Add the files.
- [ ] **Step 4:** Run `go test ./... && go run ./cmd/eleve-discord validate`. Expected: PASS, `ok`.
- [ ] **Step 5:** Commit `feat: server spec, content and README`.

---

### Task 11: CI workflow

**Files:**
- Create: `.github/workflows/discord.yaml`

**Don't use `go run` to run the CLI in CI.** `go run` reports any non-zero exit as 1 (checked with Go 1.26: a program exiting 2 gives `rc=1`), which would turn `plan`'s "changes" (2) into a failure. Every job builds the binary first with `go build -o "$RUNNER_TEMP/eleve-discord" ./cmd/eleve-discord` and runs that.

Jobs:
- `test`: runs on `pull_request` and on `push` to `main`. `actions/setup-go` with `go-version-file: go.mod`; `go vet ./...`; `go test ./...`; build; `"$RUNNER_TEMP/eleve-discord" validate`.
- `plan`: `needs: test`; runs on `pull_request` with `if: github.event.pull_request.head.repo.full_name == github.repository`; `environment: discord`. It builds, runs `"$RUNNER_TEMP/eleve-discord" plan` with `DISCORD_BOT_TOKEN: ${{ secrets.DISCORD_BOT_TOKEN }}` and writes the output into `$GITHUB_STEP_SUMMARY` inside a code fence. Exit code 2 is success: `set +e; …; rc=$?; [ "$rc" -eq 1 ] && exit 1; exit 0`.
- `apply`: `needs: test`; runs on `push` to `main`; `environment: discord`; `concurrency: {group: discord-apply, cancel-in-progress: false}`; builds, then runs `"$RUNNER_TEMP/eleve-discord" apply` (never `--prune`).
- Top-level `permissions: contents: read`.

- [ ] **Step 1:** Write the workflow.
- [ ] **Step 2:** Run `go run github.com/rhysd/actionlint/cmd/actionlint@latest`. Expected: no output (exit 0).
- [ ] **Step 3:** Commit `ci: validate, plan on PR, apply on main`.
- [ ] **Step 4:** Push the branch, open a PR, and confirm the `test` job passes. `plan` (and `apply` after merge) will fail until launch step 2 creates the `discord` environment and its secret, which is expected. Note this in the PR description.

---

## Spec coverage

| Spec | Task |
|---|---|
| §1.1 roles, §1.2 matrix and replace rule | 4 (overwrites), 10 (`server.yaml`) |
| §1.1 @everyone's server-wide permissions | 1 (perm names), 2 (field), 4 (desired + single-message rule), 5 (`UpdateEveryone`), 7 (fake), 8 (live), 10 |
| §1.3 topics for `#amendments` and `#vetting` | 10 |
| §2.1 repo layout | 1–11 |
| §2.2 schema and validation, single-message rule | 2, 4 |
| §2.3 precheck, reconcile order, overwrites, unmanaged objects, prune | 5, 6 |
| §2.4 chunking | 3 |
| §2.5 CLI and exit codes | 9 |
| §2.6 bot permissions, invite URL | 1, 9 |
| §2.7 CI | 11 |
| §2.8 testing | 2–9 |
| §3 content | 10 (constitution already committed) |
| §4 launch checklist | README (10); carried out by Aaron after the merge |
