package plugin

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
)

func remoteFixture(t *testing.T) (Manager, *httptest.Server) {
	t.Helper()
	data := []byte("native plugin fixture")
	sum := sha256.Sum256(data)
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/catalog" {
			_ = json.NewEncoder(w).Encode(Catalog{Plugins: []CatalogEntry{{Manifest: Manifest{Name: "remote", Version: "1", ProtocolVersion: 1, Executable: "run"}, Files: []RemoteFile{{Path: "run", URL: server.URL + "/run", SHA256: hex.EncodeToString(sum[:])}}}}})
		} else {
			_, _ = w.Write(data)
		}
	}))
	t.Cleanup(server.Close)
	m := Manager{Root: t.TempDir()}
	if err := m.installRemote(context.Background(), server.Client(), server.URL+"/catalog", "remote", false, false); err != nil {
		t.Fatal(err)
	}
	return m, server
}

func TestRemoteUpdatePreservesLocalEdits(t *testing.T) {
	for _, kind := range []string{"changed", "added", "deleted", "manifest", "legacy"} {
		t.Run(kind, func(t *testing.T) {
			m, s := remoteFixture(t)
			dir := filepath.Join(m.Root, "plugins", "remote")
			switch kind {
			case "changed":
				_ = os.WriteFile(filepath.Join(dir, "run"), []byte("local changes"), 0700)
			case "added":
				_ = os.WriteFile(filepath.Join(dir, "extra"), []byte("local addition"), 0600)
			case "deleted":
				_ = os.Remove(filepath.Join(dir, "run"))
			case "manifest":
				b, _ := os.ReadFile(filepath.Join(dir, "manifest.json"))
				var v map[string]any
				_ = json.Unmarshal(b, &v)
				v["version"] = "local"
				b, _ = json.Marshal(v)
				_ = os.WriteFile(filepath.Join(dir, "manifest.json"), b, 0600)
			case "legacy":
				_ = os.Remove(filepath.Join(dir, ".catalog-hashes"))
			}
			if err := m.installRemote(context.Background(), s.Client(), s.URL+"/catalog", "remote", true, false); err == nil {
				t.Fatal("remote update overwrote local modification or missing baseline")
			}
		})
	}
}

func TestPackageExportImportRemovePreservesStateAndSnapshot(t *testing.T) {
	m, server := remoteFixture(t)
	state := filepath.Join(m.Root, "state", "remote")
	if err := os.MkdirAll(state, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(state, "value"), []byte("saved"), 0600); err != nil {
		t.Fatal(err)
	}
	snapshot, cleanup, err := m.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	items, err := m.Installed()
	if err != nil || len(items) != 1 || items[0].Source != server.URL+"/catalog" || items[0].Modified {
		t.Fatalf("installed: %+v %v", items, err)
	}
	data, err := m.Export("remote")
	if err != nil {
		t.Fatal(err)
	}
	target := Manager{Root: t.TempDir()}
	name, err := target.ImportPackage(data, false)
	if err != nil || name != "remote" {
		t.Fatalf("import %q %v", name, err)
	}
	items, err = target.Installed()
	if err != nil || len(items) != 1 || items[0].Source != "manual" {
		t.Fatalf("import ownership: %+v %v", items, err)
	}
	if _, err = target.ImportPackage(data, false); err == nil {
		t.Fatal("duplicate import replaced plugin")
	}
	if _, err = target.ImportPackage(data, true); err != nil {
		t.Fatal(err)
	}
	if err = m.Remove("remote"); err != nil {
		t.Fatal(err)
	}
	if _, err = m.Load("remote"); !os.IsNotExist(err) {
		t.Fatalf("not removed: %v", err)
	}
	if _, err = snapshot.Load("remote"); err != nil {
		t.Fatal("snapshot changed", err)
	}
	if b, err := os.ReadFile(filepath.Join(state, "value")); err != nil || string(b) != "saved" {
		t.Fatal("state removed")
	}
}

