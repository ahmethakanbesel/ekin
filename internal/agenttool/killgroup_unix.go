//go:build unix

package agenttool

import (
	"os/exec"
	"syscall"
	"time"
)

// killGroup runs cmd in a process group of its own and stops the whole
// group when its context ends, so a build's children (a dev server, a
// bundler's workers) do not outlive it.
func killGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	cmd.WaitDelay = 5 * time.Second
}
