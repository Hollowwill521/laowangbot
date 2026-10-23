package backup

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCreateContainsOnlyPluginConfiguration(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "config.json"), `{"api_id":1}`)
	mustWrite(t, filepath.Join(root, "gotd-session.json"), `{"session":true}`)
	mustWrite(t, filepath.Join(root, ".env"), "PREFIX=.")
	mustWrite(t, filepath.Join(root, "data", "alias.json"), `{"aliases":{}}`)
	mustWrite(t, filepath.Join(root, "state", "monitor", "config.json"), `{"enabled":true}`)

	archive, names, err := Create(root, "test", time.Unix(0, 0))
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 2 || names[0] != "data/alias.json" || names[1] != "state/monitor/config.json" {
		t.Fatalf("unexpected files: %#v", names)
	}
	if _, err := Restore(bytes.NewReader(archive), t.TempDir(), false); err != nil {
		t.Fatal(err)
	}
}

func TestRestorePreservesAccountFiles(t *testing.T) {
	source := t.TempDir()
	mustWrite(t, filepath.Join(source, "config.json"), `{"api_id":1}`)
	mustWrite(t, filepath.Join(source, "gotd-session.json"), `{"session":"keep"}`)
	mustWrite(t, filepath.Join(source, ".env"), "PREFIX=.")
	mustWrite(t, filepath.Join(source, "data", "alias.json"), `{"aliases":{"old":"x"}}`)
	archive, _, err := Create(source, "test", time.Now())
	if err != nil {
		t.Fatal(err)
	}

	target := t.TempDir()
	mustWrite(t, filepath.Join(target, "config.json"), `{"api_id":2}`)
	mustWrite(t, filepath.Join(target, "gotd-session.json"), `{"session":"keep"}`)
	mustWrite(t, filepath.Join(target, ".env"), "PREFIX=!")
	mustWrite(t, filepath.Join(target, "data", "alias.json"), `{"aliases":{"new":"y"}}`)
	if _, err := Restore(bytes.NewReader(archive), target, false); err != nil {
		t.Fatal(err)
	}
	for _, file := range []string{"config.json", "gotd-session.json", ".env"} {
		if _, err := os.Stat(filepath.Join(target, file)); err != nil {
			t.Fatalf("account file %s missing: %v", file, err)
		}
	}
	got, err := os.ReadFile(filepath.Join(target, "gotd-session.json"))
	if err != nil || string(got) != `{"session":"keep"}` {
		t.Fatalf("session changed: %q, %v", got, err)
	}
}

func TestRestorePluginsRejectsAccountArchive(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "config.json"), `{"api_id":1,"api_hash":"hash"}`)
	mustWrite(t, filepath.Join(root, "data", "alias.json"), `{"aliases":{}}`)
	var buf bytes.Buffer
	z := gzip.NewWriter(&buf)
	tw := tar.NewWriter(z)
	manifestBody, _ := json.Marshal(manifest{Format: format, Files: []string{"config.json", "data/alias.json"}})
	for _, entry := range []struct{ name, body string }{{Manifest, string(manifestBody)}, {"config.json", `{"api_id":1,"api_hash":"hash"}`}, {"data/alias.json", `{"aliases":{}}`}} {
		if err := tw.WriteHeader(&tar.Header{Name: entry.name, Mode: 0o600, Size: int64(len(entry.body)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(entry.body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	archive := buf.Bytes()
	if _, err := RestorePlugins(bytes.NewReader(archive), t.TempDir()); err == nil {
		t.Fatal("RestorePlugins accepted account archive")
	}
}

func mustWrite(t *testing.T, name, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(name), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}