func TestPackageImportRejectsUnsafeEntries(t *testing.T) {
	for _, name := range []string{"../escape", "/escape", "a/../../escape", `a\escape`, ".catalog-source", ".catalog-hashes", "manifest.json"} {
		t.Run(name, func(t *testing.T) {
			var buf bytes.Buffer
			zw := zip.NewWriter(&buf)
			f, _ := zw.Create("manifest.json")
			_, _ = f.Write([]byte(`{"name":"demo","version":"1","protocol_version":1,"executable":"run"}`))
			f, _ = zw.Create("run")
			_, _ = f.Write([]byte("fixture"))
			f, _ = zw.Create(name)
			_, _ = f.Write([]byte("bad"))
			_ = zw.Close()
			m := Manager{Root: t.TempDir()}
			if _, err := m.ImportPackage(buf.Bytes(), false); err == nil {
				t.Fatal("unsafe ZIP accepted")
			}
		})
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	h := &zip.FileHeader{Name: "run"}
	h.SetMode(os.ModeSymlink | 0700)
	f, _ := zw.CreateHeader(h)
	_, _ = f.Write([]byte("/tmp/outside"))
	_ = zw.Close()
	if _, err := (Manager{Root: t.TempDir()}).ImportPackage(buf.Bytes(), false); err == nil {
		t.Fatal("symlink accepted")
	}
}

func TestSearchCatalogValidationAndFiltering(t *testing.T) {
	m, s := remoteFixture(t)
	for _, query := range []string{"REMOTE", "rem", ""} {
		got, err := m.search(context.Background(), s.Client(), s.URL+"/catalog", query, false)
		if err != nil || len(got) != 1 {
			t.Fatalf("search %q = %+v %v", query, got, err)
		}
	}
	got, err := m.search(context.Background(), s.Client(), s.URL+"/catalog", "absent", false)
	if err != nil || len(got) != 0 {
		t.Fatalf("search missing %+v %v", got, err)
	}
}

func TestForceRemoteUpdateAndManualRejection(t *testing.T) {
	m, s := remoteFixture(t)
	dir := filepath.Join(m.Root, "plugins", "remote")
	_ = os.WriteFile(filepath.Join(dir, "run"), []byte("local"), 0700)
	if err := m.installRemoteForce(context.Background(), s.Client(), s.URL+"/catalog", "remote", true, false, true); err != nil {
		t.Fatal(err)
	}
	if isModified(dir) {
		t.Fatal("forced update has wrong baseline")
	}
	_ = os.Remove(filepath.Join(dir, remoteMarker))
	if err := m.installRemoteForce(context.Background(), s.Client(), s.URL+"/catalog", "remote", true, false, true); err == nil {
		t.Fatal("force updated manual package")
	}
}

func TestCatalogRejectsInvalidEntriesAndSearchesMetadata(t *testing.T) {
	sum := sha256.Sum256([]byte("fixture"))
	entry := CatalogEntry{Manifest: Manifest{Name: "remote", Description: "Useful WEATHER", Version: "1", ProtocolVersion: 1, Executable: "run", Commands: []string{"forecast"}}, Files: []RemoteFile{{Path: "run", URL: "https://raw.githubusercontent.com/OrionG-hub/laowangbot/master/plugins/run", SHA256: hex.EncodeToString(sum[:])}}}
	cases := []struct {
		name   string
		modify func(*Catalog)
	}{
		{"duplicate", func(c *Catalog) { c.Plugins = append(c.Plugins, c.Plugins[0]) }},
		{"outside", func(c *Catalog) { c.Plugins[0].Files[0].URL = "https://example.com/run" }},
		{"reserved", func(c *Catalog) { c.Plugins[0].Files[0].Path = ".catalog-hashes" }},
		{"traversal", func(c *Catalog) { c.Plugins[0].Files[0].Path = "a/../run" }},
		{"bad hash", func(c *Catalog) { c.Plugins[0].Files[0].SHA256 = "bad" }},
		{"bad manifest", func(c *Catalog) { c.Plugins[0].Manifest.ProtocolVersion = 2 }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := Catalog{Plugins: []CatalogEntry{entry}}
			c.Plugins[0].Files = append([]RemoteFile(nil), entry.Files...)
			tc.modify(&c)
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _ = json.NewEncoder(w).Encode(c) }))
			defer server.Close()
			client := server.Client()
			client.Transport = catalogTransport{base: client.Transport, target: server.URL}
			if _, err := (Manager{}).search(context.Background(), client, CatalogURL, "", true); err == nil {
				t.Fatal("invalid catalog accepted")
			}
		})
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(Catalog{Plugins: []CatalogEntry{entry}})
	}))
	defer server.Close()
	for _, query := range []string{"weather", "FORECAST"} {
		got, err := (Manager{}).search(context.Background(), server.Client(), server.URL, query, false)
		if err != nil || len(got) != 1 {
			t.Fatalf("metadata search %q: %v %v", query, got, err)
		}
	}
}

