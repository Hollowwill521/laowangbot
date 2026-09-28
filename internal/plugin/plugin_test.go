package plugin

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func source(t *testing.T, script string) string {
	t.Helper()
	d := t.TempDir()
	os.WriteFile(filepath.Join(d, "manifest.json"), []byte(`{"name":"echo","version":"1","protocol_version":1,"executable":"run.sh","commands":["hello"]}`), 0600)
	os.WriteFile(filepath.Join(d, "run.sh"), []byte(script), 0700)
	return d
}
func TestInstallRun(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix shell/symlink fixture; native protocol is tested separately")
	}
	m := Manager{Root: t.TempDir()}
	if err := m.InstallLocal(source(t, "#!/bin/sh\nread line\nprintf '%s\\n' '{\"version\":1,\"text\":\"hello\"}'\n")); err != nil {
		t.Fatal(err)
	}
	r, e := m.Run(context.Background(), "echo", Request{Type: "command", Command: "hello"})
	if e != nil || r.Text != "hello" {
		t.Fatalf("%+v %v", r, e)
	}
	if _, e = os.Stat(filepath.Join(m.Root, "state", "echo")); e != nil {
		t.Fatal(e)
	}
}
func TestRejectEscape(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix shell/symlink fixture; native protocol is tested separately")
	}
	d := source(t, "#!/bin/sh\n")
	os.Remove(filepath.Join(d, "run.sh"))
	os.Symlink("/bin/sh", filepath.Join(d, "run.sh"))
	m := Manager{Root: t.TempDir()}
	if m.InstallLocal(d) == nil {
		t.Fatal("accepted symlink")
	}
}
func TestCancel(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix shell/symlink fixture; native protocol is tested separately")
	}
	m := Manager{Root: t.TempDir()}
	if e := m.InstallLocal(source(t, "#!/bin/sh\nread line\nwhile :; do :; done\n")); e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, e := m.Run(ctx, "echo", Request{Type: "command", Command: "hello"}); e == nil {
		t.Fatal("expected cancellation")
	}
}

func TestPersistentAndArgs(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix shell/symlink fixture; native protocol is tested separately")
	}
	d := source(t, "#!/bin/sh\nwhile IFS= read -r line; do printf '{\"version\":1,\"text\":\"%s\"}\\n' \"$1\"; done\n")
	os.WriteFile(filepath.Join(d, "manifest.json"), []byte(`{"name":"echo","version":"1","protocol_version":1,"executable":"run.sh","args":["hello world"],"events":["message"],"interval_seconds":1,"persistent":true}`), 0600)
	m := Manager{Root: t.TempDir()}
	if e := m.InstallLocal(d); e != nil {
		t.Fatal(e)
	}
	w, e := m.StartWorker(context.Background(), "echo")
	if e != nil {
		t.Fatal(e)
	}
	defer w.Close()
	for _, typ := range []string{"event", "tick"} {
		r, e := w.Call(context.Background(), Request{Type: typ})
		if e != nil || r.Text != "hello world" {
			t.Fatalf("%+v %v", r, e)
		}
	}
	list, e := m.List()
	if e != nil || len(list) != 1 {
		t.Fatalf("%v %v", list, e)
	}
}
func TestMalformedAndOversizedOutput(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix shell/symlink fixture; native protocol is tested separately")
	}
	for _, script := range []string{"#!/bin/sh\nread line\nprintf '%s\\n' '{\"version\":2}'\n", "#!/bin/sh\nread line\nhead -c 1100000 /dev/zero | tr '\\000' a\nprintf '\\n'\n"} {
		m := Manager{Root: t.TempDir()}
		if e := m.InstallLocal(source(t, script)); e != nil {
			t.Fatal(e)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		_, e := m.Run(ctx, "echo", Request{Type: "command", Command: "hello"})
		cancel()
		if e == nil {
			t.Fatal("accepted bad output")
		}
	}
}
func TestValidation(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix shell/symlink fixture; native protocol is tested separately")
	}
	m := Manager{Root: t.TempDir()}
	if _, e := m.Load("../bad"); e == nil {
		t.Fatal("accepted traversal")
	}
	if e := m.InstallLocal(source(t, "#!/bin/sh\n")); e != nil {
		t.Fatal(e)
	}
	if _, e := m.StartWorker(context.Background(), "echo"); e == nil {
		t.Fatal("unrequested worker")
	}
	if _, e := m.Run(context.Background(), "echo", Request{Type: "command", Command: "unknown"}); e == nil {
		t.Fatal("unregistered command")
	}
	if e := m.InstallLocal(source(t, "#!/bin/sh\n")); e == nil {
		t.Fatal("overwrote plugin")
	}
}

