package pathutil

import (
	"path/filepath"
	"runtime"
	"testing"
)

func TestEncodePath(t *testing.T) {
	tests := []struct {
		name string
		path string
		want string
	}{
		{
			name: "unix absolute path",
			path: "/Users/brian/Projects/cdev",
			want: "-Users-brian-Projects-cdev",
		},
		{
			name: "unix root",
			path: "/",
			want: "-",
		},
		{
			name: "trailing slash removed",
			path: "/Users/brian/Projects/cdev/",
			want: "-Users-brian-Projects-cdev",
		},
		{
			name: "double slashes normalised",
			path: "/Users//brian///Projects/cdev",
			want: "-Users-brian-Projects-cdev",
		},
		{
			name: "relative path",
			path: "projects/cdev",
			want: "projects-cdev",
		},
		{
			name: "dot-dot normalised",
			path: "/Users/brian/../brian/Projects",
			want: "-Users-brian-Projects",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := EncodePath(tt.path)
			if got != tt.want {
				t.Errorf("EncodePath(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

func TestEncodePath_PlatformSeparator(t *testing.T) {
	// On any platform, filepath.Clean + ToSlash should produce consistent results.
	// This test verifies that paths built with the native separator work correctly.
	path := filepath.Join("Users", "brian", "Projects")
	got := EncodePath(path)
	want := "Users-brian-Projects"
	if got != want {
		t.Errorf("EncodePath(%q) = %q, want %q", path, got, want)
	}
}

func TestShellCommand(t *testing.T) {
	cmd := ShellCommand("echo hello")
	if runtime.GOOS == "windows" {
		if cmd.Path == "" || cmd.Args[0] != "cmd.exe" {
			t.Errorf("expected cmd.exe on Windows, got %v", cmd.Args)
		}
	} else {
		if len(cmd.Args) < 3 || cmd.Args[0] != "bash" || cmd.Args[1] != "-c" {
			t.Errorf("expected bash -c on Unix, got %v", cmd.Args)
		}
	}
}
