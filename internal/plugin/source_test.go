package plugin

import (
	"os"
	"path/filepath"
	"testing"
)

// Catches rejection of portable Go source packages before they reach the compiler.
func TestSourcePackageInstallAndExport(t *testing.T) {
	d := t.TempDir()
	os.WriteFile(filepath.Join(d, "manifest.json"), []byte(`{"name":"source","version":"1","protocol_version":2,"package":".","commands":["hello"]}`), 0600)
	os.WriteFile(filepath.Join(d, "main.go"), []byte("package source\n"), 0600)
	m := Manager{Root: t.TempDir()}
	if err := m.InstallLocal(d); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Export("source"); err != nil {
		t.Fatal(err)
	}
}

func TestSourceManifestRejectsLegacyAndNestedModules(t *testing.T) {
	for _, tc := range []struct{ name, manifest, extra string }{
		{"legacy", `{"name":"old","version":"1","protocol_version":1,"executable":"main.go"}`, ""},
		{"nested", `{"name":"source","version":"1","protocol_version":2,"package":"."}`, "go.mod"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := t.TempDir()
			os.WriteFile(filepath.Join(d, "manifest.json"), []byte(tc.manifest), 0600)
			os.WriteFile(filepath.Join(d, "main.go"), []byte("package example\n"), 0600)
			if tc.extra != "" {
				os.MkdirAll(filepath.Join(d, "nested"), 0700)
				os.WriteFile(filepath.Join(d, "nested", tc.extra), []byte("module example"), 0600)
			}
			if _, err := SourceManifest(d); err == nil {
				t.Fatal("accepted incompatible package")
			}
		})
	}
}