func TestReviewManifest(t *testing.T) {
	for _, extra := range []string{"{}", string(make([]byte, 65537))} {
		d := source(t, "#!/bin/sh\n")
		p := filepath.Join(d, "manifest.json")
		b, _ := os.ReadFile(p)
		os.WriteFile(p, append(b, []byte(extra)...), 0600)
		if _, e := readManifest(d); e == nil {
			t.Fatal("accepted trailing/oversized manifest")
		}
	}
	v := Manifest{Name: "echo", Version: "1", ProtocolVersion: 1, Executable: "run", Commands: []string{"Echo_2", "123"}}
	if e := validate(v); e != nil {
		t.Fatal(e)
	}
	v.Commands = []string{"bad-name"}
	if validate(v) == nil {
		t.Fatal("accepted incompatible command")
	}
}
func TestReviewExecutable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix shell/symlink fixture; native protocol is tested separately")
	}
	d := source(t, "#!/bin/sh\n./helper\n")
	p := filepath.Join(d, "manifest.json")
	b, _ := os.ReadFile(p)
	b = []byte(strings.ReplaceAll(string(b), `"run.sh"`, `"./run.sh"`))
	os.WriteFile(p, b, 0600)
	os.WriteFile(filepath.Join(d, "helper"), []byte("#!/bin/sh\nread line\nprintf '%s\\n' '{\"version\":1,\"text\":\"helper\"}'\n"), 0700)
	m := Manager{Root: t.TempDir()}
	if e := m.InstallLocal(d); e != nil {
		t.Fatal(e)
	}
	r, e := m.Run(context.Background(), "echo", Request{Type: "command", Command: "hello"})
	if e != nil || r.Text != "helper" {
		t.Fatalf("%+v %v", r, e)
	}
}
func TestInterruptedReplacement(t *testing.T) {
	for _, committed := range []bool{false, true} {
		m := Manager{Root: t.TempDir()}
		if e := m.InstallLocal(source(t, "#!/bin/sh\n")); e != nil {
			t.Fatal(e)
		}
		target := filepath.Join(m.Root, "plugins", "echo")
		backup := filepath.Join(m.Root, "plugins", ".previous-echo")
		if e := os.Rename(target, backup); e != nil {
			t.Fatal(e)
		}
		if committed {
			if e := os.Mkdir(target, 0700); e != nil {
				t.Fatal(e)
			}
			for _, f := range []string{"manifest.json", "run.sh"} {
				b, _ := os.ReadFile(filepath.Join(backup, f))
				os.WriteFile(filepath.Join(target, f), b, 0700)
			}
		}
		list, e := m.List()
		if e != nil || len(list) != 1 {
			t.Fatalf("%v %v", list, e)
		}
		if _, e := m.Load("echo"); e != nil {
			t.Fatal(e)
		}
		if _, e := os.Stat(backup); !os.IsNotExist(e) {
			t.Fatal("backup not recovered")
		}
	}
}
func TestQueuedCallCancellation(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix shell/symlink fixture; native protocol is tested separately")
	}
	m := Manager{Root: t.TempDir()}
	if e := m.InstallLocal(source(t, "#!/bin/sh\nread line\nsleep 1\nprintf '%s\\n' '{\"version\":1}'\n")); e != nil {
		t.Fatal(e)
	}
	w, e := m.start(context.Background(), "echo")
	if e != nil {
		t.Fatal(e)
	}
	defer w.Close()
	first := make(chan struct{})
	go func() { w.Call(context.Background(), Request{Type: "command", Command: "hello"}); close(first) }()
	time.Sleep(30 * time.Millisecond)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, e := w.Call(ctx, Request{Type: "command", Command: "hello"}); e == nil {
		t.Fatal("expected cancellation")
	}
	if time.Since(start) > 300*time.Millisecond {
		t.Fatal("queued call ignored deadline")
	}
	<-first
}

func TestRecoveryLoadInstallAndInvalidReplacement(t *testing.T) {
	for _, operation := range []string{"load", "install", "invalid"} {
		t.Run(operation, func(t *testing.T) {
			m := Manager{Root: t.TempDir()}
			if e := m.InstallLocal(source(t, "#!/bin/sh\n")); e != nil {
				t.Fatal(e)
			}
			target := filepath.Join(m.Root, "plugins", "echo")
			backup := filepath.Join(m.Root, "plugins", ".previous-echo")
			if e := os.Rename(target, backup); e != nil {
				t.Fatal(e)
			}
			if operation == "invalid" {
				os.Mkdir(target, 0700)
				os.WriteFile(filepath.Join(target, "manifest.json"), []byte("broken"), 0600)
			}
			if operation == "install" {
				if e := m.ReplaceLocal(source(t, "#!/bin/sh\n# newer\n")); e != nil {
					t.Fatal(e)
				}
			} else if _, e := m.Load("echo"); e != nil {
				t.Fatal(e)
			}
			if _, e := os.Stat(backup); !os.IsNotExist(e) {
				t.Fatal("backup remains")
			}
			if _, e := m.Load("echo"); e != nil {
				t.Fatal(e)
			}
		})
	}
}

func TestPersistentRequiresSubscription(t *testing.T) {
	v := Manifest{Name: "echo", Version: "1", ProtocolVersion: 1, Executable: "run.sh", Commands: []string{"hello"}, Persistent: true}
	if validate(v) == nil {
		t.Fatal("accepted persistent plugin without event or interval subscription")
	}
	v.Events = []string{"message"}
	if e := validate(v); e != nil {
		t.Fatal(e)
	}
	v.Events = nil
	v.IntervalSeconds = 1
	if e := validate(v); e != nil {
		t.Fatal(e)
	}
}

func TestRemoteOwnershipRecheckedInTransaction(t *testing.T) {
	m := Manager{Root: t.TempDir()}
	if e := m.InstallLocal(source(t, "#!/bin/sh\n")); e != nil {
		t.Fatal(e)
	}
	candidate := source(t, "#!/bin/sh\n# remote\n")
	if e := m.installOwned(candidate, true, CatalogURL); e == nil {
		t.Fatal("remote transaction replaced manual plugin")
	}
	b, e := os.ReadFile(filepath.Join(m.Root, "plugins", "echo", "run.sh"))
	if e != nil || string(b) != "#!/bin/sh\n" {
		t.Fatal("manual plugin changed")
	}
}
