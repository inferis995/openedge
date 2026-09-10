//go:build !linux

package handlers

import "errors"

// freeSpaceBytes is only implemented for the platform OpenEdge is deployed on.
// Elsewhere it reports that the answer is unknown, and the caller treats an
// unknown as "do not block the backup" — refusing to run because a developer
// is on a laptop would be the wrong trade.
func freeSpaceBytes(string) (uint64, error) {
	return 0, errors.New("free space is only reported on linux")
}
