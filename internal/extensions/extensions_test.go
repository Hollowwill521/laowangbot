package extensions

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/OrionG-hub/laowangbot/internal/app"
	"github.com/OrionG-hub/laowangbot/internal/bot"
	"github.com/OrionG-hub/laowangbot/internal/command"
	"github.com/OrionG-hub/laowangbot/internal/compiled"
	"github.com/OrionG-hub/laowangbot/internal/hostbridge"
	"github.com/OrionG-hub/laowangbot/internal/plugin"
	"github.com/OrionG-hub/laowangbot/pkg/pluginapi"
	"github.com/gotd/td/tg"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func fixture(t *testing.T, names []string) *app.App {
	t.Helper()
	root := t.TempDir()
	p := filepath.Join(root, "plugins/demo")
	os.MkdirAll(p, 0700)
	raw, _ := json.Marshal(plugin.Manifest{Name: "demo", Version: "1", ProtocolVersion: 1, Executable: "run", Commands: names})
	os.WriteFile(filepath.Join(p, "manifest.json"), raw, 0600)
	os.WriteFile(filepath.Join(p, "run"), []byte("#!/bin/sh\nexit 0\n"), 0700)
	return &app.App{Root: root, Logger: slog.Default(), Registry: command.New([]string{"，"}, slog.Default())}
}
func TestRegisterPluginsAndManagement(t *testing.T) {
	a := fixture(t, []string{"hello"})
	if e := registerEntries(a, staticEntries([]string{"hello"})); e != nil {
		t.Fatal(e)
	}
	for _, name := range []string{"hello", "tpm"} {
		if _, ok := a.Registry.Lookup(name); !ok {
			t.Fatal(name)
		}
	}
}
func TestCommandCollisionReturnsError(t *testing.T) {
	a := fixture(t, []string{"ping"})
	a.Registry.Register(&command.Command{Name: "ping"})
	if e := registerEntries(a, staticEntries([]string{"ping"})); e == nil {
		t.Fatal("accepted collision")
	}
}
func TestNoPartialRegistrationOnConflict(t *testing.T) {
	a := fixture(t, []string{"hello", "ping"})
	a.Registry.Register(&command.Command{Name: "ping"})
	if e := registerEntries(a, staticEntries([]string{"hello", "ping"})); e == nil {
		t.Fatal("accepted collision")
	}
	if _, ok := a.Registry.Lookup("hello"); ok {
		t.Fatal("partial registration")
	}
}

type testPlugin struct {
	handle func(context.Context, pluginapi.Request) pluginapi.Response
	close  func()
}

func (p *testPlugin) Handle(c context.Context, q pluginapi.Request) pluginapi.Response {
	if p.handle != nil {
		return p.handle(c, q)
	}
	return pluginapi.Response{Version: 1}
}
func (p *testPlugin) Close() {
	if p.close != nil {
		p.close()
	}
}
func staticEntries(names []string) []compiled.Entry {
	return []compiled.Entry{{Manifest: plugin.Manifest{Name: "demo", Version: "1", ProtocolVersion: 2, Package: ".", Commands: names}, New: func(context.Context, pluginapi.Host, string) (pluginapi.Plugin, error) { return &testPlugin{}, nil }}}
}

func TestNamespacedSenderAndCapabilityMessages(t *testing.T) {
	event := messageEvent(&bot.Message{Sender: &tg.PeerChannel{ChannelID: 123}})
	if event["sender_is_user"] != false || event["sender_peer_id"] == "123" {
		t.Fatal(event)
	}
	err := sendFor(t.Context(), nil, pluginapi.Response{Messages: []pluginapi.Outgoing{{ChatID: "1", Text: "x"}}}, plugin.Manifest{Name: "read", Capabilities: []string{"messages"}})
	if err == nil {
		t.Fatal("read-only plugin sent messages")
	}
}

