package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func put(t *testing.T, root, path, body string) {
	t.Helper()
	p := filepath.Join(root, path)
	if e := os.MkdirAll(filepath.Dir(p), 0755); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(p, []byte(body), 0644); e != nil {
		t.Fatal(e)
	}
}
func fixture(t *testing.T) string {
	t.Helper()
	r := t.TempDir()
	put(t, r, "examples/plugins/echo/manifest.json", `{"name":"echo","protocol_version":2,"package":"."}`)
	put(t, r, "examples/plugins/echo/echo.go", "package echo\n")
	if e := os.MkdirAll(filepath.Join(r, "plugins"), 0755); e != nil {
		t.Fatal(e)
	}
	return r
}
func TestCatalogSourceHashesAndReleasePaths(t *testing.T) {
	r := fixture(t)
	put(t, r, "examples/plugins/echo/assets/a b.txt", "embedded bytes")
	put(t, r, "plugins/legacy/manifest.json", `{"name":"legacy","protocol_version":1,"executable":"plugin"}`)
	c, e := generate(r, "v1.2.3")
	if e != nil {
		t.Fatal(e)
	}
	if len(c.Plugins) != 1 || len(c.Plugins[0].Files) != 2 {
		t.Fatalf("%+v", c)
	}
	for _, f := range c.Plugins[0].Files {
		b, e := os.ReadFile(filepath.Join(r, "examples/plugins/echo", f.Path))
		if e != nil {
			t.Fatal(e)
		}
		sum := sha256.Sum256(b)
		if f.SHA256 != hex.EncodeToString(sum[:]) {
			t.Fatal("hash mismatch")
		}
		if !strings.Contains(f.URL, "/v1.2.3/examples/plugins/echo/") || strings.Contains(f.URL, " ") {
			t.Fatal(f.URL)
		}
	}
	output := filepath.Join(r, "dist", "catalog.json")
	if e := writeCatalog(output, c); e != nil {
		t.Fatal(e)
	}
	b, e := os.ReadFile(output)
	if e != nil || !json.Valid(b) {
		t.Fatal(string(b), e)
	}
	put(t, r, "examples/plugins/echo/echo.go", "package changed\n")
	changed, e := generate(r, "v1.2.3")
	if e != nil {
		t.Fatal(e)
	}
	if changed.Plugins[0].Files[1].SHA256 == c.Plugins[0].Files[1].SHA256 {
		t.Fatal("source modification not reflected")
	}
}
func TestCatalogRejectsUnsupportedPackages(t *testing.T) {
	for _, tc := range []struct{ name, path, body string }{
		{"nested module", "examples/plugins/echo/sub/go.mod", "module forbidden"},
		{"workspace", "examples/plugins/echo/go.work", "go 1.25"},
		{"hidden data", "examples/plugins/echo/.secret", "do not publish"},
		{"legacy field", "examples/plugins/echo/manifest.json", `{"name":"echo","protocol_version":2,"package":".","persistent":false}`},
		{"bad package", "examples/plugins/echo/manifest.json", `{"name":"echo","protocol_version":2,"package":"../escape"}`},
		{"invalid manifest", "examples/plugins/echo/manifest.json", "{"},
		{"duplicate", "plugins/echo/manifest.json", `{"name":"echo","protocol_version":2,"package":"."}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := fixture(t)
			put(t, r, tc.path, tc.body)
			if tc.name == "duplicate" {
				put(t, r, "plugins/echo/echo.go", "package echo")
			}
			if _, e := generate(r, "v1.0.0"); e == nil {
				t.Fatal("accepted invalid package")
			}
		})
	}
	r := fixture(t)
	if _, e := generate(r, "master"); e == nil {
		t.Fatal("accepted unpinned branch")
	}
	if e := os.Remove(filepath.Join(r, "examples/plugins/echo/echo.go")); e != nil {
		t.Fatal(e)
	}
	if _, e := generate(r, "v1.0.0"); e == nil {
		t.Fatal("accepted empty source")
	}
}
func TestCatalogRejectsSymlink(t *testing.T) {
	r := fixture(t)
	if e := os.Symlink("echo.go", filepath.Join(r, "examples/plugins/echo/link.go")); e != nil {
		t.Skip(e)
	}
	if _, e := generate(r, "v1.0.0"); e == nil {
		t.Fatal("accepted symlink")
	}
}
