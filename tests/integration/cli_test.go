//go:build integration

package integration

import (
	"bytes"
	"encoding/json"
	"github.com/OrionG-hub/laowangbot/internal/session"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

func TestBuiltCLIMigrationBackupAndPlugins(t *testing.T) {
	repo, e := filepath.Abs("../..")
	if e != nil {
		t.Fatal(e)
	}
	t.Setenv("LAOWANGBOT_SOURCE", repo)
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
	checkDir := filepath.Join(root, "validation-root")
	if err := os.Mkdir(checkDir, 0700); err != nil {
		t.Fatal(err)
	}
	run("--check-plugins", "--root", checkDir)
	if entries, err := os.ReadDir(checkDir); err != nil || len(entries) != 0 {
		t.Fatalf("check-plugins changed target: %v %v", entries, err)
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
	if runtime.GOOS != "windows" {
		wizardRoot := filepath.Join(root, "wizard")
		cmd := exec.Command("bash", filepath.Join(repo, "scripts/install.sh"), "--wizard", "--no-service", "--binary", binary, "--root", wizardRoot)
		cmd.Stdin = strings.NewReader("1\n" + old + "\nmanual\ny\n")
		if b, e := cmd.CombinedOutput(); e != nil {
			t.Fatalf("migration wizard: %v\n%s", e, b)
		}
		for _, name := range []string{"config.json", "data/alias.json", "migration-report.json"} {
			if _, e := os.Stat(filepath.Join(wizardRoot, name)); e != nil {
				t.Fatal(e)
			}
		}
		run("--check", "--root", wizardRoot)
	}
	dest := filepath.Join(root, "new")
	run("--migrate", old, "--from", "mibot-lite", "--root", dest)
	baselineCheck := run("--check", "--root", dest)
	bootstrap, _ := os.ReadFile(binary)
	archive := filepath.Join(root, "backup.tgz")
	run("--backup", archive, "--root", dest)
	restored := filepath.Join(root, "restored")
	run("--restore", archive, "--root", restored)
	run("--check", "--root", restored)
	if runtime.GOOS != "windows" {
		run("--plugin", "install-local", "--plugin-source", filepath.Join(repo, "examples/plugins/echo"), "--root", dest)
		if b := run("--plugin", "list", "--root", dest); !strings.Contains(b, "echo 2.0.0") {
			t.Fatal(b)
		}
		run("--check", "--root", dest)
		run("--check-plugins")
		before, err := os.ReadFile(binary)
		if err != nil {
			t.Fatal(err)
		}
		bad := filepath.Join(root, "bad-plugin")
		os.MkdirAll(bad, 0700)
		os.WriteFile(filepath.Join(bad, "manifest.json"), []byte(`{"name":"bad","version":"1","protocol_version":2,"package":".","commands":["ping"]}`), 0600)
		os.WriteFile(filepath.Join(bad, "bad.go"), []byte(`package bad
import("context";api "github.com/OrionG-hub/laowangbot/pkg/pluginapi")
type P struct{}
func Open(context.Context,api.Host,string)(api.Plugin,error){return P{},nil}
func(P)Close(){}
func(P)Handle(context.Context,api.Request)api.Response{return api.Response{Version:1}}
`), 0600)
		if out, err := exec.Command(binary, "--plugin", "install-local", "--plugin-source", bad, "--root", dest).CombinedOutput(); err == nil || !strings.Contains(string(out), "conflicting command") {
			t.Fatalf("collision accepted or wrong failure: %v %s", err, out)
		}
		after, _ := os.ReadFile(binary)
		if !bytes.Equal(before, after) {
			t.Fatal("failed build replaced binary")
		}
		if _, err := os.Stat(filepath.Join(dest, "plugins/bad")); !os.IsNotExist(err) {
			t.Fatal("failed build installed source")
		}
		run("--plugin", "replace-local", "--plugin-source", filepath.Join(repo, "examples/plugins/echo"), "--root", dest)
		run("--source-rollback", "--root", dest)
		run("--check-plugins")
		// A crash after candidate replacement must reload the recovered old image.
		pending := filepath.Join(dest, ".compiled", "pending")
		os.MkdirAll(pending, 0700)
		os.WriteFile(filepath.Join(pending, "binary"), bootstrap, 0700)
		recoveredCheck := run("--check", "--root", dest)
		count := regexp.MustCompile(`commands[=:]([0-9]+)`)
		if count.FindString(baselineCheck) == "" || count.FindString(baselineCheck) != count.FindString(recoveredCheck) {
			t.Fatalf("recovery kept wrong loaded registry: baseline=%s recovered=%s", baselineCheck, recoveredCheck)
		}
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
