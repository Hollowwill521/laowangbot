package sourceplugin

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestExplicitGoExecutable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix executable fixture")
	}
	dir := t.TempDir()
	file := filepath.Join(dir, "go")
	os.WriteFile(file, []byte("#!/bin/sh\nexit 0\n"), 0700)
	t.Setenv("LAOWANGBOT_GO", file)
	t.Setenv("PATH", t.TempDir())
	got, e := findGo()
	if e != nil || got != file {
		t.Fatal(got, e)
	}
}
func TestInvalidExplicitGoHasActionableError(t *testing.T) {
	t.Setenv("LAOWANGBOT_GO", filepath.Join(t.TempDir(), "missing"))
	if _, e := findGo(); e == nil || !strings.Contains(e.Error(), "LAOWANGBOT_GO") {
		t.Fatal(e)
	}
}

func TestBuildUsesExplicitGoOutsideServicePATH(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("running binary replacement unsupported")
	}
	compiler, e := exec.LookPath("go")
	if e != nil {
		t.Fatal(e)
	}
	compiler, e = filepath.Abs(compiler)
	if e != nil {
		t.Fatal(e)
	}
	b := fixture(t)
	t.Setenv("LAOWANGBOT_GO", compiler)
	t.Setenv("PATH", "/usr/bin:/bin")
	if e = b.Apply(t.Context(), nil); e != nil {
		t.Fatal(e)
	}
}