type catalogTransport struct {
	base   http.RoundTripper
	target string
}

func (c catalogTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	target, _ := url.Parse(c.target)
	copy := r.Clone(r.Context())
	u := *r.URL
	copy.URL = &u
	copy.URL.Scheme = target.Scheme
	copy.URL.Host = target.Host
	return c.base.RoundTrip(copy)
}

func TestRemoteUpdateRechecksEditsBeforeReplacement(t *testing.T) {
	m, s := remoteFixture(t)
	dir := filepath.Join(m.Root, "plugins", "remote")
	client := s.Client()
	client.Transport = editTransport{base: client.Transport, edit: func() {
		if err := os.WriteFile(filepath.Join(dir, "run"), []byte("edited during download"), 0700); err != nil {
			t.Error(err)
		}
	}}
	if err := m.installRemote(context.Background(), client, s.URL+"/catalog", "remote", true, false); !errors.Is(err, ErrModified) {
		t.Fatalf("lost concurrent edit: %v", err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "run"))
	if err != nil || string(b) != "edited during download" {
		t.Fatal("concurrent edit destroyed")
	}
}

func TestPackageSizeAndFileLimits(t *testing.T) {
	for _, kind := range []string{"size", "count"} {
		t.Run(kind, func(t *testing.T) {
			var buf bytes.Buffer
			zw := zip.NewWriter(&buf)
			if kind == "size" {
				f, _ := zw.Create("huge")
				_, _ = io.CopyN(f, zeroReader{}, MaxPackageSize+1)
			} else {
				for i := 0; i < 257; i++ {
					_, _ = zw.Create(fmt.Sprintf("f%d", i))
				}
			}
			_ = zw.Close()
			if _, err := (Manager{Root: t.TempDir()}).ImportPackage(buf.Bytes(), false); err == nil {
				t.Fatal("oversized ZIP accepted")
			}
		})
	}
	m := Manager{Root: t.TempDir()}
	if err := m.InstallLocal(source(t, "fixture")); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(filepath.Join(m.Root, "plugins", "echo", "huge"))
	if err != nil {
		t.Fatal(err)
	}
	if err = f.Truncate(MaxPackageSize + 1); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	if _, err = m.Export("echo"); err == nil {
		t.Fatal("oversized export accepted")
	}
}

type zeroReader struct{}

func (zeroReader) Read(b []byte) (int, error) { clear(b); return len(b), nil }

func TestInstalledReportsDeletedPayload(t *testing.T) {
	m, _ := remoteFixture(t)
	_ = os.Remove(filepath.Join(m.Root, "plugins", "remote", "run"))
	entries, err := m.Installed()
	if err != nil || len(entries) != 1 || !entries[0].Modified {
		t.Fatalf("cannot inspect damaged plugin: %+v %v", entries, err)
	}
}

type editTransport struct {
	base http.RoundTripper
	edit func()
}

func (c editTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.URL.Path == "/run" {
		c.edit()
	}
	return c.base.RoundTrip(r)
}

func TestTrustedURLRejectsEscapingProject(t *testing.T) {
	for _, raw := range []string{
		"https://raw.githubusercontent.com/OrionG-hub/laowangbot/../other/master/run",
		"https://raw.githubusercontent.com/OrionG-hub/laowangbot/%2e%2e/other/master/run",
		"https://raw.githubusercontent.com/OrionG-hub/laowangbot/%5c..%5cother/run",
	} {
		if trustedURL(raw) {
			t.Errorf("trusted escaping URL %q", raw)
		}
	}
}

