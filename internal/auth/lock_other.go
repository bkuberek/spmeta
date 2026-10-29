//go:build !(darwin || dragonfly || freebsd || linux || netbsd || openbsd)

package auth

import "sync"

// processLock stands in for flock on platforms where the standard library has
// none (Windows, Plan 9, Solaris, AIX, js/wasm). It serializes refreshers
// inside one process only. Two spmeta processes can then refresh at the same
// moment: each sends its own request and the last write to the token file
// wins. If Spotify rotated the refresh token in between, the process holding
// the older one may later get invalid_grant, and `spmeta auth login`
// recovers. spmeta targets macOS and Linux, so this degraded mode only keeps
// other builds compiling; native Windows locking would need a dependency
// (ADR 0006).
var processLock sync.Mutex

// tryLock takes the process-wide lock without blocking. path is unused.
func tryLock(string) (release func(), err error) {
	if !processLock.TryLock() {
		return nil, errLockHeld
	}
	return processLock.Unlock, nil
}
