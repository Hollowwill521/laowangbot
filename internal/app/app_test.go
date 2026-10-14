package app

import (
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestCloseRunsDynamicCleanupAndSharesResult(t *testing.T) {
	var count atomic.Int32
	started := make(chan struct{})
	release := make(chan struct{})
	a := &App{}
	a.OnClose(func() {
		count.Add(1)
		close(started)
		<-release
		a.OnClose(func() { count.Add(1) })
	})
	first := make(chan error, 1)
	go func() { first <- a.Close() }()
	<-started
	second := make(chan error, 1)
	go func() { second <- a.Close() }()
	select {
	case <-second:
		t.Fatal("concurrent Close returned before cleanup completed")
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	if err := <-first; err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := <-second; err != nil {
		t.Fatalf("second Close: %v", err)
	}
	if got := count.Load(); got != 2 {
		t.Fatalf("cleanup count = %d, want 2", got)
	}
}

func TestCloseRecordsCleanupPanic(t *testing.T) {
	a := &App{}
	a.OnClose(func() { panic("boom") })
	err := a.Close()
	if err == nil || !strings.Contains(err.Error(), "close cleanup panic") {
		t.Fatalf("Close error = %v", err)
	}
	if got := a.Close(); got == nil || got.Error() != err.Error() {
		t.Fatalf("second Close error = %v, want %v", got, err)
	}
}
