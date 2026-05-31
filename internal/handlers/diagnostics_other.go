//go:build !linux

package handlers

// statfsBytes stub for non-Linux builds. The diagnostics handler
// degrades to "no disk info" on platforms where /proc isn't available;
// the Postgres / Redis / API health checks still work.
func statfsBytes(_ string) (DiskInfo, bool) { return DiskInfo{}, false }
