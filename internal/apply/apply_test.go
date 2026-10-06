package apply_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/theding0x/eleventh-thesis-discord/internal/apply"
	"github.com/theding0x/eleventh-thesis-discord/internal/discord"
	"github.com/theding0x/eleventh-thesis-discord/internal/model"
	"github.com/theding0x/eleventh-thesis-discord/internal/reconcile"
	"github.com/theding0x/eleventh-thesis-discord/internal/spec"
)

var ctx = context.Background()

func desired(t *testing.T) reconcile.Desired {
	t.Helper()
	s, err := spec.Load("../spec/testdata/valid.yaml")
	if err != nil {
		t.Fatalf("spec.Load: %v", err)
	}
	d, err := reconcile.FromSpec(s, "../spec/testdata", os.ReadFile)
	if err != nil {
		t.Fatalf("FromSpec: %v", err)
	}
	return d
}

// contentChannels lists the channels the desired state manages messages in.
func contentChannels(d reconcile.Desired) []string {
	var names []string
	for name := range d.Content {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// plan observes the fake, checks it and reconciles it, as the CLI does.
func plan(t *testing.T, f *discord.Fake, d reconcile.Desired, prune bool) reconcile.Plan {
	t.Helper()
	g, err := f.Observe(ctx, contentChannels(d))
	if err != nil {
		t.Fatalf("Observe: %v", err)
	}
	if err := reconcile.Check(g, d); err != nil {
		t.Fatalf("Check: %v", err)
	}
	return reconcile.Reconcile(d, g, prune)
}

// converge plans and applies once, returning the plan that was applied.
func converge(t *testing.T, f *discord.Fake, d reconcile.Desired, prune bool) reconcile.Plan {
	t.Helper()
	p := plan(t, f, d, prune)
	if err := apply.Execute(ctx, f, p.Actions, &bytes.Buffer{}); err != nil {
		t.Fatalf("Execute: %v\nplan:\n%s", err, p)
	}
	return p
}

func TestApplyFromEmptyConverges(t *testing.T) {
	d := desired(t)
	f := discord.NewFake("Provisioner")

	first := converge(t, f, d, false)
	if len(first.Actions) == 0 {
		t.Fatal("first plan has no actions")
	}
	if f.Guild.EveryonePermissions != d.EveryonePermissions {
		t.Errorf("everyone = %d, want %d", f.Guild.EveryonePermissions, d.EveryonePermissions)
	}
	if again := plan(t, f, d, false); again.String() != "no changes" {
		t.Errorf("second plan:\n%s\nwant no changes", again)
	}
}

func TestApplyStopsAtFirstFailure(t *testing.T) {
	d := desired(t)
	f := discord.NewFake("Provisioner")
	f.FailOn = "CreateChannel"

	p := plan(t, f, d, false)
	err := apply.Execute(ctx, f, p.Actions, &bytes.Buffer{})
	if err == nil {
		t.Fatal("want an error")
	}
	// Roles are planned before channels: update-everyone, 3 roles, reorder, then
	// the first channel is action 6.
	want := fmt.Sprintf("action 6/%d (+ create category Front Door)", len(p.Actions))
	if !strings.Contains(err.Error(), want) {
		t.Errorf("err = %q, want it to contain %q", err, want)
	}
	if !strings.Contains(err.Error(), "CreateChannel") {
		t.Errorf("err = %q, want the client's error wrapped", err)
	}
	for _, c := range f.Calls {
		if c == "PostMessage" {
			t.Errorf("PostMessage was called after the failure: %v", f.Calls)
		}
	}
	if n := countCalls(f.Calls, "CreateChannel"); n != 1 {
		t.Errorf("CreateChannel called %d times, want 1 (stop at the first failure)", n)
	}

	f.FailOn = ""
	converge(t, f, d, false)
	if again := plan(t, f, d, false); again.String() != "no changes" {
		t.Errorf("plan after retry:\n%s\nwant no changes", again)
	}
}

func countCalls(calls []string, name string) int {
	n := 0
	for _, c := range calls {
		if c == name {
			n++
		}
	}
	return n
}

func TestApplyReportsProgress(t *testing.T) {
	d := desired(t)
	f := discord.NewFake("Provisioner")
	p := plan(t, f, d, false)
	var out bytes.Buffer
	if err := apply.Execute(ctx, f, p.Actions, &out); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSuffix(out.String(), "\n"), "\n")
	if len(lines) != len(p.Actions) {
		t.Fatalf("%d output lines for %d actions:\n%s", len(lines), len(p.Actions), out.String())
	}
	for i, a := range p.Actions {
		if want := "done: " + a.String(); lines[i] != want {
			t.Errorf("line %d = %q, want %q", i, lines[i], want)
		}
	}
	if !strings.Contains(out.String(), "done: + create role Director\n") {
		t.Errorf("output lacks the Director line:\n%s", out.String())
	}
}

func TestApplyFailureIsNotReportedAsDone(t *testing.T) {
	d := desired(t)
	f := discord.NewFake("Provisioner")
	f.FailOn = "CreateRole"
	p := plan(t, f, d, false)
	var out bytes.Buffer
	if err := apply.Execute(ctx, f, p.Actions, &out); err == nil {
		t.Fatal("want an error")
	}
	if strings.Contains(out.String(), "create role") {
		t.Errorf("output reports a failed action as done:\n%s", out.String())
	}
	if want := "done: ~ update @everyone permissions\n"; out.String() != want {
		t.Errorf("output = %q, want %q", out.String(), want)
	}
}

// messy builds a realistic half-configured server: Discord's default @everyone
// permissions, an existing #general with a hand-added member overwrite, a
// duplicate #general appended after it, a stray channel, a stray role and a
// Member role with the wrong colour.
func messy(t *testing.T, d reconcile.Desired) *discord.Fake {
	t.Helper()
	f := discord.NewFake("Provisioner")

	var category, general model.Channel
	for _, c := range d.Channels {
		switch {
		case c.Type == model.Category && c.Name == "The Collective":
			category = c
		case c.Type == model.Text && c.Name == "general":
			general = c
		}
	}
	category.ID = "c101"
	general.ID = "c102"
	general.Overwrites = append(append([]model.Overwrite(nil), general.Overwrites...),
		model.Overwrite{Target: "member:42", Allow: 1})
	sort.Slice(general.Overwrites, func(i, j int) bool { return general.Overwrites[i].Target < general.Overwrites[j].Target })

	dup := general
	dup.ID = "c103"
	dup.Overwrites = nil
	stray := model.Channel{ID: "c104", Name: "old", Type: model.Text, Parent: "The Collective", Position: 9}

	f.Guild.Channels = []model.Channel{category, general, dup, stray}
	f.Guild.Roles = append(f.Guild.Roles,
		model.Role{ID: "r101", Name: "Old", Position: 1},
		model.Role{ID: "r102", Name: "Member", Position: 3, Color: 1, Hoist: true},
	)
	if f.Guild.EveryonePermissions == d.EveryonePermissions {
		t.Fatal("test setup: @everyone already matches")
	}
	return f
}

func channelByID(g model.Guild, id string) (model.Channel, bool) {
	for _, c := range g.Channels {
		if c.ID == id {
			return c, true
		}
	}
	return model.Channel{}, false
}

func TestApplyConvergesFromMessyGuildWithoutPrune(t *testing.T) {
	d := desired(t)
	f := messy(t, d)

	first := plan(t, f, d, false)
	for _, a := range first.Actions {
		if a.Kind == reconcile.DeleteChannel || a.Kind == reconcile.DeleteRole {
			t.Fatalf("plan without prune contains a delete: %s", a)
		}
	}
	if !strings.Contains(first.String(), "~ update text #general (overwrites)") {
		t.Fatalf("setup: the member overwrite is not planned for removal:\n%s", first)
	}
	converge(t, f, d, false)

	again := plan(t, f, d, false)
	if len(again.Actions) != 0 {
		t.Fatalf("actions remain after apply:\n%s", again)
	}
	wantUnmanaged := []string{"role Old", "text #general (duplicate)", "text #old"}
	if !reflect.DeepEqual(again.Unmanaged, wantUnmanaged) {
		t.Errorf("unmanaged = %q, want %q", again.Unmanaged, wantUnmanaged)
	}

	g, _ := f.Observe(ctx, nil)
	if g.EveryonePermissions != d.EveryonePermissions {
		t.Errorf("everyone = %d, want %d", g.EveryonePermissions, d.EveryonePermissions)
	}
	general, ok := channelByID(g, "c102")
	if !ok {
		t.Fatal("the original #general was lost")
	}
	for _, o := range general.Overwrites {
		if o.Target == "member:42" {
			t.Error("member:42 overwrite survived")
		}
	}
	if _, ok := channelByID(g, "c103"); !ok {
		t.Error("duplicate #general was deleted without --prune")
	}
	if _, ok := channelByID(g, "c104"); !ok {
		t.Error("stray #old was deleted without --prune")
	}
	for _, r := range g.Roles {
		if r.Name == "Member" && (r.ID != "r102" || r.Color == 1) {
			t.Errorf("Member role = %+v, want r102 updated in place", r)
		}
	}
}

func TestApplyConvergesFromMessyGuildWithPrune(t *testing.T) {
	d := desired(t)
	f := messy(t, d)

	first := converge(t, f, d, true)
	deletes := map[string]bool{}
	for _, a := range first.Actions {
		switch a.Kind {
		case reconcile.DeleteChannel:
			deletes[a.Channel.ID] = true
		case reconcile.DeleteRole:
			deletes[a.Role.ID] = true
		}
	}
	if want := map[string]bool{"c103": true, "c104": true, "r101": true}; !reflect.DeepEqual(deletes, want) {
		t.Errorf("deleted = %v, want %v", deletes, want)
	}

	second := converge(t, f, d, true)
	if len(second.Actions) != 0 {
		t.Errorf("second apply was not a no-op:\n%s", second)
	}
	if final := plan(t, f, d, true); final.String() != "no changes" {
		t.Errorf("final plan:\n%s\nwant no changes", final)
	}

	g, _ := f.Observe(ctx, nil)
	if _, ok := channelByID(g, "c102"); !ok {
		t.Error("the first #general (c102) was deleted instead of the duplicate")
	}
	if _, ok := channelByID(g, "c103"); ok {
		t.Error("the duplicate #general (c103) survived")
	}
	if _, ok := channelByID(g, "c104"); ok {
		t.Error("stray #old survived")
	}
	for _, r := range g.Roles {
		if r.Name == "Old" {
			t.Error("stray role Old survived")
		}
	}
}

func messageIDs(g model.Guild, channel string) []string {
	var ids []string
	for _, m := range g.BotMessages[channel] {
		ids = append(ids, m.ID)
	}
	return ids
}

func TestApplyContentEditPostAndDelete(t *testing.T) {
	d := desired(t)
	f := discord.NewFake("Provisioner")
	converge(t, f, d, false)

	chunks := append([]string(nil), d.Content["constitution"]...)
	if len(chunks) < 3 {
		t.Fatalf("test needs a constitution of at least 3 chunks, have %d", len(chunks))
	}
	g, _ := f.Observe(ctx, []string{"constitution"})
	ids := messageIDs(g, "constitution")
	if len(ids) != len(chunks) {
		t.Fatalf("observed %d messages for %d chunks", len(ids), len(chunks))
	}

	// Change one chunk: one edit, the message keeps its ID.
	changed := append([]string(nil), chunks...)
	changed[1] = "Changed article."
	d.Content["constitution"] = changed
	p := converge(t, f, d, false)
	if len(p.Actions) != 1 || p.Actions[0].Kind != reconcile.EditMessage {
		t.Fatalf("plan:\n%s\nwant a single edit", p)
	}
	g, _ = f.Observe(ctx, []string{"constitution"})
	if got := messageIDs(g, "constitution"); !reflect.DeepEqual(got, ids) {
		t.Errorf("IDs after edit = %v, want %v", got, ids)
	}
	if got := g.BotMessages["constitution"][1].Content; got != "Changed article." {
		t.Errorf("edited content = %q", got)
	}
	if again := plan(t, f, d, false); again.String() != "no changes" {
		t.Errorf("not converged after edit:\n%s", again)
	}

	// Shorten the content: the surplus messages are deleted, the rest untouched.
	d.Content["constitution"] = changed[:1]
	p = converge(t, f, d, false)
	for _, a := range p.Actions {
		if a.Kind != reconcile.DeleteMessage {
			t.Errorf("unexpected action %s", a)
		}
	}
	if len(p.Actions) != len(chunks)-1 {
		t.Errorf("%d delete actions, want %d", len(p.Actions), len(chunks)-1)
	}
	g, _ = f.Observe(ctx, []string{"constitution"})
	if got := messageIDs(g, "constitution"); !reflect.DeepEqual(got, ids[:1]) {
		t.Errorf("IDs after delete = %v, want %v", got, ids[:1])
	}
	if again := plan(t, f, d, false); again.String() != "no changes" {
		t.Errorf("not converged after delete:\n%s", again)
	}

	// Grow it again: new messages are posted after the surviving one.
	d.Content["constitution"] = chunks
	converge(t, f, d, false)
	g, _ = f.Observe(ctx, []string{"constitution"})
	got := g.BotMessages["constitution"]
	if len(got) != len(chunks) || got[0].ID != ids[0] {
		t.Errorf("messages after growing = %+v", got)
	}
	for i, m := range got {
		if m.Content != chunks[i] {
			t.Errorf("message %d content = %q, want chunk %d", i, m.Content, i)
		}
	}
	if again := plan(t, f, d, false); again.String() != "no changes" {
		t.Errorf("not converged after growing:\n%s", again)
	}
}

func TestExecuteCancelledContextRunsNothing(t *testing.T) {
	d := desired(t)
	f := discord.NewFake("Provisioner")
	p := plan(t, f, d, false)
	f.Calls = nil

	cctx, cancel := context.WithCancel(ctx)
	cancel()
	var out bytes.Buffer
	err := apply.Execute(cctx, f, p.Actions, &out)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if want := fmt.Sprintf("action 1/%d (", len(p.Actions)); !strings.Contains(err.Error(), want) {
		t.Errorf("err = %q, want it to contain %q", err, want)
	}
	if len(f.Calls) != 0 || out.Len() != 0 {
		t.Errorf("calls = %v, output = %q, want nothing", f.Calls, out.String())
	}
}

func TestExecuteStopsWhenContextIsCancelledBetweenActions(t *testing.T) {
	cctx, cancel := context.WithCancel(ctx)
	defer cancel()
	rc := &recorder{onCall: func(n int) {
		if n == 1 {
			cancel()
		}
	}}
	actions := []reconcile.Action{
		{Kind: reconcile.UpdateEveryone, Permissions: 5},
		{Kind: reconcile.CreateRole, Role: model.Role{Name: "A"}},
	}
	err := apply.Execute(cctx, rc, actions, &bytes.Buffer{})
	if !errors.Is(err, context.Canceled) || !strings.Contains(err.Error(), "action 2/2") {
		t.Fatalf("err = %v, want context.Canceled at action 2/2", err)
	}
	if len(rc.calls) != 1 {
		t.Errorf("calls = %q, want only the first", rc.calls)
	}
}

// recorder is a discord.Client that records each call with its arguments.
type recorder struct {
	calls  []string
	onCall func(n int)
	err    error
}

func (r *recorder) rec(format string, args ...any) error {
	r.calls = append(r.calls, fmt.Sprintf(format, args...))
	if r.onCall != nil {
		r.onCall(len(r.calls))
	}
	return r.err
}

func (r *recorder) Observe(context.Context, []string) (model.Guild, error) {
	return model.Guild{}, r.rec("Observe")
}
func (r *recorder) UpdateEveryone(_ context.Context, p int64) error {
	return r.rec("UpdateEveryone(%d)", p)
}
func (r *recorder) CreateRole(_ context.Context, x model.Role) error {
	return r.rec("CreateRole(%s)", x.Name)
}
func (r *recorder) UpdateRole(_ context.Context, x model.Role) error {
	return r.rec("UpdateRole(%s)", x.Name)
}
func (r *recorder) ReorderRoles(_ context.Context, names []string) error {
	return r.rec("ReorderRoles(%s)", strings.Join(names, ","))
}
func (r *recorder) DeleteRole(_ context.Context, x model.Role) error {
	return r.rec("DeleteRole(%s,%s)", x.ID, x.Name)
}
func (r *recorder) CreateChannel(_ context.Context, c model.Channel) error {
	return r.rec("CreateChannel(%s)", c.Name)
}
func (r *recorder) UpdateChannel(_ context.Context, c model.Channel) error {
	return r.rec("UpdateChannel(%s)", c.Name)
}
func (r *recorder) DeleteChannel(_ context.Context, c model.Channel) error {
	return r.rec("DeleteChannel(%s,%s)", c.ID, c.Name)
}
func (r *recorder) PostMessage(_ context.Context, ch, content string) error {
	return r.rec("PostMessage(%s,%s)", ch, content)
}
func (r *recorder) EditMessage(_ context.Context, ch, id, content string) error {
	return r.rec("EditMessage(%s,%s,%s)", ch, id, content)
}
func (r *recorder) DeleteMessage(_ context.Context, ch, id string) error {
	return r.rec("DeleteMessage(%s,%s)", ch, id)
}

var _ discord.Client = (*recorder)(nil)

func TestExecuteMapsEveryKindToItsClientCall(t *testing.T) {
	actions := []reconcile.Action{
		{Kind: reconcile.UpdateEveryone, Permissions: 7},
		{Kind: reconcile.CreateRole, Role: model.Role{Name: "A"}},
		{Kind: reconcile.UpdateRole, Role: model.Role{Name: "B"}},
		{Kind: reconcile.ReorderRoles, RoleOrder: []string{"A", "B"}},
		{Kind: reconcile.DeleteRole, Role: model.Role{ID: "r5", Name: "C"}},
		{Kind: reconcile.CreateChannel, Channel: model.Channel{Name: "x"}},
		{Kind: reconcile.UpdateChannel, Channel: model.Channel{Name: "y"}},
		{Kind: reconcile.DeleteChannel, Channel: model.Channel{ID: "c9", Name: "z"}},
		{Kind: reconcile.PostMessage, Target: "chat", Message: model.Message{Content: "hi"}},
		{Kind: reconcile.EditMessage, Target: "chat", Message: model.Message{ID: "m1", Content: "yo"}},
		{Kind: reconcile.DeleteMessage, Target: "chat", Message: model.Message{ID: "m2"}},
	}
	rc := &recorder{}
	if err := apply.Execute(ctx, rc, actions, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"UpdateEveryone(7)",
		"CreateRole(A)",
		"UpdateRole(B)",
		"ReorderRoles(A,B)",
		"DeleteRole(r5,C)",
		"CreateChannel(x)",
		"UpdateChannel(y)",
		"DeleteChannel(c9,z)",
		"PostMessage(chat,hi)",
		"EditMessage(chat,m1,yo)",
		"DeleteMessage(chat,m2)",
	}
	if !reflect.DeepEqual(rc.calls, want) {
		t.Errorf("calls =\n%q\nwant\n%q", rc.calls, want)
	}
}

func TestExecuteUnknownKindIsAnError(t *testing.T) {
	rc := &recorder{}
	actions := []reconcile.Action{
		{Kind: reconcile.CreateRole, Role: model.Role{Name: "A"}},
		{Kind: "bogus"},
		{Kind: reconcile.CreateRole, Role: model.Role{Name: "B"}},
	}
	var out bytes.Buffer
	err := apply.Execute(ctx, rc, actions, &out)
	if err == nil || !strings.Contains(err.Error(), "action 2/3 (bogus)") {
		t.Fatalf("err = %v, want one naming action 2/3 (bogus)", err)
	}
	if len(rc.calls) != 1 {
		t.Errorf("calls = %q, want only the first", rc.calls)
	}
}

func TestExecuteWrapsClientError(t *testing.T) {
	boom := errors.New("boom")
	rc := &recorder{err: boom}
	err := apply.Execute(ctx, rc, []reconcile.Action{{Kind: reconcile.CreateRole, Role: model.Role{Name: "A"}}}, &bytes.Buffer{})
	if !errors.Is(err, boom) {
		t.Errorf("err = %v, want it to wrap the client error", err)
	}
	if want := "action 1/1 (+ create role A): boom"; err == nil || err.Error() != want {
		t.Errorf("err = %v, want %q", err, want)
	}
}

func TestExecuteNoActions(t *testing.T) {
	var out bytes.Buffer
	if err := apply.Execute(ctx, &recorder{}, nil, &out); err != nil || out.Len() != 0 {
		t.Errorf("err = %v, out = %q, want a silent success", err, out.String())
	}
}

// Guard against a vacuous convergence test: the fixture's @everyone must differ
// from Discord's default, or UpdateEveryone would never be exercised.
func TestFixtureEveryoneDiffersFromDiscordDefault(t *testing.T) {
	if desired(t).EveryonePermissions == discord.DefaultEveryone {
		t.Fatal("fixture @everyone equals Discord's default")
	}
}
