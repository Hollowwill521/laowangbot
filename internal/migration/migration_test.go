package migration

import (
	"encoding/json"
	"github.com/OrionG-hub/laowangbot/internal/session"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	s := session.StringSession{DC: 2, Address: "149.154.167.51", Port: 443, AuthKey: make([]byte, 256)}
	enc, e := s.Encode()
	if e != nil {
		t.Fatal(e)
	}
	raw, _ := json.Marshal(map[string]any{"api_id": 123, "api_hash": "test", "session": enc})
	put(t, root, "config.json", string(raw))
	return root
}
func put(t *testing.T, root, name, body string) {
	t.Helper()
	p := filepath.Join(root, name)
	if e := os.MkdirAll(filepath.Dir(p), 0700); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(p, []byte(body), 0600); e != nil {
		t.Fatal(e)
	}
}
func TestMigrationCopiesStateAndPreservesSource(t *testing.T) {
	s := fixture(t)
	put(t, s, "data/nested/state.json", `{"ok":true}`)
	put(t, s, ".env", "MIBOT_PREFIX=!\nMIBOT_UPDATE_REPO=old/old\n")
	d := filepath.Join(t.TempDir(), "new")
	r, e := Run(Options{Source: s, Destination: d, Kind: "mibot-lite"})
	if e != nil {
		t.Fatal(e)
	}
	if r.Kind != "mibot-lite" {
		t.Fatal(r)
	}
	for _, root := range []string{s, d} {
		if b, e := os.ReadFile(filepath.Join(root, "data/nested/state.json")); e != nil || string(b) != `{"ok":true}` {
			t.Fatal(e, string(b))
		}
	}
	b, _ := os.ReadFile(filepath.Join(d, ".env"))
	if !strings.Contains(string(b), "LAOWANGBOT_UPDATE_REPO=OrionG-hub/laowangbot") {
		t.Fatal(string(b))
	}
}
func TestMigrationRejectsOccupiedDestination(t *testing.T) {
	s := fixture(t)
	d := t.TempDir()
	put(t, d, "keep", "safe")
	if _, e := Run(Options{Source: s, Destination: d}); e == nil {
		t.Fatal("accepted occupied directory")
	}
	b, _ := os.ReadFile(filepath.Join(d, "keep"))
	if string(b) != "safe" {
		t.Fatal("changed target")
	}
}
func TestMigrationRejectsSymlinksAndLeavesNoTarget(t *testing.T) {
	s := fixture(t)
	outside := t.TempDir()
	put(t, outside, "secret", "private")
	os.MkdirAll(filepath.Join(s, "data"), 0700)
	if e := os.Symlink(filepath.Join(outside, "secret"), filepath.Join(s, "data", "link")); e != nil {
		t.Skip(e)
	}
	d := filepath.Join(t.TempDir(), "new")
	if _, e := Run(Options{Source: s, Destination: d}); e == nil {
		t.Fatal("accepted symlink")
	}
	if _, e := os.Stat(d); !os.IsNotExist(e) {
		t.Fatalf("partial target: %v", e)
	}
}
func TestTeleBoxInventory(t *testing.T) {
	s := fixture(t)
	put(t, s, "assets/tpm/plugins.json", `{"ping":{"url":"https://old/ping.ts"},"unknown":{"url":"https://old/unknown.ts"}}`)
	put(t, s, "plugins/ping.ts", "old")
	put(t, s, "plugins/unknown.ts", "old")
	put(t, s, "plugins/private.ts", "manual")
	d := filepath.Join(t.TempDir(), "new")
	r, e := Run(Options{Source: s, Destination: d, Kind: "telebox", Builtins: map[string]bool{"ping": true}})
	if e != nil {
		t.Fatal(e)
	}
	if len(r.Plugins) != 3 {
		t.Fatal(r.Plugins)
	}
	statuses := map[string]string{}
	for _, p := range r.Plugins {
		statuses[p.Name] = p.Status
		if p.Name == "ping" && !strings.Contains(p.Source, "OrionG-hub/laowangbot") {
			t.Fatal(p)
		}
	}
	if statuses["ping"] != "builtin" || statuses["unknown"] != "needs-adaptation" || statuses["private"] != "manual" {
		t.Fatal(statuses)
	}
	if _, e := os.Stat(filepath.Join(d, "legacy/plugins/private.ts")); e != nil {
		t.Fatal(e)
	}
}

func TestRejectDestinationSymlinkAncestorIntoSource(t *testing.T) {
	s := fixture(t)
	alias := filepath.Join(t.TempDir(), "alias")
	if e := os.Symlink(s, alias); e != nil {
		t.Skip(e)
	}
	d := filepath.Join(alias, "new")
	if _, e := Run(Options{Source: s, Destination: d}); e == nil {
		t.Fatal("accepted source alias destination")
	}
	if _, e := os.Stat(filepath.Join(s, "new")); !os.IsNotExist(e) {
		t.Fatal("modified source")
	}
}

func TestMigrationCopiesKnownPluginState(t *testing.T) {
	src := fixture(t)
	put(t, src, "assets/monitor/monitor.json", `{"monitor_settings":{"isGlobalEnabled":false}}`)
	put(t, src, "assets/qdsg/signin_config.json", `{"tasks":[],"seq":"7"}`)
	dst := filepath.Join(t.TempDir(), "new")
	if _, e := Run(Options{Source: src, Destination: dst, Kind: "telebox"}); e != nil {
		t.Fatal(e)
	}
	for _, f := range []string{"monitor/monitor.json", "qdsg/signin_config.json"} {
		old, e := os.ReadFile(filepath.Join(src, "assets", f))
		if e != nil {
			t.Fatal(e)
		}
		now, e := os.ReadFile(filepath.Join(dst, "state", f))
		if e != nil || string(old) != string(now) {
			t.Fatal(f, e)
		}
	}
}
