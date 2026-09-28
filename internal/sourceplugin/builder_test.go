package sourceplugin

import (
	"context"
	"github.com/OrionG-hub/laowangbot/internal/platform"
	"github.com/OrionG-hub/laowangbot/internal/plugin"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

func put(t *testing.T, p, s string) {
	t.Helper()
	if e := os.MkdirAll(filepath.Dir(p), 0700); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(p, []byte(s), 0700); e != nil {
		t.Fatal(e)
	}
}
func fixture(t *testing.T) Builder {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("Windows running-executable replacement is explicitly unsupported")
	}
	root := t.TempDir()
	src := t.TempDir()
	put(t, filepath.Join(src, "go.mod"), "module github.com/OrionG-hub/laowangbot\n\ngo 1.26.0\n")
	put(t, filepath.Join(src, "internal/plugin/p.go"), `package plugin; type Manifest struct { Name string }`)
	put(t, filepath.Join(src, "internal/compiled/r.go"), `package compiled; import "github.com/OrionG-hub/laowangbot/internal/plugin"; func Register(m plugin.Manifest,f func()) {f()}`)
	put(t, filepath.Join(src, "cmd/laowangbot/main.go"), `package main; import "os"; var version string;func main(){if len(os.Args)>1&&os.Args[1]=="--check-plugins" {return};os.Exit(1)}`)
	put(t, filepath.Join(root, "bot"), "original")
	put(t, filepath.Join(root, "state/p/value"), "keep")
	return Builder{Root: root, Binary: filepath.Join(root, "bot"), Source: src, Version: "v1.0.0"}
}
func install(t *testing.T, m plugin.Manager, code string) error {
	t.Helper()
	dir := filepath.Join(m.Root, "plugins/p")
	put(t, filepath.Join(dir, "manifest.json"), `{"name":"p","version":"1.0.0","protocol_version":2,"package":"."}`)
	put(t, filepath.Join(dir, "p.go"), code)
	return nil
}
func TestApplyFailureAndRollback(t *testing.T) {
	b := fixture(t)
	ctx := context.Background()
	if e := b.Apply(ctx, func(m plugin.Manager) error { return install(t, m, "package p; func Open() {}") }); e != nil {
		t.Fatal(e)
	}
	before, _ := os.ReadFile(b.Binary)
	if e := b.Apply(ctx, func(m plugin.Manager) error { return install(t, m, "package p; broken") }); e == nil {
		t.Fatal("expected compile failure")
	}
	after, _ := os.ReadFile(b.Binary)
	if string(before) != string(after) {
		t.Fatal("binary changed on failure")
	}
	code, _ := os.ReadFile(filepath.Join(b.Root, "plugins/p/p.go"))
	if string(code) != "package p; func Open() {}" {
		t.Fatal("source changed")
	}
	if e := b.Rollback(ctx); e != nil {
		t.Fatal(e)
	}
	old, _ := os.ReadFile(b.Binary)
	if string(old) != "original" {
		t.Fatal("rollback binary")
	}
	if _, e := os.Stat(filepath.Join(b.Root, "plugins/p")); !os.IsNotExist(e) {
		t.Fatal("rollback sources")
	}
	state, _ := os.ReadFile(filepath.Join(b.Root, "state/p/value"))
	if string(state) != "keep" {
		t.Fatal("state changed")
	}
}

