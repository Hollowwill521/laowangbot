// Package platform owns operating-system dependent process facilities.
package platform

import (
	"errors"
	"os"
	"path/filepath"
)

var ErrRunning = errors.New("another instance is using this deployment directory")

// Keep the old lock filename so running mibot-lite and laowangbot cannot share a deployment.
func LockRoot(root string) (*os.File, error) {
	f, e := os.OpenFile(filepath.Join(root, "mibot-lite.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		return nil, e
	}
	if e = lockFile(f); e != nil {
		f.Close()
		return nil, e
	}
	return f, nil
}
