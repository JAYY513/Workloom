//go:build !windows

package harness

import (
	"os"
	"os/exec"
	"syscall"
)

// configureProcessGroup puts the child in its own process group, so the whole
// tree can be signalled: stopping an attempt must not leave grandchildren
// behind (方案 §4.8).
func configureProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// killTree kills the child's process group, falling back to the process
// itself when the group is already gone.
func killTree(proc *os.Process) error {
	pgid, err := syscall.Getpgid(proc.Pid)
	if err != nil {
		return proc.Kill()
	}
	if err := syscall.Kill(-pgid, syscall.SIGKILL); err != nil {
		return proc.Kill()
	}
	return nil
}
