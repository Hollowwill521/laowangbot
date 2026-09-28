package platform

import (
	"errors"
	"testing"
)

func TestLockExclusionAndRelease(t *testing.T) {
	root := t.TempDir()
	a, e := LockRoot(root)
	if e != nil {
		t.Fatal(e)
	}
	if b, e := LockRoot(root); !errors.Is(e, ErrRunning) {
		if b != nil {
			b.Close()
		}
		t.Fatalf("second lock: %v", e)
	}
	a.Close()
	b, e := LockRoot(root)
	if e != nil {
		t.Fatal(e)
	}
	b.Close()
}
