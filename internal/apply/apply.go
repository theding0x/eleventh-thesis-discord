// Package apply runs a reconcile plan against a Discord client.
package apply

import (
	"context"
	"fmt"
	"io"

	"github.com/theding0x/eleventh-thesis-discord/internal/discord"
	"github.com/theding0x/eleventh-thesis-discord/internal/reconcile"
)

// Execute runs the actions in order against c, printing "done: <action>" to out
// after each success. It stops at the first failure, or when ctx is cancelled
// between actions, and returns an error that names the action ("action i/n").
func Execute(ctx context.Context, c discord.Client, actions []reconcile.Action, out io.Writer) error {
	for i, a := range actions {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("action %d/%d (%s): %w", i+1, len(actions), a, err)
		}
		if err := run(ctx, c, a); err != nil {
			return fmt.Errorf("action %d/%d (%s): %w", i+1, len(actions), a, err)
		}
		if _, err := fmt.Fprintf(out, "done: %s\n", a); err != nil {
			return fmt.Errorf("action %d/%d (%s): write progress: %w", i+1, len(actions), a, err)
		}
	}
	return nil
}

// run maps one action to its client call.
func run(ctx context.Context, c discord.Client, a reconcile.Action) error {
	switch a.Kind {
	case reconcile.UpdateEveryone:
		return c.UpdateEveryone(ctx, a.Permissions)
	case reconcile.CreateRole:
		return c.CreateRole(ctx, a.Role)
	case reconcile.UpdateRole:
		return c.UpdateRole(ctx, a.Role)
	case reconcile.ReorderRoles:
		return c.ReorderRoles(ctx, a.RoleOrder)
	case reconcile.DeleteRole:
		return c.DeleteRole(ctx, a.Role)
	case reconcile.CreateChannel:
		return c.CreateChannel(ctx, a.Channel)
	case reconcile.UpdateChannel:
		return c.UpdateChannel(ctx, a.Channel)
	case reconcile.DeleteChannel:
		return c.DeleteChannel(ctx, a.Channel)
	case reconcile.PostMessage:
		return c.PostMessage(ctx, a.Target, a.Message.Content)
	case reconcile.EditMessage:
		return c.EditMessage(ctx, a.Target, a.Message.ID, a.Message.Content)
	case reconcile.DeleteMessage:
		return c.DeleteMessage(ctx, a.Target, a.Message.ID)
	default:
		return fmt.Errorf("unknown action kind %q", a.Kind)
	}
}
