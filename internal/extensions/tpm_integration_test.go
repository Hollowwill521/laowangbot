//go:build integration

package extensions

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/OrionG-hub/laowangbot/internal/plugin"
)

// Exercise the same command dispatcher used by Telegram with real plugin files.
func TestTPMLocalPackageLifecycle(t *testing.T) {
	ctx := context.Background()
	a := fixture(t, []string{"hello"})
	defer a.Close()
	raw, err := json.Marshal(staticEntries([]string{"hello"})[0].Manifest)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(a.Root, "plugins", "demo")
	if err = os.WriteFile(filepath.Join(dir, "manifest.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(filepath.Join(dir, "run")); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, "plugin.go"), []byte("package demo\n"), 0600); err != nil {
		t.Fatal(err)
	}

	m := plugin.Manager{Root: a.Root}
	state := filepath.Join(a.Root, "state/demo")
	if e := os.MkdirAll(state, 0700); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(state, "saved.json"), []byte(`{"saved":true}`), 0600); e != nil {
		t.Fatal(e)
	}
	r, e := executeTPM(ctx, m, []string{"lv"}, nil)
	if e != nil || !strings.Contains(r.Text, "demo 1") {
		t.Fatal(r, e)
	}
	archive, e := executeTPM(ctx, m, []string{"ul", "demo"}, nil)
	if e != nil || len(archive.File) == 0 {
		t.Fatal(e)
	}
	r, e = executeTPM(ctx, m, []string{"rm", "all"}, nil)
	if e != nil || !strings.Contains(r.Text, "成功 1") {
		t.Fatal(r, e)
	}
	if b, e := os.ReadFile(filepath.Join(state, "saved.json")); e != nil || string(b) != `{"saved":true}` {
		t.Fatal("state lost", e)
	}
	name, e := m.ImportPackage(archive.File, false)
	if e != nil || name != "demo" {
		t.Fatal(name, e)
	}
	r, e = executeTPM(ctx, m, []string{"ua", "-f"}, nil)
	if e != nil || !strings.Contains(r.Text, "跳过 1") || !strings.Contains(r.Text, "成功 0") {
		t.Fatal(r, e)
	}
	if e = Register(a); e != nil {
		t.Fatal(e)
	}
	if _, ok := a.Registry.Lookup("hello"); ok {
		t.Fatal("disk import changed compiled runtime")
	}
}
