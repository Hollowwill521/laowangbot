package platform

import (
	"errors"
	"golang.org/x/sys/windows"
	"os"
)

func lockFile(f *os.File) error {
	var o windows.Overlapped
	e := windows.LockFileEx(windows.Handle(f.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, &o)
	if errors.Is(e, windows.ERROR_LOCK_VIOLATION) {
		return ErrRunning
	}
	return e
}
