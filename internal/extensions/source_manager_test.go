package extensions

import (
	"context"
	"errors"
	"github.com/OrionG-hub/laowangbot/internal/plugin"
	"os"
	"path/filepath"
	"testing"
)

// The build boundary must see staged mutations, not already modified live files.
func TestSourceManagerDoesNotInstallWhenBuildFails(t *testing.T) {
	root := t.TempDir()
	src := t.TempDir()
	os.WriteFile(filepath.Join(src, "manifest.json"), []byte(`{"name":"demo","version":"1","protocol_version":2,"package":"."}`), 0600)
	os.WriteFile(filepath.Join(src, "plugin.go"), []byte("package demo\n"), 0600)
	expected := errors.New("compiler failed")
	m := sourceManager{Manager: plugin.Manager{Root: root}, ctx: context.Background(), apply: func(ctx context.Context, change func(plugin.Manager) error) error {
		if err := change(plugin.Manager{Root: t.TempDir()}); err != nil {
			t.Fatal(err)
		}
		return expected
	}}
	if err := m.InstallLocal(src); !errors.Is(err, expected) {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "plugins", "demo")); !os.IsNotExist(err) {
		t.Fatal("live sources changed", err)
	}
	if m.changed {
		t.Fatal("failed build requests restart")
	}
}
