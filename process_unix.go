//go:build unix

package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"syscall"
)

// Arduino CLI can spawn a compiler or uploader. Stop the whole group on cancel.
func backgroundCommand(ctx context.Context, name string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		} else {
			return err
		}
	}
	return cmd
}