func TestRemoteUpdateRechecksOwnershipBeforeReplacement(t *testing.T) {
	m, s := remoteFixture(t)
	client := s.Client()
	client.Transport = editTransport{base: client.Transport, edit: func() {
		// Same-name manual replacement without network or package execution.
		dir := filepath.Join(m.Root, "plugins", "remote")
		if err := os.Remove(filepath.Join(dir, remoteMarker)); err != nil {
			t.Error(err)
		}
	}}
	if err := m.installRemoteForce(context.Background(), client, s.URL+"/catalog", "remote", true, false, true); err == nil {
		t.Fatal("force lost ownership race")
	}
}

func TestManifestWhitespaceDoesNotMarkRemoteModified(t *testing.T) {
	m, _ := remoteFixture(t)
	dir := filepath.Join(m.Root, "plugins", "remote")
	p := filepath.Join(dir, "manifest.json")
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err = json.Indent(&out, b, "", "  "); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(p, out.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	if isModified(dir) {
		t.Fatal("manifest whitespace reported as local change")
	}
}

func TestInstalledRetainsBrokenManifestEntries(t *testing.T) {
	for _, kind := range []string{"missing", "malformed", "invalid", "mismatched"} {
		t.Run(kind, func(t *testing.T) {
			m, server := remoteFixture(t)
			if err := m.InstallLocal(source(t, "fixture")); err != nil {
				t.Fatal(err)
			}
			manifest := filepath.Join(m.Root, "plugins", "remote", "manifest.json")
			switch kind {
			case "missing":
				if err := os.Remove(manifest); err != nil {
					t.Fatal(err)
				}
			case "malformed":
				if err := os.WriteFile(manifest, []byte("{"), 0600); err != nil {
					t.Fatal(err)
				}
			case "invalid":
				if err := os.WriteFile(manifest, []byte(`{"name":"remote","version":"1","protocol_version":2,"executable":"run"}`), 0600); err != nil {
					t.Fatal(err)
				}
			case "mismatched":
				if err := os.WriteFile(manifest, []byte(`{"name":"other","version":"1","protocol_version":1,"executable":"run"}`), 0600); err != nil {
					t.Fatal(err)
				}
			}
			entries, err := m.Installed()
			if err != nil || len(entries) != 2 {
				t.Fatalf("healthy/broken enumeration: %+v %v", entries, err)
			}
			if entries[0].Manifest.Name != "echo" || entries[0].Source != "manual" {
				t.Fatalf("healthy changed: %+v", entries[0])
			}
			broken := entries[1]
			if broken.Manifest.Name != "remote" || broken.Source != server.URL+"/catalog" || !broken.Modified || broken.Error == "" {
				t.Fatalf("broken ownership lost: %+v", broken)
			}
			// Export and execution loading retain strict validation.
			if _, err = m.Export("remote"); err == nil {
				t.Fatal("exported broken manifest")
			}
			if _, err = m.Load("remote"); err == nil {
				t.Fatal("loaded broken manifest")
			}
			if err = m.installRemoteForce(context.Background(), server.Client(), server.URL+"/catalog", "remote", true, false, true); err != nil {
				t.Fatal("forced repair failed", err)
			}
			if _, err = m.Load("remote"); err != nil {
				t.Fatal("repair not usable", err)
			}
			manual := filepath.Join(m.Root, "plugins", "echo", "manifest.json")
			if err = os.Remove(manual); err != nil {
				t.Fatal(err)
			}
			entries, err = m.Installed()
			if err != nil || len(entries) != 2 || entries[0].Source != "manual" || !entries[0].Modified || entries[0].Error == "" {
				t.Fatalf("broken manual lost: %+v %v", entries, err)
			}
			if err = m.Remove("echo"); err != nil {
				t.Fatal("cannot remove broken manual", err)
			}
		})
	}
}
