package sourceplugin

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/OrionG-hub/laowangbot/internal/compiled"
	"github.com/OrionG-hub/laowangbot/internal/platform"
	"github.com/OrionG-hub/laowangbot/internal/plugin"
)

func TestBinaryUpdateWithoutGoAndRollback(t *testing.T) {
	b := fixture(t)
	t.Setenv("PATH", t.TempDir())
	t.Setenv("LAOWANGBOT_GO", "/missing/go")
	put(t, filepath.Join(b.Root, "data/keep"), "data")
	err := b.InstallBinary(t.Context(), "v2.0.0", func(candidate string) error {
		put(t, candidate, "#!/bin/sh\nif [ \"$1\" = --version ]; then echo v2.0.0; fi\nexit 0\n")
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if required, err := NeedsSourceBuild(b.Root); err != nil || required {
		t.Fatalf("binary updates must remain Go-free: %v %v", required, err)
	}
	if err := b.Rollback(t.Context()); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(b.Binary)
	if string(data) != "original" {
		t.Fatal("rollback lost original")
	}
	for _, file := range []string{"data/keep", "state/p/value"} {
		if _, err := os.Stat(filepath.Join(b.Root, file)); err != nil {
			t.Fatal(err)
		}
	}
}

func TestBinaryCandidateFailuresPreserveDeploymentAndPrevious(t *testing.T) {
	for _, mode := range []string{"download", "version", "plugins", "config", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			b := fixture(t)
			put(t, filepath.Join(b.Root, ".compiled/previous/binary"), "previous")
			put(t, filepath.Join(b.Root, "config.json"), "{}")
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			err := b.InstallBinary(ctx, "v2.0.0", func(candidate string) error {
				if mode == "download" {
					return errors.New("download failed")
				}
				version := "v2.0.0"
				if mode == "version" {
					version = "v1.0.0"
				}
				script := "#!/bin/sh\ncase \"$1\" in\n--version) echo " + version + ";;\n"
				if mode == "plugins" {
					script += "--check-plugins) exit 1;;\n"
				}
				if mode == "config" {
					script += "--check) exit 1;;\n"
				}
				put(t, candidate, script+"esac\nexit 0\n")
				if mode == "cancel" {
					cancel()
				}
				return nil
			})
			if err == nil {
				t.Fatal("accepted failing candidate")
			}
			for file, want := range map[string]string{b.Binary: "original", filepath.Join(b.Root, ".compiled/previous/binary"): "previous"} {
				data, _ := os.ReadFile(file)
				if string(data) != want {
					t.Fatalf("modified %s", file)
				}
			}
		})
	}
}

func TestBinaryUpdateProtectsSourcesAndLock(t *testing.T) {
	for _, file := range []string{"plugins/p/manifest.json", "plugins/.manual"} {
		t.Run(file, func(t *testing.T) {
			b := fixture(t)
			put(t, filepath.Join(b.Root, file), "preserve")
			if required, err := NeedsSourceBuild(b.Root); err != nil || !required {
				t.Fatalf("%v %v", required, err)
			}
			called := false
			if err := b.InstallBinary(t.Context(), "v2.0.0", func(string) error { called = true; return nil }); err == nil || called {
				t.Fatal("official binary could erase sources")
			}
		})
	}
	b := fixture(t)
	os.MkdirAll(filepath.Join(b.Root, ".compiled"), 0700)
	lock, err := platform.LockRoot(filepath.Join(b.Root, ".compiled"))
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	called := false
	if err := b.InstallBinary(t.Context(), "v2.0.0", func(string) error { called = true; return nil }); err == nil || called {
		t.Fatal("shared build lock bypassed")
	}
}

func TestBinaryDetectionFailsClosed(t *testing.T) {
	root := t.TempDir()
	put(t, filepath.Join(root, "plugins"), "not a directory")
	if _, err := NeedsSourceBuild(root); err == nil {
		t.Fatal("ignored unreadable plugin directory")
	}
}

func TestBinaryDetectionProtectsLinkedPlugin(t *testing.T) {
	if os.Getenv("LAOWANGBOT_TEST_LINKED_PLUGIN") == "1" {
		compiled.Register(plugin.Manifest{Name: "linked", ProtocolVersion: 2}, nil)
		if required, err := NeedsSourceBuild(t.TempDir()); err != nil || !required {
			t.Fatal("linked plugin ignored")
		}
		return
	}
	if runtime.GOOS == "windows" {
		t.Skip("binary update unsupported")
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(exe, "-test.run=^TestBinaryDetectionProtectsLinkedPlugin$")
	cmd.Env = append(os.Environ(), "LAOWANGBOT_TEST_LINKED_PLUGIN=1")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s: %v", output, err)
	}
}

func TestEmptySourceDeploymentCanReturnToOfficialBinary(t *testing.T) {
	b := fixture(t)
	put(t, filepath.Join(b.Root, ".compiled/current.json"), `{"version":"v1.0.0"}`)
	put(t, filepath.Join(b.Root, ".compiled/source/go.mod"), "old host source")
	if required, err := NeedsSourceBuild(b.Root); err != nil || required {
		t.Fatalf("%v %v", required, err)
	}
	if err := b.InstallBinary(t.Context(), "v2.0.0", func(candidate string) error {
		put(t, candidate, "#!/bin/sh\nif [ \"$1\" = --version ]; then echo v2.0.0; fi\nexit 0\n")
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"current.json", "source"} {
		if _, err := os.Stat(filepath.Join(b.Root, ".compiled", name)); !os.IsNotExist(err) {
			t.Fatal("retained stale source", name)
		}
	}
	if err := b.Rollback(t.Context()); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(filepath.Join(b.Root, ".compiled/source/go.mod")); string(data) != "old host source" {
		t.Fatal("lost source on rollback")
	}
}