func TestRelativeDeploymentPaths(t *testing.T) {
	for _, rootName := range []string{".", "deployment"} {
		t.Run(rootName, func(t *testing.T) {
			b := fixture(t)
			cwd := b.Root
			if rootName != "." {
				cwd = t.TempDir()
				if err := os.Rename(b.Root, filepath.Join(cwd, rootName)); err != nil {
					t.Fatal(err)
				}
			}
			var err error
			b.Source, err = filepath.Rel(cwd, b.Source)
			if err != nil {
				t.Fatal(err)
			}
			t.Chdir(cwd)
			b.Root = rootName
			b.Binary = filepath.Join(rootName, "bot")
			if err = b.Apply(context.Background(), func(m plugin.Manager) error {
				return install(t, m, "package p; func Open() {}")
			}); err != nil {
				t.Fatal(err)
			}
			if _, err = os.Stat(filepath.Join(b.Root, "plugins/p/p.go")); err != nil {
				t.Fatal(err)
			}
			if err = b.Rollback(context.Background()); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(b.Binary)
			if err != nil || string(data) != "original" {
				t.Fatalf("rollback binary: %q %v", data, err)
			}
		})
	}
}
func TestBuildLock(t *testing.T) {
	b := fixture(t)
	os.MkdirAll(filepath.Join(b.Root, ".compiled"), 0700)
	l, e := platform.LockRoot(filepath.Join(b.Root, ".compiled"))
	if e != nil {
		t.Fatal(e)
	}
	defer l.Close()
	called := false
	if e = b.Apply(context.Background(), func(plugin.Manager) error { called = true; return nil }); e == nil || called {
		t.Fatal("lock not enforced")
	}
	if e = b.Update(context.Background(), "v1.0.0"); e == nil {
		t.Fatal("update lock not enforced")
	}
}
func TestPreflight(t *testing.T) {
	b := fixture(t)
	t.Setenv("PATH", t.TempDir())
	called := false
	if e := b.Apply(context.Background(), func(plugin.Manager) error { called = true; return nil }); e == nil || called {
		t.Fatal("missing go preflight")
	}
}
func TestSavedSourceAndRecovery(t *testing.T) {
	b := fixture(t)
	ctx := context.Background()
	if err := b.Apply(ctx, nil); err != nil {
		t.Fatal(err)
	}
	put(t, filepath.Join(b.Source, "cmd/laowangbot/main.go"), "bad source")
	if err := b.Apply(ctx, nil); err != nil {
		t.Fatalf("saved source not used: %v", err)
	}
	pending := filepath.Join(b.Root, ".compiled/pending")
	if err := b.snapshot(pending); err != nil {
		t.Fatal(err)
	}
	put(t, b.Binary, "interrupted")
	if err := b.recover(); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(b.Binary)
	if string(data) == "interrupted" {
		t.Fatal("journal not recovered")
	}
}
func TestSourceExcludesDeploymentSecrets(t *testing.T) {
	b := fixture(t)
	for _, p := range []string{"config.json", ".env", "session.json", "session/session", "state/value", "data/value", "dist/bot"} {
		put(t, filepath.Join(b.Source, p), "secret")
	}
	if err := b.Apply(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"config.json", ".env", "session.json", "session/session", "state/value", "data/value", "dist/bot"} {
		if _, err := os.Stat(filepath.Join(b.Root, ".compiled/source", p)); !os.IsNotExist(err) {
			t.Errorf("copied %s", p)
		}
	}
}
func TestCandidateCheckFailure(t *testing.T) {
	b := fixture(t)
	put(t, filepath.Join(b.Source, "cmd/laowangbot/main.go"), `package main; import "os";func main(){os.Exit(7)}`)
	if err := b.Apply(context.Background(), nil); err == nil {
		t.Fatal("check failure accepted")
	}
	data, _ := os.ReadFile(b.Binary)
	if string(data) != "original" {
		t.Fatal("binary replaced")
	}
}

