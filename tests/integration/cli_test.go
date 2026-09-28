//go:build integration

package integration

import (
	"encoding/json"
	"github.com/OrionG-hub/laowangbot/internal/session"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestBuiltCLIMigrationBackupAndPlugins(t *testing.T) {
	repo, e := filepath.Abs("../..")
	if e != nil {
		t.Fatal(e)
	}
	root := t.TempDir()
	binary := filepath.Join(root, "laowangbot")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	build := exec.Command("go", "build", "-o", binary, "./cmd/laowangbot")
	build.Dir = repo
	if b, e := build.CombinedOutput(); e != nil {
		t.Fatal(e, string(b))
	}
	run := func(args ...string) string {
		t.Helper()
		b, e := exec.Command(binary, args...).CombinedOutput()
		if e != nil {
			t.Fatalf("%v: %v\n%s", args, e, b)
		}
		return string(b)
	}
	old := filepath.Join(root, "old")
	os.Mkdir(old, 0700)
	s := session.StringSession{DC: 2, Address: "149.154.167.51", Port: 443, AuthKey: make([]byte, 256)}
	encoded, e := s.Encode()
	if e != nil {
		t.Fatal(e)
	}
	raw, _ := json.Marshal(map[string]any{"api_id": 123, "api_hash": "fixture", "session": encoded})
	os.WriteFile(filepath.Join(old, "config.json"), raw, 0600)
	os.Mkdir(filepath.Join(old, "data"), 0700)
	os.WriteFile(filepath.Join(old, "data/alias.json"), []byte(`{"aliases":{"p":"ping"}}`), 0600)
	dest := filepath.Join(root, "new")
	run("--migrate", old, "--from", "mibot-lite", "--root", dest)
	run("--check", "--root", dest)
	archive := filepath.Join(root, "backup.tgz")
	run("--backup", archive, "--root", dest)
	restored := filepath.Join(root, "restored")
	run("--restore", archive, "--root", restored)
	run("--check", "--root", restored)
	if runtime.GOOS != "windows" {
		run("--plugin", "install-local", "--plugin-source", filepath.Join(repo, "examples/plugins/echo"), "--root", dest)
		if b := run("--plugin", "list", "--root", dest); !strings.Contains(b, "echo 1.0.0") {
			t.Fatal(b)
		}
		run("--check", "--root", dest)
		run("--plugin", "replace-local", "--plugin-source", filepath.Join(repo, "examples/plugins/echo"), "--root", dest)
	}
	for _, kind := range []string{"telebox", "mibox"} {
		legacy := filepath.Join(root, kind)
		os.MkdirAll(filepath.Join(legacy, "assets/alias"), 0700)
		os.MkdirAll(filepath.Join(legacy, "assets/sure"), 0700)
		os.WriteFile(filepath.Join(legacy, "config.json"), raw, 0600)
		for _, name := range []string{"alias", "sure"} {
			body, err := os.ReadFile(filepath.Join(repo, "internal/mibox/testdata", name+".db"))
			if err != nil {
				t.Fatal(err)
			}
			os.WriteFile(filepath.Join(legacy, "assets", name, name+".db"), body, 0600)
		}
		migrated := filepath.Join(root, kind+"-new")
		run("--migrate", legacy, "--from", kind, "--root", migrated)
		run("--check", "--root", migrated)
		for _, name := range []string{"alias", "sure"} {
			body, err := os.ReadFile(filepath.Join(migrated, "data", name+".json"))
			if err != nil || !json.Valid(body) {
				t.Fatalf("%s conversion: %s %v", kind, body, err)
			}
		}
	}
	if b, e := os.ReadFile(filepath.Join(old, "config.json")); e != nil || string(b) != string(raw) {
		t.Fatal("source changed")
	}
}
