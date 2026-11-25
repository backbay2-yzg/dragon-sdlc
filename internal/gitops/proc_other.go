//go:build !windows

package gitops

import (
	"context"
	"os/exec"
)

// ConfigureHiddenCommand is a no-op on non-Windows platforms.
func ConfigureHiddenCommand(cmd *exec.Cmd) {}

func shellCommand(ctx context.Context, command string) *exec.Cmd {
	return exec.CommandContext(ctx, "sh", "-c", command)
}
