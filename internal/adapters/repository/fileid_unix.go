//go:build !windows

package repository

import (
	"os"
	"syscall"
)

// GetFileID returns the unique file identifier (inode) for a file.
// On Unix systems, this is the inode number which persists across renames.
func GetFileID(path string) (uint64, error) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, err
	}

	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, nil // Return 0 if we can't get inode
	}

	return stat.Ino, nil
}

// GetFileIDFromInfo extracts the file ID from existing FileInfo.
func GetFileIDFromInfo(info os.FileInfo) uint64 {
	if info == nil {
		return 0
	}

	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0
	}

	return stat.Ino
}
