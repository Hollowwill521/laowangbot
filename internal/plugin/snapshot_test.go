package plugin

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestSnapshotRetainsRunningResourcesAndDeploymentState(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix script fixture; native snapshots are tested separately")
	}
	m := Manager{Root: t.TempDir()}
	makeSource := func(value string) string {
		d := source(t, "#!/bin/sh\nwhile IFS= read -r line; do v=$(cat value.txt 2>/dev/null || printf MISSING); printf '%s' state > \"$LAOWANGBOT_STATE_DIR/check\"; printf '{\"version\":1,\"text\":\"%s\"}\\n' \"$v\"; done\n")
		os.WriteFile(filepath.Join(d, "manifest.json"), []byte(`{"name":"echo","version":"1","protocol_version":1,"executable":"run.sh","commands":["hello"],"events":["message"],"persistent":true}`), 0600)
		os.WriteFile(filepath.Join(d, "value.txt"), []byte(value), 0600)
		return d
	}
	if e := m.InstallLocal(makeSource("old")); e != nil {
		t.Fatal(e)
	}
	snapshot, cleanup, e := m.Snapshot()
	if e != nil {
		t.Fatal(e)
	}
	defer cleanup()
	w, e := snapshot.StartWorker(context.Background(), "echo")
	if e != nil {
		t.Fatal(e)
	}
	defer w.Close()
	q := Request{Type: "command", Command: "hello"}
	before, e := w.Call(context.Background(), q)
	if e != nil || before.Text != "old" {
		t.Fatalf("before %+v %v", before, e)
	}
	if e = m.ReplaceLocal(makeSource("new")); e != nil {
		t.Fatal(e)
	}
	after, e := w.Call(context.Background(), q)
	if e != nil || after.Text != "old" {
		t.Fatalf("running worker changed %+v %v", after, e)
	}
	next, e := snapshot.Run(context.Background(), "echo", q)
	if e != nil || next.Text != "old" {
		t.Fatalf("new call changed %+v %v", next, e)
	}
	b, e := os.ReadFile(filepath.Join(m.Root, "state/echo/check"))
	if e != nil || string(b) != "state" {
		t.Fatalf("state not kept in deployment: %q %v", b, e)
	}
	refreshed, remove, e := m.Snapshot()
	if e != nil {
		t.Fatal(e)
	}
	defer remove()
	updated, e := refreshed.Run(context.Background(), "echo", q)
	if e != nil || updated.Text != "new" {
		t.Fatalf("restart did not load update %+v %v", updated, e)
	}
}

func TestNativeSnapshotAndCleanup(t *testing.T) {
	executable, e := os.Executable()
	if e != nil {
		t.Fatal(e)
	}
	binary, e := os.ReadFile(executable)
	if e != nil {
		t.Fatal(e)
	}
	d := t.TempDir()
	os.WriteFile(filepath.Join(d, "helper.exe"), binary, 0700)
	manifest := Manifest{Name: "native", Version: "1", ProtocolVersion: 1, Executable: "helper.exe", Args: []string{"-test.run=^TestNativePluginHelper$"}, Commands: []string{"echo"}}
	b, _ := json.Marshal(manifest)
	os.WriteFile(filepath.Join(d, "manifest.json"), b, 0600)
	m := Manager{Root: t.TempDir()}
	if e = m.InstallLocal(d); e != nil {
		t.Fatal(e)
	}
	snapshot, cleanup, e := m.Snapshot()
	if e != nil {
		t.Fatal(e)
	}
	defer cleanup()
	if e = os.RemoveAll(filepath.Join(m.Root, "plugins")); e != nil {
		t.Fatal(e)
	}
	r, e := snapshot.Run(context.Background(), "native", Request{Type: "command", Command: "echo", Text: "snapshot"})
	if e != nil || r.Text != "snapshot" {
		t.Fatalf("%+v %v", r, e)
	}
	if _, e = os.Stat(filepath.Join(m.Root, "state/native")); e != nil {
		t.Fatal(e)
	}
	cleanup()
	if _, e = os.Stat(snapshot.Root); !os.IsNotExist(e) {
		t.Fatalf("snapshot not cleaned: %v", e)
	}
}