func TestLazyFactoryStateAndClose(t *testing.T) {
	a := fixture(t, nil)
	entries := staticEntries([]string{"hello"})
	var lifetime context.Context
	count, closed := 0, 0
	entries[0].New = func(ctx context.Context, h pluginapi.Host, state string) (pluginapi.Plugin, error) {
		count++
		lifetime = ctx
		if state != filepath.Join(a.Root, "state", "demo") {
			t.Errorf("state=%s", state)
		}
		if _, err := os.Stat(state); err != nil {
			t.Error(err)
		}
		return &testPlugin{close: func() {
			closed++
			if ctx.Err() == nil {
				t.Error("close before cancellation")
			}
		}}, nil
	}
	if err := registerEntries(a, entries); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("factory was eager")
	}
	c, _ := a.Registry.Lookup("hello")
	ctx, cancel := context.WithCancel(context.Background())
	inv := &command.Invocation{Command: "hello", Message: &bot.Message{}}
	if err := c.Handle(ctx, inv); err != nil {
		t.Fatal(err)
	}
	cancel()
	if lifetime.Err() != nil {
		t.Fatal("factory received request-scoped context")
	}
	if err := c.Handle(context.Background(), inv); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("constructed %d times", count)
	}
	a.Close()
	a.Close()
	if closed != 1 {
		t.Fatalf("closed %d times", closed)
	}
	if err := c.Handle(context.Background(), inv); err == nil {
		t.Fatal("call after close succeeded")
	}
}

func TestStateSymlinksRejected(t *testing.T) {
	for _, where := range []string{"state", "state/demo"} {
		t.Run(where, func(t *testing.T) {
			a := fixture(t, nil)
			defer a.Close()
			path := filepath.Join(a.Root, where)
			if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(t.TempDir(), path); err != nil {
				t.Skip(err)
			}
			entries := staticEntries([]string{"hello"})
			entries[0].New = func(context.Context, pluginapi.Host, string) (pluginapi.Plugin, error) {
				t.Fatal("factory reached unsafe state path")
				return nil, nil
			}
			if err := registerEntries(a, entries); err != nil {
				t.Fatal(err)
			}
			c, _ := a.Registry.Lookup("hello")
			if err := c.Handle(context.Background(), &command.Invocation{Message: &bot.Message{}}); err == nil {
				t.Fatal("accepted symlink")
			}
		})
	}
}

func TestInvalidCompiledEntriesDoNotRegister(t *testing.T) {
	for _, change := range []func(*compiled.Entry){
		func(e *compiled.Entry) { e.Manifest.ProtocolVersion = 1 },
		func(e *compiled.Entry) { e.Manifest.Package = "../outside" },
		func(e *compiled.Entry) { e.Manifest.Name = "../outside" },
		func(e *compiled.Entry) { e.Manifest.Events = []string{"unknown"} },
		func(e *compiled.Entry) { e.Manifest.Capabilities = []string{"unknown"} },
		func(e *compiled.Entry) { e.Manifest.TimeoutSeconds = 301 },
		func(e *compiled.Entry) { e.New = nil },
	} {
		a := fixture(t, nil)
		entries := staticEntries([]string{"hello"})
		change(&entries[0])
		if err := registerEntries(a, entries); err == nil {
			t.Fatal("invalid entry accepted")
		}
		if _, ok := a.Registry.Lookup("tpm"); ok {
			t.Fatal("partial registration")
		}
	}
	a := fixture(t, nil)
	entries := staticEntries([]string{"hello"})
	entries = append(entries, entries[0])
	if err := registerEntries(a, entries); err == nil {
		t.Fatal("duplicate plugin accepted")
	}
}

func newTestRuntime(t *testing.T, p pluginapi.Plugin) *runtime {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	r := &runtime{root: t.TempDir(), manifest: plugin.Manifest{Name: "demo", TimeoutSeconds: 1}, life: ctx, cancel: cancel, gate: make(chan struct{}, 1), queue: make(chan event, 64), factory: func(context.Context, pluginapi.Host, string) (pluginapi.Plugin, error) { return p, nil }}
	t.Cleanup(r.close)
	return r
}

