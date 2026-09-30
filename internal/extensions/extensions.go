// Package extensions connects statically linked Go plugins to Telegram.
package extensions

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	"regexp"
	"slices"
	"strconv"
	"sync/atomic"
	"time"
)

var commandName = regexp.MustCompile(`^[a-zA-Z0-9_]+$`)

// eventQueueSize bounds one plugin's pending events; a full queue drops the
// newest event rather than stalling the update loop for every plugin.
const eventQueueSize = 64

// Register validates the complete namespace before registering any plugin.
// Installing or replacing a plugin takes effect after restart.
func Register(a *app.App) error {
	external := compiled.Entries()
	if err := registerEntries(a, bundledEntries(external)); err != nil {
		return err
	}
	registerBundledHelp(a, external)
	return nil
}

// ValidateCompiled checks linked manifests and the command namespace without
// constructing plugins, touching state, or connecting to Telegram.
func ValidateCompiled(a *app.App) error {
	return validateEntries(a, bundledEntries(compiled.Entries()))
}

var pluginName = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,63}$`)

func validateEntries(a *app.App, entries []compiled.Entry) error {
	names := map[string]bool{"tpm": true}
	plugins := map[string]bool{}
	for _, c := range a.Registry.Commands() {
		names[c.Name] = true
	}
	for _, entry := range entries {
		m := entry.Manifest
		if !pluginName.MatchString(m.Name) || plugins[m.Name] || m.Version == "" || m.ProtocolVersion != 2 || m.Package != "." || m.Executable != "" || m.Persistent || len(m.Args) > 0 || entry.New == nil {
			return fmt.Errorf("invalid compiled plugin %q", m.Name)
		}
		plugins[m.Name] = true
		if m.TimeoutSeconds < 0 || m.TimeoutSeconds > 300 || m.IntervalSeconds < 0 || m.IntervalSeconds > 31536000 {
			return fmt.Errorf("plugin %s: invalid timeout/interval", m.Name)
		}
		if _, err := hostbridge.Direct(nil, m.Capabilities); err != nil {
			return err
		}
		for _, n := range m.Commands {
			if !commandName.MatchString(n) || names[n] {
				return fmt.Errorf("plugin %s has invalid or conflicting command %q", m.Name, n)
			}
			names[n] = true
		}
		for _, e := range m.Events {
			if e != "message" && e != "outgoing" && e != "edited" {
				return fmt.Errorf("plugin %s: unsupported event %s", m.Name, e)
			}
		}
	}
	return nil
}
func registerEntries(a *app.App, entries []compiled.Entry) error {
	if err := validateEntries(a, entries); err != nil {
		return err
	}
	registerManagement(a, plugin.Manager{Root: a.Root})
	for _, entry := range entries {
		m := entry.Manifest
		host, _ := hostbridge.Direct(a.Bot, m.Capabilities)
		life, cancel := context.WithCancel(context.Background())
		r := &runtime{factory: entry.New, host: host, root: a.Root, manifest: m, logger: a.Logger, queue: make(chan event, eventQueueSize), gate: make(chan struct{}, 1), life: life, cancel: cancel}
		a.OnClose(r.close)
		for _, n := range m.Commands {
			a.Registry.Register(&command.Command{Name: n, Description: "插件 " + m.Name, Handle: func(ctx context.Context, inv *command.Invocation) error {
				event := messageEvent(inv.Message, inv.Client)
				if inv.Client != nil {
					event["self_id"] = strconv.FormatInt(inv.Client.SelfID(), 10)
				}
				raw, _ := json.Marshal(event)
				response, e := r.call(ctx, pluginapi.Request{Version: 1, Type: "command", Command: inv.Command, Args: inv.Args, Text: inv.Text, Event: raw})
				if e != nil {
					return e
				}
				if e = sendFor(ctx, inv.Client, response, m); e != nil {
					return e
				}
				formatted, isHelp := pluginHelpHTML(response.Text, inv.Prefix)
				if response.HTML {
					formatted, isHelp = response.Text, true
				}
				if isHelp {
					for i, page := range pluginapi.PanelPages(formatted) {
						var err error
						if i == 0 {
							err = inv.Edit(ctx, page)
						} else {
							err = inv.Reply(ctx, page)
						}
						if err != nil {
							return err
						}
					}
					return nil
				}
				if response.Text != "" {
					return writePluginText(ctx, response.Text,
						func(text string) error { return inv.EditText(ctx, text) },
						func(text string) error { return inv.Reply(ctx, command.Escape(text)) })
				}
				return nil
			}})
		}
		if len(m.Events) > 0 {
			a.OnIncoming(func(ctx context.Context, c *bot.Client, msg *bot.Message) bool {
				if msg.Edited && !slices.Contains(m.Events, "edited") {
					return false
				}
				if msg.Out && !slices.Contains(m.Events, "outgoing") {
					return false
				}
				if !msg.Out && !slices.Contains(m.Events, "message") {
					return false
				}
				payload := messageEvent(msg, c)
				payload["self_id"] = strconv.FormatInt(c.SelfID(), 10)
				raw, _ := json.Marshal(payload)
				select {
				case r.queue <- event{request: pluginapi.Request{Version: 1, Type: "event", Event: raw}, client: c, message: msg, queuedAt: time.Now()}:
				default:
					r.dropped.Add(1)
				}
				return false
			})
		}
		a.Registry.AddJob(r.run)
	}
	return nil
}

type event struct {
	request  pluginapi.Request
	client   *bot.Client
	message  *bot.Message
	queuedAt time.Time
}
type runtime struct {
	factory      pluginapi.Factory
	host         pluginapi.Host
	root         string
	manifest     plugin.Manifest
	logger       *slog.Logger
	queue        chan event
	dropped      atomic.Int64
	late         atomic.Int64
	seen         atomic.Int64
	slow         atomic.Int64
	slowMax      atomic.Int64
	waitMax      atomic.Int64
	callMax      atomic.Int64
	propMax      atomic.Int64
	reported     time.Time
	reportedSlow time.Time
	gate         chan struct{}
	instance     pluginapi.Plugin
	life         context.Context
	cancel       context.CancelFunc
}

func stateDirectory(root, name string) (string, error) {
	path, err := filepath.Abs(filepath.Join(root, "state", name))
	if err != nil {
		return "", err
	}
	// Reject state redirects without rejecting normal OS aliases above the deployment.
	for _, p := range []string{filepath.Dir(filepath.Dir(path)), filepath.Dir(path), path} {
		info, e := os.Lstat(p)
		if e != nil && !os.IsNotExist(e) {
			return "", e
		}
		if e == nil && (info.Mode()&os.ModeSymlink != 0 || !info.IsDir()) {
			return "", fmt.Errorf("state directory symlink or non-directory rejected: %s", p)
		}
	}
	if err = os.MkdirAll(path, 0700); err != nil {
		return "", err
	}
	return path, nil
}
func (r *runtime) call(ctx context.Context, q pluginapi.Request) (response pluginapi.Response, err error) {
	seconds := r.manifest.TimeoutSeconds
	if seconds == 0 {
		seconds = 15
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(seconds)*time.Second)
	defer cancel()
	stop := context.AfterFunc(r.life, cancel)
	defer stop()
	select {
	case r.gate <- struct{}{}:
	case <-ctx.Done():
		return response, ctx.Err()
	case <-r.life.Done():
		return response, r.life.Err()
	}
	defer func() { <-r.gate }()
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("plugin %s panicked: %v", r.manifest.Name, p)
		}
	}()
	if err = r.life.Err(); err != nil {
		return response, err
	}
	if err = ctx.Err(); err != nil {
		return response, err
	}
	if r.instance == nil {
		var state string
		state, err = stateDirectory(r.root, r.manifest.Name)
		if err != nil {
			return response, err
		}
		r.instance, err = r.factory(r.life, r.host, state)
		if err != nil {
			return response, err
		}
		if r.instance == nil {
			return response, errors.New("plugin factory returned nil")
		}
	}
	// Invoke synchronously: cooperative timeout never detaches work or permits overlap.
	response = r.instance.Handle(ctx, q)
	if err = ctx.Err(); err != nil {
		return response, err
	}

	if response.Version != 1 {
		return response, errors.New("invalid response version")
	}
	if len(response.Messages) > 20 {
		return response, errors.New("too many response messages")
	}
	data, e := json.Marshal(response)
	if e != nil {
		return response, e
	}
	if len(data) > plugin.MaxOutput {
		return response, errors.New("plugin response too large")
	}
	if response.Error != "" {
		return response, errors.New(response.Error)
	}
	return response, nil
}

// eventLateAfter mirrors the freshness gate plugins apply to queued messages: an
// event older than this is about to be ignored by them, and that loss is
// otherwise invisible because it happens after a successful dequeue.
const eventLateAfter = 60

// eventSlowAfter is the age at which an event is considered late to arrive
// rather than merely old; it separates Telegram-side propagation from our own
// queue backlog.
const eventSlowAfter = 3

// markLate counts events that reached the plugin too late to be acted on, and
// separately tracks how old events were when they finally got dequeued.
func (r *runtime) markLate(ev event) {
	if ev.message == nil {
		return
	}
	d := messageDate(ev.message)
	if d <= 0 {
		return
	}
	r.seen.Add(1)
	age := time.Now().Unix() - int64(d)
	if age > eventLateAfter {
		r.late.Add(1)
	}
	if age <= eventSlowAfter {
		return
	}
	r.slow.Add(1)
	peak(&r.slowMax, int64(age))
	// Split the age into "Telegram had not delivered it yet" and "it sat in our
	// own queue": the two have opposite fixes, so a single number is useless.
	if !ev.queuedAt.IsZero() {
		wait := int64(time.Since(ev.queuedAt).Seconds())
		peak(&r.waitMax, wait)
		peak(&r.propMax, int64(age)-wait)
	}
}

// peak records the largest observed value without a lock.
func peak(target *atomic.Int64, v int64) {
	for {
		cur := target.Load()
		if v <= cur || target.CompareAndSwap(cur, v) {
			return
		}
	}
}

// reportSlow samples how long events wait before the plugin sees them. It is
// rate-limited far more coarsely than the loss summary so a channel that is
// simply slow to push does not generate a line every minute.
func (r *runtime) reportSlow() {
	n := r.slow.Load()
	if n == 0 || r.logger == nil || time.Since(r.reportedSlow) < 10*time.Minute {
		return
	}
	r.reportedSlow = time.Now()
	r.logger.Warn("plugin.event_slow", "plugin", r.manifest.Name, "slow", r.slow.Swap(0), "seen", r.seen.Swap(0), "max_age_s", r.slowMax.Swap(0), "max_queue_s", r.waitMax.Swap(0), "max_source_s", r.propMax.Swap(0), "max_call_ms", r.callMax.Swap(0))
}

// reportDropped turns silent loss into one line per minute: the per-message
// warning it replaces could bury the log at thousands of lines an hour.
func (r *runtime) reportDropped() {
	dropped, late := r.dropped.Load(), r.late.Load()
	if (dropped == 0 && late == 0) || r.logger == nil || time.Since(r.reported) < time.Minute {
		return
	}
	r.reported = time.Now()
	r.logger.Warn("plugin.events_dropped", "plugin", r.manifest.Name, "count", r.dropped.Swap(0), "late", r.late.Swap(0), "queue", cap(r.queue))
}

func (r *runtime) close() {
	r.cancel()
	r.gate <- struct{}{}
	defer func() { <-r.gate }()
	defer func() {
		if p := recover(); p != nil && r.logger != nil {
			r.logger.Warn("plugin.close_panicked", "plugin", r.manifest.Name, "panic", p)
		}
	}()
	if r.instance != nil {
		instance := r.instance
		r.instance = nil
		instance.Close()
	}
}
func (r *runtime) run(ctx context.Context, client *bot.Client) {
	defer r.close()
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
		case <-r.life.Done():
			return
		case ev = <-r.queue:
		case <-ticks:
			ev = event{request: pluginapi.Request{Version: 1, Type: "tick"}, client: client}
		}
		r.markLate(ev)
		r.reportDropped()
		r.reportSlow()
		called := time.Now()
		response, e := r.call(ctx, ev.request)
		peak(&r.callMax, time.Since(called).Milliseconds())
		if e == nil {
			e = sendFor(ctx, ev.client, response, r.manifest)
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
func messageEvent(m *bot.Message, clients ...*bot.Client) map[string]any {
	channelDM := false
	if m.Out && m.Raw != nil && m.Raw.SavedPeerID != nil && len(clients) > 0 && clients[0] != nil {
		if id, ok := m.Channel(); ok {
			ch, found := clients[0].Peers().Channel(id)
			channelDM = found && ch.Monoforum
		}
	}
	_, user := m.Sender.(*tg.PeerUser)
	payload := map[string]any{"chat_type": string(m.ChatType), "channel_dm": channelDM, "type": "message", "chat_id": m.ChatID, "message_id": m.ID, "sender_id": m.SenderID(), "sender_is_user": user, "sender_peer_id": bot.PeerID(m.Sender), "text": m.Text, "edited": m.Edited, "reply_to_id": m.ReplyToID, "out": m.Out, "date": messageDate(m)}
	if m.Raw != nil && len(clients) > 0 && clients[0] != nil {
		payload["message"] = hostbridge.SerializeFor(clients[0], m.Raw)
	}
	return payload
}
func send(ctx context.Context, c *bot.Client, r pluginapi.Response) error {
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

func messageDate(m *bot.Message) int {
	if m.Raw != nil {
		return m.Raw.Date
	}
	return 0
}

func sendFor(ctx context.Context, c *bot.Client, r pluginapi.Response, m plugin.Manifest) error {
	if len(m.Capabilities) > 0 && len(r.Messages) > 0 && !slices.Contains(m.Capabilities, "send") {
		return fmt.Errorf("plugin %s lacks send capability", m.Name)
	}
	return send(ctx, c, r)
}
