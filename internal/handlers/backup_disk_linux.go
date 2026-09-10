//go:build linux

package handlers

import "syscall"

// freeSpaceBytes reports the space available to an unprivileged writer on the
// filesystem holding path.
//
// Bavail, not Bfree: the difference is the reserve the filesystem keeps for
// root, and a backup written by the service account cannot touch it. Reporting
// Bfree would promise room that the very next write is refused.
func freeSpaceBytes(path string) (uint64, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, err
	}
	return uint64(st.Bavail) * uint64(st.Bsize), nil
}
