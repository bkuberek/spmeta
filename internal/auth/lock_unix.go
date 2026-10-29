//go:build darwin || dragonfly || freebsd || linux || netbsd || openbsd

package auth

import (
	"errors"
	"os"
	"syscall"
)

// tryLock takes an exclusive flock on path without blocking, creating the file
// when needed. It returns errLockHeld while another open file description, in
// this or another process, holds the lock. The kernel drops the lock when the
// holder exits, so a crash leaves nothing stale. The file is never deleted:
// removing it would let two processes lock different inodes.
func tryLock(path string) (release func(), err error) {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, err
	}
	for {
		err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if !errors.Is(err, syscall.EINTR) {
			break
		}
	}
	if err != nil {
		_ = f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, errLockHeld
		}
		return nil, err
	}
	return func() { _ = f.Close() }, nil // closing releases the lock
}
