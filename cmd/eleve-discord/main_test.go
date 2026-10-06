package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/theding0x/eleventh-thesis-discord/internal/discord"
	"github.com/theding0x/eleventh-thesis-discord/internal/model"
	"github.com/theding0x/eleventh-thesis-discord/internal/perms"
)

const validSpec = "../../internal/spec/testdata/valid.yaml"

// harness runs the CLI with a fake token env and a dialer that hands out one
// shared Fake, so state persists across invocations.
type harness struct {
	t     *testing.T
	fake  *discord.Fake
	token string
	dials int
	// dialArgs records what the dialer was called with.
	dialArgs [][3]string
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	return &harness{t: t, fake: discord.NewFake("Provisioner"), token: "tok-123"}
}

func (h *harness) dial(token, guildID, provisionerRole string) (discord.Client, error) {
	h.dials++
	h.dialArgs = append(h.dialArgs, [3]string{token, guildID, provisionerRole})
	return h.fake, nil
}

func (h *harness) run(args ...string) (code int, stdout, stderr string) {
	h.t.Helper()
	var out, errb bytes.Buffer
	getenv := func(k string) string {
		if k == "DISCORD_BOT_TOKEN" {
			return h.token
		}
		return ""
	}
	code = run(args, getenv, &out, &errb, h.dial)
	return code, out.String(), errb.String()
}

// observeOnly reports whether the Fake's calls are exactly the given number of
// Observe calls.
func observeOnly(calls []string, n int) bool {
	want := make([]string, n)
	for i := range want {
		want[i] = "Observe"
	}
	return reflect.DeepEqual(calls, want)
}

func has(calls []string, name string) bool {
	for _, c := range calls {
		if c == name {
			return true
		}
	}
	return false
}

