package plugin

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCatalogSelectsPlatformArtifact(t *testing.T) {
	e := CatalogEntry{Manifest: Manifest{Name: "demo", Version: "1", ProtocolVersion: 1, Executable: "plugin"}, Platforms: map[string]Platform{"windows/arm64": {Executable: "plugin.exe", Files: []RemoteFile{{Path: "plugin.exe"}}}}}
	p, err := e.ForPlatform("windows", "arm64")
	if err != nil || p.Manifest.Executable != "plugin.exe" || len(p.Files) != 1 {
		t.Fatal(p, err)
	}
	if _, err = e.ForPlatform("linux", "amd64"); err == nil {
		t.Fatal("accepted unavailable platform")
	}
}
func TestReleaseArtifactURLTrust(t *testing.T) {
	for _, u := range []string{"https://github.com/OrionG-hub/laowangbot/releases/download/v0.1.1/plugin-monitor-linux-amd64", "https://raw.githubusercontent.com/OrionG-hub/laowangbot/master/plugins/catalog.json"} {
		if !trustedURL(u) {
			t.Fatal(u)
		}
	}
	for _, u := range []string{"https://github.com/evil/repo/releases/download/x/file", "https://github.com/OrionG-hub/laowangbot/../../evil/x", "https://release-assets.githubusercontent.com/x"} {
		if trustedURL(u) {
			t.Fatal(u)
		}
	}
}

func TestCatalogPlatformValidation(t *testing.T) {
	catalog := `{"plugins":[{"manifest":{"name":"demo","version":"1.0.0","protocol_version":1,"executable":"plugin","capabilities":["self"]},"platforms":{"windows/amd64":{"executable":"plugin.exe","files":[{"path":"plugin.exe","url":"https://github.com/OrionG-hub/laowangbot/releases/download/v0.1.1/demo.exe","sha256":"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"}]}}}]}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(catalog)) }))
	defer server.Close()
	got, e := fetchCatalog(t.Context(), server.Client(), server.URL, false)
	if e != nil || len(got.Plugins) != 1 {
		t.Fatal(got, e)
	}
}
