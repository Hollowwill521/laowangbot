package plugin

import (
	"bufio"
	"context"
	"encoding/json"
	"github.com/OrionG-hub/laowangbot/internal/platform"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// The test binary itself is the plugin, including on Windows where shebangs do not work.
func TestNativePluginHelper(t *testing.T) {
	if os.Getenv("LAOWANGBOT_PROTOCOL_VERSION") != "1" {
		return
	}
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		var r Request
		if json.Unmarshal(scanner.Bytes(), &r) != nil {
			os.Exit(2)
		}
		out := Response{Version: 1, Text: r.Text}
		if r.Command == "fail" {
			out.Error = "native failure"
		}
		json.NewEncoder(os.Stdout).Encode(out)
	}
	os.Exit(0)
}
func TestNativeProcessProtocol(t *testing.T) {
	executable, e := os.Executable()
	if e != nil {
		t.Fatal(e)
	}
	b, e := os.ReadFile(executable)
	if e != nil {
		t.Fatal(e)
	}
	d := t.TempDir()
	if e = os.WriteFile(filepath.Join(d, "helper.exe"), b, 0700); e != nil {
		t.Fatal(e)
	}
	manifest := Manifest{Name: "native", Version: "1", ProtocolVersion: 1, Executable: "helper.exe", Args: []string{"-test.run=^TestNativePluginHelper$"}, Commands: []string{"echo", "fail"}, Events: []string{"message"}, Persistent: true}
	b, _ = json.Marshal(manifest)
	os.WriteFile(filepath.Join(d, "manifest.json"), b, 0600)
	m := Manager{Root: t.TempDir()}
	if e = m.InstallLocal(d); e != nil {
		t.Fatal(e)
	}
	r, e := m.Run(context.Background(), "native", Request{Type: "command", Command: "echo", Text: "native protocol"})
	if e != nil || r.Text != "native protocol" {
		t.Fatalf("%+v %v", r, e)
	}
	if _, e = m.Run(context.Background(), "native", Request{Type: "command", Command: "fail"}); e == nil {
		t.Fatal("ignored response error")
	}
	w, e := m.StartWorker(context.Background(), "native")
	if e != nil {
		t.Fatal(e)
	}
	defer w.Close()
	for i := 0; i < 2; i++ {
		r, e = w.Call(context.Background(), Request{Type: "event", Text: "persistent"})
		if e != nil || r.Text != "persistent" {
			t.Fatalf("%+v %v", r, e)
		}
	}
}
func TestIntervalBound(t *testing.T) {
	v := Manifest{Name: "echo", Version: "1", ProtocolVersion: 1, Executable: "run", IntervalSeconds: 31536001}
	if validate(v) == nil {
		t.Fatal("accepted overflowing schedule interval")
	}
	v.IntervalSeconds = 31536000
	if e := validate(v); e != nil {
		t.Fatal(e)
	}
}

func TestNativeLockHelper(t *testing.T) {
	root := os.Getenv("LAOWANGBOT_TEST_LOCK_ROOT")
	if root == "" {
		return
	}
	_, e := (Manager{Root: root}).List()
	if e == nil {
		os.Exit(3)
	}
	os.Exit(0)
}
func TestCrossProcessTransactionLock(t *testing.T) {
	m := Manager{Root: t.TempDir()}
	d := filepath.Join(m.Root, ".plugin-lock")
	if e := os.MkdirAll(d, 0700); e != nil {
		t.Fatal(e)
	}
	lock, e := platform.LockRoot(d)
	if e != nil {
		t.Fatal(e)
	}
	defer lock.Close()
	binary, e := os.Executable()
	if e != nil {
		t.Fatal(e)
	}
	cmd := exec.Command(binary, "-test.run=^TestNativeLockHelper$")
	cmd.Env = append(os.Environ(), "LAOWANGBOT_TEST_LOCK_ROOT="+m.Root)
	if b, e := cmd.CombinedOutput(); e != nil {
		t.Fatalf("lock not respected: %s %v", b, e)
	}
}