func TestCooperativeTimeoutDoesNotDetachOrOverlap(t *testing.T) {
	entered, expired, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	r := newTestRuntime(t, &testPlugin{handle: func(ctx context.Context, _ pluginapi.Request) pluginapi.Response {
		if calls.Add(1) == 1 {
			close(entered)
			<-ctx.Done()
			close(expired)
			<-release
		}
		return pluginapi.Response{Version: 1}
	}})
	first := make(chan error, 1)
	go func() { _, err := r.call(context.Background(), pluginapi.Request{}); first <- err }()
	<-entered
	<-expired
	select {
	case <-first:
		t.Fatal("timed-out plugin detached")
	default:
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := r.call(ctx, pluginapi.Request{}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatal("overlapping calls")
	}
	close(release)
	if err := <-first; !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	if _, err := r.call(context.Background(), pluginapi.Request{}); err != nil {
		t.Fatal(err)
	}
}

func TestPanicAndPluginErrorAreReturned(t *testing.T) {
	for _, handler := range []func(context.Context, pluginapi.Request) pluginapi.Response{
		func(context.Context, pluginapi.Request) pluginapi.Response { panic("failed") },
		func(context.Context, pluginapi.Request) pluginapi.Response {
			return pluginapi.Response{Error: "failed"}
		},
	} {
		r := newTestRuntime(t, &testPlugin{handle: handler, close: func() { panic("close failure") }})
		if _, err := r.call(context.Background(), pluginapi.Request{}); err == nil {
			t.Fatal("failure ignored")
		}
		r.close()
	}
}

func TestRunDeliversEventsAndTicksAndCloses(t *testing.T) {
	requests := make(chan string, 4)
	var closed atomic.Int32
	r := newTestRuntime(t, &testPlugin{handle: func(_ context.Context, q pluginapi.Request) pluginapi.Response {
		requests <- q.Type
		return pluginapi.Response{Version: 1}
	}, close: func() { closed.Add(1) }})
	r.manifest.IntervalSeconds = 1
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { r.run(ctx, nil); close(done) }()
	r.queue <- event{request: pluginapi.Request{Type: "event"}}
	for _, want := range []string{"event", "tick"} {
		select {
		case got := <-requests:
			if got != want {
				t.Fatalf("got %s want %s", got, want)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("event/tick missing")
		}
	}
	cancel()
	<-done
	if closed.Load() != 1 {
		t.Fatal("run did not close plugin")
	}
}

func TestDirectHostEnforcesCapabilitiesBeforeClient(t *testing.T) {
	if _, err := hostbridge.Direct(nil, []string{"unknown"}); err == nil {
		t.Fatal("unknown capability accepted")
	}
	reached := false
	h, err := hostbridge.Direct(func() *bot.Client { reached = true; return nil }, []string{"self"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = h.Call(context.Background(), pluginapi.Call{Method: "send"}); err == nil || reached {
		t.Fatal("denied call dispatched")
	}
	if _, err = h.Call(context.Background(), pluginapi.Call{Method: "self"}); err == nil || !reached {
		t.Fatal("disconnected client not checked")
	}
}

func TestResponseBoundsAndPersistentManifest(t *testing.T) {
	a := fixture(t, nil)
	entries := staticEntries([]string{"hello"})
	entries[0].Manifest.Persistent = true
	entries[0].Manifest.Events = []string{"message"}
	if err := validateEntries(a, entries); err == nil {
		t.Fatal("persistent flag without subscriptions accepted")
	}
	for _, response := range []pluginapi.Response{
		{Version: 2},
		{Version: 1, Messages: make([]pluginapi.Outgoing, 21)},
		{Version: 1, Text: strings.Repeat("x", (1<<20)+1)},
	} {
		r := newTestRuntime(t, &testPlugin{handle: func(context.Context, pluginapi.Request) pluginapi.Response { return response }})
		if _, err := r.call(context.Background(), pluginapi.Request{}); err == nil {
			t.Fatal("invalid response accepted")
		}
	}
}