func writeFile(t *testing.T, name, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestValidateOK(t *testing.T) {
	h := newHarness(t)
	code, stdout, stderr := h.run("validate", "-f", validSpec)
	if code != 0 || stdout != "ok\n" || stderr != "" {
		t.Fatalf("code=%d stdout=%q stderr=%q, want 0 \"ok\\n\" \"\"", code, stdout, stderr)
	}
	if h.dials != 0 {
		t.Errorf("validate dialed %d times, want 0", h.dials)
	}
}

func TestValidateNeedsNoToken(t *testing.T) {
	h := newHarness(t)
	h.token = ""
	if code, _, stderr := h.run("validate", "-f", validSpec); code != 0 {
		t.Fatalf("code=%d stderr=%q, want 0", code, stderr)
	}
}

func TestValidateInvalid(t *testing.T) {
	bad, err := os.ReadFile(validSpec)
	if err != nil {
		t.Fatal(err)
	}
	body := strings.Replace(string(bad), `"#B22222"`, `"red"`, 1)
	if body == string(bad) {
		t.Fatal("setup: colour not replaced")
	}
	p := writeFile(t, "server.yaml", body)

	h := newHarness(t)
	code, stdout, stderr := h.run("validate", "-f", p)
	if code != 1 {
		t.Fatalf("code = %d, want 1", code)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want empty", stdout)
	}
	if !strings.HasPrefix(stderr, "error: ") {
		t.Errorf("stderr = %q, want an \"error: \" message", stderr)
	}
}

func TestMissingFile(t *testing.T) {
	h := newHarness(t)
	missing := filepath.Join(t.TempDir(), "nope.yaml")
	for _, cmd := range []string{"validate", "plan", "apply"} {
		code, stdout, stderr := h.run(cmd, "-f", missing)
		if code != 1 || stdout != "" {
			t.Errorf("%s: code=%d stdout=%q, want 1 and empty stdout", cmd, code, stdout)
		}
		if !strings.HasPrefix(stderr, "error: ") || !strings.Contains(stderr, "nope.yaml") {
			t.Errorf("%s: stderr = %q, want an error naming the file", cmd, stderr)
		}
	}
	if h.dials != 0 {
		t.Errorf("dialed %d times with an unreadable spec, want 0", h.dials)
	}
}

func TestMissingToken(t *testing.T) {
	for _, cmd := range []string{"plan", "apply"} {
		h := newHarness(t)
		h.token = ""
		code, stdout, stderr := h.run(cmd, "-f", validSpec)
		if code != 1 {
			t.Errorf("%s: code = %d, want 1", cmd, code)
		}
		if stdout != "" {
			t.Errorf("%s: stdout = %q, want empty", cmd, stdout)
		}
		if !strings.Contains(stderr, "DISCORD_BOT_TOKEN is not set") {
			t.Errorf("%s: stderr = %q, want DISCORD_BOT_TOKEN is not set", cmd, stderr)
		}
		if h.dials != 0 {
			t.Errorf("%s: dial called %d times, want 0", cmd, h.dials)
		}
	}
}

func TestDialReceivesTokenGuildAndRole(t *testing.T) {
	h := newHarness(t)
	h.run("plan", "-f", validSpec)
	want := [][3]string{{"tok-123", "1", "Provisioner"}}
	if !reflect.DeepEqual(h.dialArgs, want) {
		t.Errorf("dial args = %v, want %v", h.dialArgs, want)
	}
}

func TestTokenNeverPrinted(t *testing.T) {
	for _, dialErr := range []string{"boom", "login failed for sekrit-token"} {
		for _, cmd := range []string{"plan", "apply"} {
			var out, errb bytes.Buffer
			getenv := func(k string) string {
				if k == "DISCORD_BOT_TOKEN" {
					return "sekrit-token"
				}
				return ""
			}
			dial := func(string, string, string) (discord.Client, error) { return nil, errors.New(dialErr) }
			code := run([]string{cmd, "-f", validSpec}, getenv, &out, &errb, dial)
			if code != 1 {
				t.Errorf("%s/%q: code = %d, want 1", cmd, dialErr, code)
			}
			if strings.Contains(out.String(), "sekrit") || strings.Contains(errb.String(), "sekrit") {
				t.Errorf("%s/%q: token leaked: stdout=%q stderr=%q", cmd, dialErr, out.String(), errb.String())
			}
			if errb.Len() == 0 {
				t.Errorf("%s/%q: stderr is empty, want an error", cmd, dialErr)
			}
		}
	}
	// The redacted message keeps the rest of the text.
	var out, errb bytes.Buffer
	getenv := func(string) string { return "sekrit-token" }
	dial := func(string, string, string) (discord.Client, error) {
		return nil, errors.New("login failed for sekrit-token")
	}
	run([]string{"plan", "-f", validSpec}, getenv, &out, &errb, dial)
	if !strings.Contains(errb.String(), "login failed for ***") {
		t.Errorf("stderr = %q, want the token replaced by ***", errb.String())
	}
}

func TestRedact(t *testing.T) {
	cases := []struct{ msg, token, want string }{
		{"a sekrit b sekrit", "sekrit", "a *** b ***"},
		{"nothing here", "sekrit", "nothing here"},
		{"keep", "", "keep"},
	}
	for _, c := range cases {
		if got := redact(c.msg, c.token); got != c.want {
			t.Errorf("redact(%q, %q) = %q, want %q", c.msg, c.token, got, c.want)
		}
	}
}

func TestObserveErrorIsRedacted(t *testing.T) {
	h := newHarness(t)
	h.token = "sekrit-token"
	h.fake.FailOn = "Observe"
	// leakyClient appends the token to the fake's forced failure.
	var out, errb bytes.Buffer
	dial := func(string, string, string) (discord.Client, error) { return leakyClient{h.fake}, nil }
	getenv := func(string) string { return "sekrit-token" }
	code := run([]string{"plan", "-f", validSpec}, getenv, &out, &errb, dial)
	if code != 1 {
		t.Fatalf("code = %d, want 1", code)
	}
	if strings.Contains(out.String()+errb.String(), "sekrit") {
		t.Errorf("token leaked: stdout=%q stderr=%q", out.String(), errb.String())
	}
	if !strings.Contains(errb.String(), "***") {
		t.Errorf("stderr = %q, want a redacted message", errb.String())
	}
}

func TestApplyErrorIsRedacted(t *testing.T) {
	h := newHarness(t)
	h.fake.FailOn = "UpdateEveryone"
	var out, errb bytes.Buffer
	dial := func(string, string, string) (discord.Client, error) { return leakyClient{h.fake}, nil }
	getenv := func(string) string { return "sekrit-token" }
	code := run([]string{"apply", "-f", validSpec}, getenv, &out, &errb, dial)
	if code != 1 {
		t.Fatalf("code = %d, want 1", code)
	}
	if strings.Contains(out.String()+errb.String(), "sekrit") {
		t.Errorf("token leaked: stdout=%q stderr=%q", out.String(), errb.String())
	}
	if !strings.Contains(errb.String(), "***") {
		t.Errorf("stderr = %q, want a redacted message", errb.String())
	}
}

// leakyClient wraps a Fake and appends the token to every error it returns.
type leakyClient struct{ *discord.Fake }

func (l leakyClient) leak(err error) error {
	if err == nil {
		return nil
	}
	return errors.New(err.Error() + " (token sekrit-token)")
}

func (l leakyClient) Observe(ctx context.Context, ch []string) (model.Guild, error) {
	g, err := l.Fake.Observe(ctx, ch)
	return g, l.leak(err)
}

func (l leakyClient) UpdateEveryone(ctx context.Context, p int64) error {
	return l.leak(l.Fake.UpdateEveryone(ctx, p))
}

func TestPlanFailsPrecheck(t *testing.T) {
	h := newHarness(t)
	h.fake.Guild.Roles = append(h.fake.Guild.Roles, model.Role{ID: "r11", Name: "Director", Position: 11})
	code, stdout, stderr := h.run("plan", "-f", validSpec)
	if code != 1 {
		t.Fatalf("code = %d, want 1", code)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want empty", stdout)
	}
	if !strings.Contains(stderr, "drag the bot's role above") {
		t.Errorf("stderr = %q, want the drag message", stderr)
	}
	if !observeOnly(h.fake.Calls, 1) {
		t.Errorf("fake calls = %v, want exactly [Observe]", h.fake.Calls)
	}

	h.fake.Calls = nil
	code, _, stderr = h.run("apply", "-f", validSpec)
	if code != 1 || !strings.Contains(stderr, "drag the bot's role above") {
		t.Errorf("apply: code=%d stderr=%q, want 1 and the drag message", code, stderr)
	}
	if !observeOnly(h.fake.Calls, 1) {
		t.Errorf("apply mutated after a failed precheck: calls = %v", h.fake.Calls)
	}
}

func TestPlanDoesNotMutate(t *testing.T) {
	h := newHarness(t)
	code, stdout, _ := h.run("plan", "-f", validSpec)
	if code != 2 {
		t.Fatalf("code = %d, want 2", code)
	}
	if !strings.HasSuffix(stdout, "\n") || strings.HasSuffix(stdout, "\n\n") {
		t.Errorf("stdout must end in exactly one newline: %q", stdout)
	}
	if !strings.Contains(stdout, "Director") {
		t.Errorf("stdout = %q, want the plan", stdout)
	}
	if !observeOnly(h.fake.Calls, 1) {
		t.Errorf("fake calls = %v, want exactly [Observe]", h.fake.Calls)
	}
}

func TestPlanEmptyThenApplyThenPlan(t *testing.T) {
	h := newHarness(t)
	if code, _, stderr := h.run("plan", "-f", validSpec); code != 2 {
		t.Fatalf("first plan: code=%d stderr=%q, want 2", code, stderr)
	}
	code, stdout, stderr := h.run("apply", "-f", validSpec)
	if code != 0 {
		t.Fatalf("apply: code=%d stderr=%q, want 0", code, stderr)
	}
	if !strings.Contains(stdout, "done: ") {
		t.Errorf("apply stdout = %q, want progress lines", stdout)
	}
	code, stdout, stderr = h.run("plan", "-f", validSpec)
	if code != 0 || stderr != "" {
		t.Fatalf("second plan: code=%d stderr=%q, want 0", code, stderr)
	}
	if stdout != "no changes\n" {
		t.Errorf("second plan stdout = %q, want \"no changes\\n\"", stdout)
	}
}

func TestApplyWhenNothingToDo(t *testing.T) {
	h := newHarness(t)
	if code, _, stderr := h.run("apply", "-f", validSpec); code != 0 {
		t.Fatalf("apply: code=%d stderr=%q", code, stderr)
	}
	h.fake.Calls = nil
	code, stdout, stderr := h.run("apply", "-f", validSpec)
	if code != 0 || stdout != "no changes\n" || stderr != "" {
		t.Errorf("code=%d stdout=%q stderr=%q, want 0 \"no changes\\n\" \"\"", code, stdout, stderr)
	}
	if !observeOnly(h.fake.Calls, 1) {
		t.Errorf("fake calls = %v, want exactly [Observe]", h.fake.Calls)
	}
}

func TestApplyFailureExitsOne(t *testing.T) {
	h := newHarness(t)
	h.fake.FailOn = "CreateRole"
	code, _, stderr := h.run("apply", "-f", validSpec)
	if code != 1 {
		t.Fatalf("code = %d, want 1", code)
	}
	if !strings.HasPrefix(stderr, "error: ") || !strings.Contains(stderr, "CreateRole") {
		t.Errorf("stderr = %q, want an error naming the failure", stderr)
	}
}

// stray adds an unmanaged text channel and role to the fake.
func stray(f *discord.Fake) {
	f.Guild.Channels = append(f.Guild.Channels, model.Channel{ID: "c900", Name: "stray", Type: model.Text})
	f.Guild.Roles = append(f.Guild.Roles, model.Role{ID: "r900", Name: "Stray", Position: 1})
}

func TestApplyDeletesOnlyWithPrune(t *testing.T) {
	h := newHarness(t)
	if code, _, stderr := h.run("apply", "-f", validSpec); code != 0 {
		t.Fatalf("setup apply: code=%d stderr=%q", code, stderr)
	}
	stray(h.fake)

	h.fake.Calls = nil
	code, stdout, stderr := h.run("apply", "-f", validSpec)
	if code != 0 {
		t.Fatalf("apply: code=%d stderr=%q", code, stderr)
	}
	if has(h.fake.Calls, "DeleteChannel") || has(h.fake.Calls, "DeleteRole") {
		t.Errorf("apply without --prune deleted: %v", h.fake.Calls)
	}
	if !strings.Contains(stdout, "unmanaged:") {
		t.Errorf("stdout = %q, want an unmanaged list", stdout)
	}

	// --prune plans the deletions, and plan alone does not run them.
	h.fake.Calls = nil
	if code, _, _ := h.run("plan", "--prune", "-f", validSpec); code != 2 {
		t.Errorf("plan --prune: code = %d, want 2", code)
	}
	if !observeOnly(h.fake.Calls, 1) {
		t.Errorf("plan --prune mutated: %v", h.fake.Calls)
	}

	h.fake.Calls = nil
	if code, _, stderr := h.run("apply", "--prune", "-f", validSpec); code != 0 {
		t.Fatalf("apply --prune: code=%d stderr=%q", code, stderr)
	}
	if !has(h.fake.Calls, "DeleteChannel") || !has(h.fake.Calls, "DeleteRole") {
		t.Errorf("apply --prune did not delete: %v", h.fake.Calls)
	}
	if code, stdout, _ := h.run("plan", "--prune", "-f", validSpec); code != 0 || stdout != "no changes\n" {
		t.Errorf("plan --prune after prune: code=%d stdout=%q, want 0 \"no changes\\n\"", code, stdout)
	}
}

func TestPruneSingleDashAccepted(t *testing.T) {
	h := newHarness(t)
	if code, _, _ := h.run("apply", "-f", validSpec); code != 0 {
		t.Fatal("setup apply failed")
	}
	stray(h.fake)
	if code, _, stderr := h.run("apply", "-prune", "-f", validSpec); code != 0 {
		t.Fatalf("code=%d stderr=%q", code, stderr)
	}
	if !has(h.fake.Calls, "DeleteChannel") {
		t.Errorf("-prune did not prune: %v", h.fake.Calls)
	}
}

func TestExitCodeUnmanagedOnly(t *testing.T) {
	h := newHarness(t)
	if code, _, _ := h.run("apply", "-f", validSpec); code != 0 {
		t.Fatal("setup apply failed")
	}
	stray(h.fake)
	code, stdout, stderr := h.run("plan", "-f", validSpec)
	if code != 0 || stderr != "" {
		t.Fatalf("code=%d stderr=%q, want 0", code, stderr)
	}
	if !strings.Contains(stdout, "unmanaged:") {
		t.Errorf("stdout = %q, want an unmanaged list", stdout)
	}
}

func TestPruneFlagOnlyOnPlanAndApply(t *testing.T) {
	h := newHarness(t)
	for _, args := range [][]string{
		{"validate", "--prune", "-f", validSpec},
		{"invite-url", "--prune", "-client-id", "123"},
	} {
		code, stdout, stderr := h.run(args...)
		if code != 1 || stdout != "" || stderr == "" {
			t.Errorf("%v: code=%d stdout=%q stderr=%q, want 1, empty stdout, a message", args, code, stdout, stderr)
		}
	}
}

func TestInviteURL(t *testing.T) {
	h := newHarness(t)
	h.token = ""
	code, stdout, stderr := h.run("invite-url", "-client-id", "1234567890")
	want := "https://discord.com/oauth2/authorize?client_id=1234567890&permissions=" +
		strconv.FormatInt(perms.Bot, 10) + "&scope=bot\n"
	if code != 0 || stdout != want || stderr != "" {
		t.Fatalf("code=%d stdout=%q stderr=%q, want 0 %q", code, stdout, stderr, want)
	}
	if h.dials != 0 {
		t.Errorf("invite-url dialed %d times", h.dials)
	}
}

func TestInviteURLMissingClientID(t *testing.T) {
	h := newHarness(t)
	code, stdout, stderr := h.run("invite-url")
	if code != 1 || stdout != "" || stderr == "" {
		t.Errorf("code=%d stdout=%q stderr=%q, want 1, empty stdout, a message", code, stdout, stderr)
	}
}

func TestInviteURLRejectsNonNumeric(t *testing.T) {
	for _, id := range []string{"abc", "12a", "12&permissions=8", "1 2", "-5", "", "١٢٣"} {
		h := newHarness(t)
		code, stdout, stderr := h.run("invite-url", "-client-id", id)
		if code != 1 || stdout != "" || stderr == "" {
			t.Errorf("client id %q: code=%d stdout=%q stderr=%q, want 1, empty stdout, a message", id, code, stdout, stderr)
		}
	}
}

func TestUnknownCommand(t *testing.T) {
	h := newHarness(t)
	code, stdout, stderr := h.run("frobnicate")
	if code != 1 || stdout != "" {
		t.Errorf("code=%d stdout=%q, want 1 and empty stdout", code, stdout)
	}
	if !strings.Contains(stderr, "usage") {
		t.Errorf("stderr = %q, want usage", stderr)
	}
}

func TestNoCommand(t *testing.T) {
	h := newHarness(t)
	code, stdout, stderr := h.run()
	if code != 1 || stdout != "" || !strings.Contains(stderr, "usage") {
		t.Errorf("code=%d stdout=%q stderr=%q, want 1, empty stdout, usage", code, stdout, stderr)
	}
}

func TestFlagErrorsExitOne(t *testing.T) {
	h := newHarness(t)
	for _, args := range [][]string{
		{"plan", "--bogus"},
		{"plan", "-f"},
		{"plan", "extra"},
		{"validate", "extra"},
		{"invite-url", "-client-id", "1", "extra"},
	} {
		code, stdout, stderr := h.run(args...)
		if code != 1 || stdout != "" || stderr == "" {
			t.Errorf("%v: code=%d stdout=%q stderr=%q, want 1, empty stdout, a message", args, code, stdout, stderr)
		}
	}
	if h.dials != 0 {
		t.Errorf("dialed %d times on a usage error", h.dials)
	}
}

func TestUsageDoesNotLeakToken(t *testing.T) {
	h := newHarness(t)
	h.token = "sekrit-token"
	for _, args := range [][]string{
		{"bogus"},
		{},
		{"plan", "-f", validSpec, "sekrit-token"}, // token pasted as an argument
		{"sekrit-token"},
		// The flag package would echo these itself.
		{"plan", "--prune=sekrit-token"}, // invalid boolean value "sekrit-token" for -prune
		{"plan", "-sekrit-token"},        // flag provided but not defined: -sekrit-token
		{"apply", "--sekrit-token"},
		{"validate", "-f"},
	} {
		_, stdout, stderr := h.run(args...)
		if strings.Contains(stdout+stderr, "sekrit") {
			t.Errorf("%v: output leaked the token: %q", args, stdout+stderr)
		}
	}
}

// shapedToken is a fake: it only has the shape of a Discord bot token.
const shapedToken = "MTIzNDU2Nzg5MDEyMzQ1Njc4OQ" + ".GabCdE.abcdefghijklmnopqrstuvwxyz0"

// A token pasted into a flag is hidden even when DISCORD_BOT_TOKEN is unset,
// because redact recognises the shape of a bot token. The env-set case is
// covered by TestUsageDoesNotLeakToken.
func TestFlagErrorsDoNotLeakShapedToken(t *testing.T) {
	for _, envToken := range []string{"", shapedToken} {
		h := newHarness(t)
		h.token = envToken
		for _, args := range [][]string{
			{"plan", "--prune=" + shapedToken},
			{"plan", "-" + shapedToken},
			{"apply", "--" + shapedToken},
			{"apply", "-f", validSpec, shapedToken},
			{shapedToken},
		} {
			code, stdout, stderr := h.run(args...)
			if code != 1 {
				t.Errorf("env=%q %v: code = %d, want 1", envToken, args, code)
			}
			out := stdout + stderr
			if strings.Contains(out, shapedToken) || strings.Contains(out, "abcdefghijklmnopqrstuvwxyz0") {
				t.Errorf("env=%q %v: output leaked the token: %q", envToken, args, out)
			}
			if len(args) > 1 && !strings.Contains(stderr, "***") {
				t.Errorf("env=%q %v: stderr = %q, want the token replaced by ***", envToken, args, stderr)
			}
		}
		if h.dials != 0 {
			t.Errorf("dialed %d times on a usage error", h.dials)
		}
	}
}

func TestFlagErrorIsReportedOnceWithUsage(t *testing.T) {
	h := newHarness(t)
	code, stdout, stderr := h.run("plan", "--prune=maybe")
	if code != 1 || stdout != "" {
		t.Fatalf("code=%d stdout=%q, want 1 and empty stdout", code, stdout)
	}
	if !strings.HasPrefix(stderr, "error: invalid boolean value \"maybe\" for -prune") {
		t.Errorf("stderr = %q, want it to start with the flag error", stderr)
	}
	if !strings.Contains(stderr, "usage: eleve-discord") || strings.Count(stderr, "invalid boolean value") != 1 {
		t.Errorf("stderr = %q, want the error once, then the usage", stderr)
	}
}

func TestHelpPrintsUsageToStdout(t *testing.T) {
	for _, args := range [][]string{
		{"plan", "-h"}, {"plan", "--help"}, {"apply", "-help"}, {"validate", "-h"}, {"invite-url", "--help"},
	} {
		h := newHarness(t)
		code, stdout, stderr := h.run(args...)
		if code != 0 || stderr != "" || !strings.Contains(stdout, "usage: eleve-discord") {
			t.Errorf("%v: code=%d stdout=%q stderr=%q, want 0, usage on stdout, empty stderr", args, code, stdout, stderr)
		}
		if h.dials != 0 {
			t.Errorf("%v: dialed on --help", args)
		}
	}
}

func TestRedactShape(t *testing.T) {
	const inviteURL = "https://discord.com/oauth2/authorize?client_id=123456789012345678&permissions=1099883998294&scope=bot"
	redacted := []struct{ name, msg, token, want string }{
		{"shaped token with no env token", "bad flag -" + shapedToken, "", "bad flag ***"}, // the leading "-" is a legal token character, so it goes too
		{"shaped token inside quotes", `invalid value "` + shapedToken + `" for -prune`, "", `invalid value "***" for -prune`},
		{"url-safe characters", "x MTIzNDU2Nzg5MDEyMzQ1Njc4OQ" + ".G_b-dE.abc_efghijklmnopqrstuvwxyz0 y", "", "x *** y"},
		{"exact token and shaped token together", "sekrit " + shapedToken, "sekrit", "*** ***"},
		{"seven-character middle part", "MTIzNDU2Nzg5MDEyMzQ1Njc4OQ" + ".GabCdEf.abcdefghijklmnopqrstuvwxyz0", "", "***"},
	}
	for _, c := range redacted {
		if got := redact(c.msg, c.token); got != c.want {
			t.Errorf("%s: redact(%q, %q) = %q, want %q", c.name, c.msg, c.token, got, c.want)
		}
	}
	untouched := []string{
		"plain words and a sentence.",
		"error: unknown command \"frobnicate\"",
		inviteURL,
		"role 1234567890.123456.123456789012345678",
		"internal/spec/testdata/content/constitution.md",
		"discord: update @everyone: cannot change @everyone permissions: bit 1<<47",
		"v1.2.3",
		"AaaaaaaaaaaaaaaaaaaaaaaaaAaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaAaaaaaaaaa", // long, no dots
	}
	for _, msg := range untouched {
		if got := redact(msg, ""); got != msg {
			t.Errorf("redact(%q) = %q, want it unchanged", msg, got)
		}
	}
}
