//go:build windows

package gitops

import (
	"context"
	"os/exec"
	"syscall"
)

// ConfigureHiddenCommand hides the transient console window for background CLI processes on Windows.
func ConfigureHiddenCommand(cmd *exec.Cmd) {
	if cmd == nil {
		return
	}
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.HideWindow = true
}

func shellCommand(ctx context.Context, command string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "cmd")
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow: true,
		CmdLine:    `cmd /S /C "` + command + `"`,
	}
	return cmd
}
