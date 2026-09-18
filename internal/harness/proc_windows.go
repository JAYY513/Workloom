//go:build windows

package harness

import (
	"io"
	"os"
	"os/exec"
	"strconv"
)

// configureProcessGroup is a no-op on Windows: there is no process group to
// set up, and job objects would add a dependency for no gain here.
func configureProcessGroup(cmd *exec.Cmd) {}

// killTree terminates the process and its descendants. taskkill /T walks the
// child list, which is the portable way on Windows to avoid leaving
// grandchildren behind; Process.Kill only reaps the direct child.
func killTree(proc *os.Process) error {
	kill := exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(proc.Pid))
	kill.Stdout = io.Discard
	kill.Stderr = io.Discard
	if err := kill.Run(); err != nil {
		return proc.Kill()
	}
	return nil
}
