// Package extensions connects the process plugin protocol to Telegram.
package extensions

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/OrionG-hub/laowangbot/internal/app"
	"github.com/OrionG-hub/laowangbot/internal/bot"
	"github.com/OrionG-hub/laowangbot/internal/command"
	"github.com/OrionG-hub/laowangbot/internal/plugin"
	"log/slog"
	"regexp"
	"slices"
	"time"
)

var commandName = regexp.MustCompile(`^[a-zA-Z0-9_]+$`)

// Register validates the complete namespace before registering any plugin.
// Installing or replacing a plugin takes effect after restart.
func Register(a *app.App) error {
	manager := plugin.Manager{Root: a.Root}
	snapshot, cleanup, e := manager.Snapshot()
	if e != nil {
		return e
	}
	registered := false
	defer func() {
		if !registered {
			cleanup()
		}
	}()
	manifests, e := snapshot.List()
	if e != nil {
		return e
	}
	names := map[string]bool{"tpm": true}
	for _, c := range a.Registry.Commands() {
		names[c.Name] = true
	}
	for _, m := range manifests {
		for _, n := range m.Commands {
			if !commandName.MatchString(n) || names[n] {
				return fmt.Errorf("plugin %s has invalid or conflicting command %q", m.Name, n)
			}
			names[n] = true
		}
		for _, event := range m.Events {
			if event != "message" {
				return fmt.Errorf("plugin %s: unsupported event %s", m.Name, event)
			}
		}
	}
	registerManagement(a, manager)
	for _, m := range manifests {
		r := &runtime{manager: snapshot, manifest: m, logger: a.Logger, queue: make(chan event, 64), gate: make(chan struct{}, 1)}
		for _, n := range m.Commands {
			a.Registry.Register(&command.Command{Name: n, Description: "插件 " + m.Name, Handle: func(ctx context.Context, inv *command.Invocation) error {
				raw, _ := json.Marshal(messageEvent(inv.Message))
				response, e := r.call(ctx, plugin.Request{Version: 1, Type: "command", Command: inv.Command, Args: inv.Args, Text: inv.Text, Event: raw})
				if e != nil {
					return e
				}
				if e = send(ctx, inv.Client, response); e != nil {
					return e
				}
				if response.Text != "" {
					return inv.EditText(ctx, response.Text)
				}
				return nil
			}})
		}
		if slices.Contains(m.Events, "message") {
			a.OnIncoming(func(ctx context.Context, c *bot.Client, msg *bot.Message) bool {
				raw, _ := json.Marshal(messageEvent(msg))
				select {
				case r.queue <- event{request: plugin.Request{Version: 1, Type: "event", Event: raw}, client: c, message: msg}:
				default:
					if a.Logger != nil {
						a.Logger.Warn("plugin.event_dropped", "plugin", m.Name)
					}
				}
				return false
			})
		}
		a.Registry.AddJob(r.run)
	}
	a.OnClose(cleanup)
	registered = true
	return nil
}

type event struct {
	request plugin.Request
	client  *bot.Client
	message *bot.Message
}
type runtime struct {
	manager  plugin.Manager
	manifest plugin.Manifest
	logger   *slog.Logger
	queue    chan event
	gate     chan struct{}
	worker   *plugin.Worker
	life     context.Context
}

func (r *runtime) call(ctx context.Context, q plugin.Request) (plugin.Response, error) {
	if !r.manifest.Persistent {
		return r.manager.Run(ctx, r.manifest.Name, q)
	}
	select {
	case r.gate <- struct{}{}:
	case <-ctx.Done():
		return plugin.Response{}, ctx.Err()
	}
	defer func() { <-r.gate }()
	if r.life == nil {
		return plugin.Response{}, fmt.Errorf("plugin worker is not ready")
	}
	if r.worker == nil {
		w, e := r.manager.StartWorker(r.life, r.manifest.Name)
		if e != nil {
			return plugin.Response{}, e
		}
		r.worker = w
	}
	response, e := r.worker.Call(ctx, q)
	if e != nil {
		r.worker.Close()
		r.worker = nil
	}
	return response, e
}
func (r *runtime) run(ctx context.Context, client *bot.Client) {
	r.gate <- struct{}{}
	r.life = ctx
	<-r.gate
	defer func() {
		r.gate <- struct{}{}
		defer func() { <-r.gate }()
		if r.worker != nil {
			r.worker.Close()
		}
		r.life = nil
	}()
	var ticks <-chan time.Time
	if r.manifest.IntervalSeconds > 0 {
		t := time.NewTicker(time.Duration(r.manifest.IntervalSeconds) * time.Second)
		defer t.Stop()
		ticks = t.C
	}
	for {
		var ev event
		select {
		case <-ctx.Done():
			return
		case ev = <-r.queue:
		case <-ticks:
			ev = event{request: plugin.Request{Version: 1, Type: "tick"}, client: client}
		}
		response, e := r.call(ctx, ev.request)
		if e == nil {
			e = send(ctx, ev.client, response)
		}
		if e == nil && response.Text != "" && ev.message != nil {
			peer, err := ev.client.InputPeerFromChatID(ev.message.ChatID)
			if err == nil {
				_, e = ev.client.SendText(ctx, peer, response.Text, bot.SendOptions{ReplyTo: ev.message.ID})
			} else {
				e = err
			}
		}
		if e != nil && r.logger != nil {
			r.logger.Warn("plugin.request_failed", "plugin", r.manifest.Name, "error", e)
		}
	}
}
func messageEvent(m *bot.Message) map[string]any {
	return map[string]any{"type": "message", "chat_id": m.ChatID, "message_id": m.ID, "sender_id": m.SenderID(), "text": m.Text, "edited": m.Edited, "reply_to_id": m.ReplyToID}
}
func send(ctx context.Context, c *bot.Client, r plugin.Response) error {
	for _, m := range r.Messages {
		peer, e := c.InputPeerFromChatID(m.ChatID)
		if e != nil {
			return e
		}
		if _, e = c.SendText(ctx, peer, m.Text, bot.SendOptions{}); e != nil {
			return e
		}
	}
	return nil
}
