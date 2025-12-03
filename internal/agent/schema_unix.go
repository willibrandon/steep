//go:build !windows

package agent

import (
	"fmt"
	"path/filepath"
	"syscall"
)

// CheckDiskSpace checks available disk space at the given path.
// Returns an error if disk space cannot be determined.
func CheckDiskSpace(path string) (*DiskSpaceInfo, error) {
	// Get the directory containing the file
	dir := filepath.Dir(path)

	var stat syscall.Statfs_t
	if err := syscall.Statfs(dir, &stat); err != nil {
		return nil, fmt.Errorf("failed to get disk stats: %w", err)
	}

	info := &DiskSpaceInfo{
		AvailableBytes: stat.Bavail * uint64(stat.Bsize),
		TotalBytes:     stat.Blocks * uint64(stat.Bsize),
	}
	info.UsedBytes = info.TotalBytes - info.AvailableBytes
	if info.TotalBytes > 0 {
		info.UsedPercent = float64(info.UsedBytes) / float64(info.TotalBytes) * 100
	}

	return info, nil
}
