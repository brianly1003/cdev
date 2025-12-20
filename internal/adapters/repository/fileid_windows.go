//go:build windows

package repository

import (
	"os"
	"syscall"
)

// GetFileID returns the unique file identifier for a file.
// On Windows, this combines nFileIndexHigh and nFileIndexLow from
// BY_HANDLE_FILE_INFORMATION which persists across renames.
func GetFileID(path string) (uint64, error) {
	pathPtr, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return 0, err
	}

	// Open file to get handle
	handle, err := syscall.CreateFile(
		pathPtr,
		syscall.GENERIC_READ,
		syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE|syscall.FILE_SHARE_DELETE,
		nil,
		syscall.OPEN_EXISTING,
		syscall.FILE_FLAG_BACKUP_SEMANTICS, // Required for directories
		0,
	)
	if err != nil {
		return 0, err
	}
	defer syscall.CloseHandle(handle)

	var info syscall.ByHandleFileInformation
	if err := syscall.GetFileInformationByHandle(handle, &info); err != nil {
		return 0, err
	}

	// Combine high and low parts into a single 64-bit ID
	fileID := (uint64(info.FileIndexHigh) << 32) | uint64(info.FileIndexLow)
	return fileID, nil
}

// GetFileIDFromInfo extracts the file ID from existing FileInfo.
// On Windows, we need to re-open the file to get the ID, so this
// falls back to GetFileID if we have the path.
func GetFileIDFromInfo(info os.FileInfo) uint64 {
	// Windows doesn't expose file ID through os.FileInfo.Sys()
	// in a reliable way, so return 0 and let caller use GetFileID
	return 0
}
