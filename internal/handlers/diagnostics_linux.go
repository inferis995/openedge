//go:build linux

package handlers

import "golang.org/x/sys/unix"

// statfsBytes resolves a path with statfs(2) and converts the block /
// fragment counts to bytes. Linux-only because the syscall struct
// differs across kernels and we deliberately don't pull a heavyweight
// cross-OS dependency just for this one read.
func statfsBytes(path string) (DiskInfo, bool) {
	var s unix.Statfs_t
	if err := unix.Statfs(path, &s); err != nil {
		return DiskInfo{}, false
	}
	total := s.Blocks * uint64(s.Bsize)
	free := s.Bavail * uint64(s.Bsize)
	used := total - free
	pct := 0.0
	if total > 0 {
		pct = float64(used) / float64(total) * 100
	}
	return DiskInfo{
		TotalBytes: total,
		UsedBytes:  used,
		UsedPct:    pct,
	}, true
}
