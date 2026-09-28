//go:build !windows

package platform

import (
	"errors"
	"os"
	"syscall"
)

func lockFile(f *os.File) error {
	e := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if errors.Is(e, syscall.EWOULDBLOCK) || errors.Is(e, syscall.EAGAIN) {
		return ErrRunning
	}
	return e
}
