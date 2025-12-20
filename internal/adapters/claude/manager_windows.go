//go:build windows

package claude

import (
	"os/exec"
	"strconv"
	"syscall"
)

// setupProcess configures the process for Windows.
func (m *Manager) setupProcess(cmd *exec.Cmd) {
	// Create new process group on Windows
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP,
	}
}

// terminateProcess uses taskkill to terminate the process tree on Windows.
func (m *Manager) terminateProcess(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	// Use taskkill with /T to kill child processes too
	kill := exec.Command("taskkill", "/T", "/PID", strconv.Itoa(cmd.Process.Pid))
	return kill.Run()
}

// killProcess forcefully kills the process tree on Windows.
func (m *Manager) killProcess(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	// Use taskkill with /T /F to force kill the process tree
	kill := exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(cmd.Process.Pid))
	return kill.Run()
}
