package app

import (
	"context"
	"testing"
	"time"
)

func TestRequestRestartCancelsRuntime(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	a := &App{shutdown: cancel}
	a.RequestRestart()
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("not canceled")
	}
	if !a.RestartRequested() {
		t.Fatal("restart not recorded")
	}
}

func TestCloseRunsCleanupOnce(t *testing.T) {
	a := &App{}
	count := 0
	a.OnClose(func() { count++ })
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	a.Close()
	if count != 1 {
		t.Fatal(count)
	}
	a.OnClose(func() { count++ })
	if count != 2 {
		t.Fatal("late cleanup not called")
	}
}
