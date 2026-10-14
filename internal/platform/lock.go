// Package platform owns operating-system dependent process facilities.
package platform

import (
	"errors"
	"os"
	"path/filepath"
)

var ErrRunning = errors.New("another instance is using this deployment directory")

// LegacyLockName is shared with mibot-lite so old and new binaries cannot
// serve the same deployment concurrently.
const LegacyLockName = "mibot-lite.lock"

func LockRoot(root string) (*os.File, error) {
	f, e := os.OpenFile(filepath.Join(root, LegacyLockName), os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		return nil, e
	}
	if e = lockFile(f); e != nil {
		f.Close()
		return nil, e
	}
	return f, nil
}
