package app

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/OrionG-hub/laowangbot/internal/session"
	"os"
	"path/filepath"
	"testing"
)

func prepareFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	s := session.StringSession{DC: 2, Address: "149.154.167.51", Port: 443, AuthKey: make([]byte, 256)}
	encoded, err := s.Encode()
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(map[string]any{"api_id": 123, "api_hash": "fixture", "session": encoded})
	if err = os.WriteFile(filepath.Join(root, "config.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	return root
}
func TestPrepareRefusesBusyRootBeforeSessionWrite(t *testing.T) {
	root := prepareFixture(t)
	lock, err := LockRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	a, err := Prepare(context.Background(), Options{Root: root})
	if a != nil {
		a.Close()
	}
	if !errors.Is(err, ErrRunning) {
		t.Fatal(err)
	}
	if _, err = os.Stat(filepath.Join(root, SessionFile)); !os.IsNotExist(err) {
		t.Fatalf("busy root mutated: %v", err)
	}
}
func TestPrepareFailureReleasesRoot(t *testing.T) {
	root := prepareFixture(t)
	os.WriteFile(filepath.Join(root, SessionFile), []byte("invalid"), 0600)
	if a, err := Prepare(context.Background(), Options{Root: root}); err == nil {
		a.Close()
		t.Fatal("accepted invalid stored session")
	}
	lock, err := LockRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	lock.Close()
}
