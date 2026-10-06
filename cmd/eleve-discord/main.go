// Command eleve-discord makes a Discord server match server.yaml.
//
// The bot token is read from the DISCORD_BOT_TOKEN environment variable and
// nowhere else; it is never a flag, never written to a file and never printed.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/theding0x/eleventh-thesis-discord/internal/apply"
	"github.com/theding0x/eleventh-thesis-discord/internal/discord"
	"github.com/theding0x/eleventh-thesis-discord/internal/model"
	"github.com/theding0x/eleventh-thesis-discord/internal/perms"
	"github.com/theding0x/eleventh-thesis-discord/internal/reconcile"
	"github.com/theding0x/eleventh-thesis-discord/internal/spec"
)

const (
	tokenEnv    = "DISCORD_BOT_TOKEN"
	defaultSpec = "server.yaml"
	runTimeout  = 10 * time.Minute
)

// dialer connects to a guild. The provisioner role name is passed so that the
// live client can place roles relative to it.
type dialer func(token, guildID, provisionerRole string) (discord.Client, error)

// live is the production dialer.
func live(token, guildID, provisionerRole string) (discord.Client, error) {
	l, err := discord.NewLive(token, guildID)
	if err != nil {
		return nil, err
	}
	l.SetProvisionerRole(provisionerRole)
	return l, nil
}

func main() {
	os.Exit(run(os.Args[1:], os.Getenv, os.Stdout, os.Stderr, live))
}

const usageText = `usage: eleve-discord <command> [flags]

commands:
  validate   [-f server.yaml]               check the spec offline
  plan       [-f server.yaml] [--prune]     show what would change (exit 2 if anything)
  apply      [-f server.yaml] [--prune]     make the server match the spec
  invite-url -client-id <id>                print the OAuth2 URL that invites the bot

plan and apply read the bot token from the ` + tokenEnv + ` environment variable.
Without --prune nothing is ever deleted.
`

// botTokenShape matches anything shaped like a Discord bot token (three
// base64url parts: user ID, timestamp, HMAC), so a token that was pasted by
// mistake is hidden even when DISCORD_BOT_TOKEN is not set.
var botTokenShape = regexp.MustCompile(`[A-Za-z0-9_-]{23,28}\.[A-Za-z0-9_-]{6,7}\.[A-Za-z0-9_-]{27,}`)

// redact replaces every occurrence of the token (when one is set) and anything
// shaped like a Discord bot token in msg with ***.
func redact(msg, token string) string {
	if token != "" {
		msg = strings.ReplaceAll(msg, token, "***")
	}
	return botTokenShape.ReplaceAllString(msg, "***")
}

// cli carries what one invocation needs.
type cli struct {
	getenv func(string) string
	stdout io.Writer
	stderr io.Writer
	dial   dialer
}

// fail prints an error to stderr with the token (if the environment has one)
// scrubbed out, and returns exit code 1.
func (c *cli) fail(err error) int {
	fmt.Fprintf(c.stderr, "error: %s\n", redact(err.Error(), c.getenv(tokenEnv)))
	return 1
}

// run executes one command and returns the process exit code.
func run(args []string, getenv func(string) string, stdout, stderr io.Writer, dial dialer) int {
	c := &cli{getenv: getenv, stdout: stdout, stderr: stderr, dial: dial}
	if len(args) == 0 {
		fmt.Fprint(stderr, usageText)
		return 1
	}
	switch args[0] {
	case "validate":
		return c.validate(args[1:])
	case "plan":
		return c.planOrApply("plan", args[1:])
	case "apply":
		return c.planOrApply("apply", args[1:])
	case "invite-url":
		return c.inviteURL(args[1:])
	default:
		// The argument is echoed back, so scrub it in case it is the token.
		fmt.Fprintf(stderr, "error: unknown command %q\n\n%s", redact(args[0], getenv(tokenEnv)), usageText)
		return 1
	}
}

