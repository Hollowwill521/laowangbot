package extensions

import (
	"context"
	"os"
	"path/filepath"
	goruntime "runtime"
	"testing"

	"github.com/OrionG-hub/laowangbot/internal/bot"
	"github.com/OrionG-hub/laowangbot/internal/command"
	"github.com/OrionG-hub/laowangbot/internal/plugin"
)

// An empty successful response needs no Telegram client, so this exercises the
// registered command and real process without a network connection.
func TestReplacementWaitsForNextRegistration(t *testing.T) {
	if goruntime.GOOS == "windows" {
		t.Skip("Unix script fixture")
	}
	a := fixture(t, []string{"hello"})
	defer a.Close()
	script := "#!/bin/sh\nread line\nprintf '%s\\n' '{\"version\":1}'\n"
	os.WriteFile(filepath.Join(a.Root, "plugins/demo/run"), []byte(script), 0700)
	if e := Register(a); e != nil {
		t.Fatal(e)
	}
	replacement := fixture(t, []string{"goodbye"})
	os.WriteFile(filepath.Join(replacement.Root, "plugins/demo/run"), []byte(script), 0700)
	if e := (plugin.Manager{Root: a.Root}).ReplaceLocal(filepath.Join(replacement.Root, "plugins/demo")); e != nil {
		t.Fatal(e)
	}
	c, _ := a.Registry.Lookup("hello")
	// The handler accesses only messageEvent and empty response fields.
	inv := &command.Invocation{Command: "hello"}
	inv.Message = &bot.Message{}
	if e := c.Handle(context.Background(), inv); e != nil {
		t.Fatalf("registered command changed before restart: %v", e)
	}
	// A fresh registration represents a restart and must see the new command set.
	fresh := fixture(t, nil)
	fresh.Root = a.Root
	defer fresh.Close()
	if e := Register(fresh); e != nil {
		t.Fatal(e)
	}
	if _, ok := fresh.Registry.Lookup("goodbye"); !ok {
		t.Fatal("new command absent after restart")
	}
	if _, ok := fresh.Registry.Lookup("hello"); ok {
		t.Fatal("old command still registered after restart")
	}

}
