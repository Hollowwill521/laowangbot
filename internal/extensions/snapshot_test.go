package extensions

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLegacyDiskPluginNeverRegistered(t *testing.T) {
	a := fixture(t, []string{"hello"})
	defer a.Close()
	marker := filepath.Join(a.Root, "executed")
	if err := os.WriteFile(filepath.Join(a.Root, "plugins/demo/run"), []byte("#!/bin/sh\ntouch '"+marker+"'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := Register(a); err != nil {
		t.Fatal(err)
	}
	if _, ok := a.Registry.Lookup("hello"); ok {
		t.Fatal("legacy disk executable registered")
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("legacy executable ran")
	}
}
