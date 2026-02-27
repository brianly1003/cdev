// Package pathutil provides cross-platform path utilities for cdev.
package pathutil

import (
	"context"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// EncodePath converts a filesystem path to a flat string safe for use as
// a directory or file name. This matches the encoding Claude CLI uses for
// session storage under ~/.claude/projects/.
//
// Examples:
//
//	Unix:    /Users/brian/Projects/cdev  → -Users-brian-Projects-cdev
//	Windows: C:\Users\brian\Projects\cdev → -C:-Users-brian-Projects-cdev
func EncodePath(path string) string {
	// filepath.Clean normalises separators and removes trailing slashes.
	// filepath.ToSlash converts OS-specific separators to "/", so the
	// subsequent replace works identically on Unix, macOS, and Windows.
	return strings.ReplaceAll(filepath.ToSlash(filepath.Clean(path)), "/", "-")
}

// ShellCommand returns an *exec.Cmd that runs the given command string
// through the platform's default shell.
//
//	Unix/macOS: bash -c "<command>"
//	Windows:    cmd.exe /C "<command>"
func ShellCommand(command string) *exec.Cmd {
	if runtime.GOOS == "windows" {
		return exec.Command("cmd.exe", "/C", command)
	}
	return exec.Command("bash", "-c", command)
}

// ShellCommandContext is like ShellCommand but accepts a context for cancellation.
func ShellCommandContext(ctx context.Context, command string) *exec.Cmd {
	if runtime.GOOS == "windows" {
		return exec.CommandContext(ctx, "cmd.exe", "/C", command)
	}
	return exec.CommandContext(ctx, "bash", "-c", command)
}
