package extensions

import (
	"encoding/json"
	"github.com/OrionG-hub/laowangbot/internal/app"
	"github.com/OrionG-hub/laowangbot/internal/command"
	"github.com/OrionG-hub/laowangbot/internal/plugin"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
)

func fixture(t *testing.T, names []string) *app.App {
	t.Helper()
	root := t.TempDir()
	p := filepath.Join(root, "plugins/demo")
	os.MkdirAll(p, 0700)
	raw, _ := json.Marshal(plugin.Manifest{Name: "demo", Version: "1", ProtocolVersion: 1, Executable: "run", Commands: names})
	os.WriteFile(filepath.Join(p, "manifest.json"), raw, 0600)
	os.WriteFile(filepath.Join(p, "run"), []byte("#!/bin/sh\nexit 0\n"), 0700)
	return &app.App{Root: root, Logger: slog.Default(), Registry: command.New([]string{"，"}, slog.Default())}
}
func TestRegisterPluginsAndManagement(t *testing.T) {
	a := fixture(t, []string{"hello"})
	if e := Register(a); e != nil {
		t.Fatal(e)
	}
	for _, name := range []string{"hello", "tpm"} {
		if _, ok := a.Registry.Lookup(name); !ok {
			t.Fatal(name)
		}
	}
}
func TestCommandCollisionReturnsError(t *testing.T) {
	a := fixture(t, []string{"ping"})
	a.Registry.Register(&command.Command{Name: "ping"})
	if e := Register(a); e == nil {
		t.Fatal("accepted collision")
	}
}
func TestNoPartialRegistrationOnConflict(t *testing.T) {
	a := fixture(t, []string{"hello", "ping"})
	a.Registry.Register(&command.Command{Name: "ping"})
	if e := Register(a); e == nil {
		t.Fatal("accepted collision")
	}
	if _, ok := a.Registry.Lookup("hello"); ok {
		t.Fatal("partial registration")
	}
}
