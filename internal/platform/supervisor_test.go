package platform

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestSupervisorHelper(t *testing.T) {
	if os.Getenv("LAOWANGBOT_TEST_CHILD") != "1" {
		return
	}
	p := os.Getenv("LAOWANGBOT_TEST_COUNTER")
	if _, e := os.Stat(p); os.IsNotExist(e) {
		os.WriteFile(p, []byte("restarted"), 0600)
		os.Exit(75)
	}
	os.Exit(0)
}
func TestSupervisorRestartsOnlyRequestedExit(t *testing.T) {
	t.Setenv("LAOWANGBOT_TEST_CHILD", "1")
	p := filepath.Join(t.TempDir(), "count")
	t.Setenv("LAOWANGBOT_TEST_COUNTER", p)
	e := Supervise(context.Background(), os.Args[0], []string{"-test.run=^TestSupervisorHelper$"}, nil, io.Discard, io.Discard)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = os.Stat(p); e != nil {
		t.Fatal(e)
	}
}
