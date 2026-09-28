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

// Exercise a real build while enforcing the environment passed to the toolchain.
func TestBuildLimitsCompilerWithoutChangingHostEnvironment(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix wrapper; running binary replacement unsupported")
	}
	compiler, err := exec.LookPath("go")
	if err != nil {
		t.Fatal(err)
	}
	compiler, err = filepath.Abs(compiler)
	if err != nil {
		t.Fatal(err)
	}
	b := fixture(t)
	wrapper := filepath.Join(t.TempDir(), "go-wrapper")
	put(t, wrapper, `#!/bin/sh
set -eu
[ "$1" = build ] && [ "$2" = -p ] && [ "$3" = 1 ]
[ "$GOMAXPROCS" = 1 ] && [ "$GOMEMLIMIT" = 384MiB ] && [ "$GOGC" = 50 ]
[ "$GOMODCACHE" = "$EXPECTED_MODCACHE" ] && [ "$GOCACHE" = "$EXPECTED_BUILDCACHE" ]
exec "$REAL_GO" "$@"
`)
	t.Setenv("LAOWANGBOT_GO", wrapper)
	t.Setenv("REAL_GO", compiler)
	t.Setenv("GOMAXPROCS", "8")
	t.Setenv("GOMEMLIMIT", "off")
	t.Setenv("GOGC", "off")
	for _, pair := range [][2]string{{"GOMODCACHE", "EXPECTED_MODCACHE"}, {"GOCACHE", "EXPECTED_BUILDCACHE"}} {
		path := t.TempDir()
		t.Setenv(pair[0], path)
		t.Setenv(pair[1], path)
	}
	if err := b.Apply(t.Context(), nil); err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]string{"GOMAXPROCS": "8", "GOMEMLIMIT": "off", "GOGC": "off"} {
		if got := os.Getenv(key); got != want {
			t.Fatalf("host %s changed to %q", key, got)
		}
	}
}
