//go:build !windows

package claude

import (
	"os/exec"
	"syscall"
)

// setupProcess configures the process for Unix systems.
func (m *Manager) setupProcess(cmd *exec.Cmd) {
	// Create new process group so we can kill all children
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
	}
}

// terminateProcess sends SIGTERM to the process group.
func (m *Manager) terminateProcess(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	// Get process group ID
	pgid, err := syscall.Getpgid(cmd.Process.Pid)
	if err != nil {
		// If we can't get pgid, just signal the process directly
		return cmd.Process.Signal(syscall.SIGTERM)
	}
	// Send SIGTERM to entire process group (negative pid)
	return syscall.Kill(-pgid, syscall.SIGTERM)
}

// killProcess sends SIGKILL to the process group.
func (m *Manager) killProcess(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	pgid, err := syscall.Getpgid(cmd.Process.Pid)
	if err != nil {
		return cmd.Process.Kill()
	}
	return syscall.Kill(-pgid, syscall.SIGKILL)
}
