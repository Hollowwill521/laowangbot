package plugin

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestRemoteChecksumAndManualUpdate(t *testing.T) {
	data := []byte("#!/bin/sh\n")
	sum := sha256.Sum256(data)
	manifest := Manifest{Name: "remote", Version: "1", ProtocolVersion: 1, Executable: "run.sh"}
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/catalog" {
			json.NewEncoder(w).Encode(Catalog{Plugins: []CatalogEntry{{Manifest: manifest, Files: []RemoteFile{{Path: "run.sh", URL: server.URL + "/run", SHA256: hex.EncodeToString(sum[:])}}}}})
		} else {
			w.Write(data)
		}
	}))
	defer server.Close()
	m := Manager{Root: t.TempDir()}
	if e := m.installRemote(context.Background(), server.Client(), server.URL+"/catalog", "remote", false, false); e != nil {
		t.Fatal(e)
	}
	if _, e := m.Load("remote"); e != nil {
		t.Fatal(e)
	}
	data = []byte("corrupt")
	if e := m.installRemote(context.Background(), server.Client(), server.URL+"/catalog", "remote", true, false); e == nil {
		t.Fatal("accepted checksum mismatch")
	}
	b, _ := os.ReadFile(filepath.Join(m.Root, "plugins", "remote", "run.sh"))
	if string(b) != "#!/bin/sh\n" {
		t.Fatal("destroyed previous install")
	}
	local := Manager{Root: t.TempDir()}
	if e := local.InstallLocal(source(t, "#!/bin/sh\n")); e != nil {
		t.Fatal(e)
	}
	if e := local.UpdateRemote(context.Background(), "echo"); e == nil {
		t.Fatal("updated manual plugin")
	}
}
