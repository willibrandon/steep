//go:build windows

package agent

import (
	"fmt"
	"path/filepath"

	"golang.org/x/sys/windows"
)

// CheckDiskSpace checks available disk space at the given path.
// Returns an error if disk space cannot be determined.
func CheckDiskSpace(path string) (*DiskSpaceInfo, error) {
	// Get the directory containing the file
	dir := filepath.Dir(path)

	var freeBytesAvailable, totalBytes, totalFreeBytes uint64
	dirPtr, err := windows.UTF16PtrFromString(dir)
	if err != nil {
		return nil, fmt.Errorf("failed to convert path: %w", err)
	}

	err = windows.GetDiskFreeSpaceEx(dirPtr, &freeBytesAvailable, &totalBytes, &totalFreeBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to get disk stats: %w", err)
	}

	info := &DiskSpaceInfo{
		AvailableBytes: freeBytesAvailable,
		TotalBytes:     totalBytes,
	}
	info.UsedBytes = info.TotalBytes - info.AvailableBytes
	if info.TotalBytes > 0 {
		info.UsedPercent = float64(info.UsedBytes) / float64(info.TotalBytes) * 100
	}

	return info, nil
}