// parse parses a subcommand's flags. It returns ok = false, with the exit code
// to use, when the caller should stop: -h/--help (usage on stdout, exit 0), bad
// flags or leftover positional arguments (reported on stderr, exit 1).
//
// The flag package would echo a bad flag or value unredacted ("flag provided
// but not defined: -<token>"), so its own output is discarded and the error is
// printed here, through redact.
func (c *cli) parse(fs *flag.FlagSet, args []string) (ok bool, code int) {
	fs.SetOutput(io.Discard)
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Fprint(c.stdout, usageText)
			return false, 0
		}
		fmt.Fprintf(c.stderr, "error: %s\n\n%s", redact(err.Error(), c.getenv(tokenEnv)), redact(usageText, c.getenv(tokenEnv)))
		return false, 1
	}
	if fs.NArg() > 0 {
		// The argument is echoed back, so scrub it in case it is the token.
		fmt.Fprintf(c.stderr, "error: %s: unexpected argument %q\n", fs.Name(), redact(fs.Arg(0), c.getenv(tokenEnv)))
		return false, 1
	}
	return true, 0
}

// load reads and checks the spec and builds the desired state.
func (c *cli) load(path string) (*spec.Spec, reconcile.Desired, error) {
	s, err := spec.Load(path)
	if err != nil {
		return nil, reconcile.Desired{}, err
	}
	d, err := reconcile.FromSpec(s, filepath.Dir(path), os.ReadFile)
	if err != nil {
		return nil, reconcile.Desired{}, err
	}
	return s, d, nil
}

func (c *cli) validate(args []string) int {
	fs := flag.NewFlagSet("validate", flag.ContinueOnError)
	file := fs.String("f", defaultSpec, "spec file")
	if ok, code := c.parse(fs, args); !ok {
		return code
	}
	if _, _, err := c.load(*file); err != nil {
		return c.fail(err)
	}
	fmt.Fprintln(c.stdout, "ok")
	return 0
}

// contentChannels lists the text channels that have content, in channel order.
func contentChannels(d reconcile.Desired) []string {
	var names []string
	seen := map[string]bool{}
	for _, ch := range d.Channels {
		if ch.Type != model.Text || seen[ch.Name] {
			continue
		}
		if _, ok := d.Content[ch.Name]; ok {
			seen[ch.Name] = true
			names = append(names, ch.Name)
		}
	}
	return names
}

func (c *cli) planOrApply(name string, args []string) int {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	file := fs.String("f", defaultSpec, "spec file")
	prune := fs.Bool("prune", false, "also delete roles and channels the spec does not mention")
	if ok, code := c.parse(fs, args); !ok {
		return code
	}

	s, d, err := c.load(*file)
	if err != nil {
		return c.fail(err)
	}
	token := c.getenv(tokenEnv)
	if token == "" {
		return c.fail(fmt.Errorf("%s is not set", tokenEnv))
	}

	ctx, cancel := context.WithTimeout(context.Background(), runTimeout)
	defer cancel()

	client, err := c.dial(token, s.GuildID, s.ProvisionerRole)
	if err != nil {
		return c.fail(err)
	}
	g, err := client.Observe(ctx, contentChannels(d))
	if err != nil {
		return c.fail(err)
	}
	if err := reconcile.Check(g, d); err != nil {
		return c.fail(err)
	}
	p := reconcile.Reconcile(d, g, *prune)
	fmt.Fprintln(c.stdout, p.String())

	if name == "plan" {
		if len(p.Actions) > 0 {
			return 2
		}
		return 0
	}
	if err := apply.Execute(ctx, client, p.Actions, c.stdout); err != nil {
		return c.fail(err)
	}
	return 0
}

func (c *cli) inviteURL(args []string) int {
	fs := flag.NewFlagSet("invite-url", flag.ContinueOnError)
	clientID := fs.String("client-id", "", "the bot application's client ID")
	if ok, code := c.parse(fs, args); !ok {
		return code
	}
	if *clientID == "" {
		return c.fail(fmt.Errorf("invite-url: -client-id is required"))
	}
	for _, r := range *clientID {
		if r < '0' || r > '9' {
			return c.fail(fmt.Errorf("invite-url: -client-id must contain digits only"))
		}
	}
	fmt.Fprintf(c.stdout, "https://discord.com/oauth2/authorize?client_id=%s&permissions=%s&scope=bot\n",
		*clientID, strconv.FormatInt(perms.Bot, 10))
	return 0
}