func TestUpdateTaggedSourcePreservesPlugins(t *testing.T) {
	b := fixture(t)
	ctx := context.Background()
	if err := b.Apply(ctx, func(m plugin.Manager) error { return install(t, m, "package p; func Open() {}") }); err != nil {
		t.Fatal(err)
	}
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git unavailable")
	}
	for _, args := range [][]string{{"init"}, {"add", "."}, {"-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "commit", "-m", "fixture"}, {"tag", "v2.0.0"}} {
		cmd := exec.Command(git, args...)
		cmd.Dir = b.Source
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %s: %v", args, out, err)
		}
	}
	t.Setenv("GIT_CONFIG_COUNT", "1")
	t.Setenv("GIT_CONFIG_KEY_0", "url.file://"+b.Source+".insteadOf")
	t.Setenv("GIT_CONFIG_VALUE_0", "https://github.com/test/repo.git")
	b.Repo = "test/repo"
	if err = b.Update(ctx, "v2.0.0"); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(b.Root, "plugins/p/p.go"))
	if string(data) != "package p; func Open() {}" {
		t.Fatal("update changed local plugin")
	}
	data, _ = os.ReadFile(filepath.Join(b.Root, ".compiled/current.json"))
	if string(data) != `{"version":"v2.0.0"}` {
		t.Fatalf("version: %s", data)
	}
	if err = b.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(filepath.Join(b.Root, ".compiled/current.json"))
	if string(data) != `{"version":"v1.0.0"}` {
		t.Fatalf("rollback version: %s", data)
	}
}
func TestLockRejectsSymlink(t *testing.T) {
	b := fixture(t)
	if err := os.Symlink(t.TempDir(), filepath.Join(b.Root, ".compiled")); err != nil {
		t.Fatal(err)
	}
	if err := b.Apply(context.Background(), nil); err == nil {
		t.Fatal("symlink accepted")
	}
}
func TestSourcePreservesHostPackages(t *testing.T) {
	b := fixture(t)
	for _, p := range []string{"internal/session/session.go", "internal/store/data/config.json", "pkg/state/value.go"} {
		put(t, filepath.Join(b.Source, p), "source")
	}
	dest := t.TempDir()
	if err := copyTree(b.Source, dest, true); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"internal/session/session.go", "internal/store/data/config.json", "pkg/state/value.go"} {
		if _, err := os.Stat(filepath.Join(dest, p)); err != nil {
			t.Errorf("required host source removed: %s: %v", p, err)
		}
	}
}
func TestLegacyMigration(t *testing.T) {
	b := fixture(t)
	legacy := func(root, name string) {
		put(t, filepath.Join(root, "plugins", name, "manifest.json"), `{"name":"`+name+`","version":"1.0.0","protocol_version":1,"executable":"run"}`)
		put(t, filepath.Join(root, "plugins", name, "run"), "legacy")
	}
	legacy(b.Root, "oldone")
	legacy(b.Root, "oldtwo")
	ctx := context.Background()
	if err := b.Apply(ctx, func(m plugin.Manager) error { return m.Remove("oldone") }); err != nil {
		t.Fatal(err)
	}
	if err := b.Apply(ctx, func(m plugin.Manager) error { return install(t, m, "package p; func Open() {}") }); err != nil {
		t.Fatal(err)
	}
	if err := b.Apply(ctx, func(m plugin.Manager) error { legacy(m.Root, "newold"); return nil }); err == nil {
		t.Fatal("new legacy plugin accepted")
	}
	if err := b.Apply(ctx, func(m plugin.Manager) error {
		put(t, filepath.Join(m.Root, "plugins/oldtwo/run"), "changed")
		return nil
	}); err == nil {
		t.Fatal("legacy modification accepted")
	}
}

func TestWindowsMutationsExplainUnsupportedPlatform(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows-only contract")
	}
	b := Builder{Root: t.TempDir(), Binary: "bot.exe"}
	if _, err := b.lock(); err == nil {
		t.Fatal("Windows replacement accepted")
	}
	if err := b.Recover(); err != nil {
		t.Fatal("normal Windows startup should work", err)
	}
}
func TestPreviousSymlinkDoesNotOverwriteTarget(t *testing.T) {
	b := fixture(t)
	sentinel := filepath.Join(t.TempDir(), "sentinel")
	put(t, sentinel, "preserve")
	if err := os.Symlink(sentinel, b.Binary+".previous"); err != nil {
		t.Fatal(err)
	}
	if err := b.Apply(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(sentinel)
	if err != nil || string(data) != "preserve" {
		t.Fatalf("symlink target changed: %q %v", data, err)
	}
	info, err := os.Lstat(b.Binary + ".previous")
	if err != nil || !info.Mode().IsRegular() {
		t.Fatalf("backup not regular: %v", err)
	}
}
func TestDeploymentConfigValidationFailurePreservesBinary(t *testing.T) {
	b := fixture(t)
	put(t, filepath.Join(b.Root, "config.json"), "{}")
	put(t, filepath.Join(b.Source, "cmd/laowangbot/main.go"), `package main;import "os";func main(){if len(os.Args)>1&&os.Args[1]=="--check-plugins"{return};os.Exit(8)}`)
	if err := b.Apply(context.Background(), nil); err == nil {
		t.Fatal("deployment validation failure accepted")
	}
	data, _ := os.ReadFile(b.Binary)
	if string(data) != "original" {
		t.Fatal("binary replaced before configuration validation")
	}
}
