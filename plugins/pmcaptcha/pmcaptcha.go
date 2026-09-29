// Package pmcaptcha implements the PMCaptcha 5.1.2 Merged private-chat rules.
package pmcaptcha

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	api "github.com/OrionG-hub/laowangbot/pkg/pluginapi"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

//go:embed manifest.json
var manifestJSON string

func ManifestJSON() string                                                 { return manifestJSON }
func Open(ctx context.Context, h api.Host, dir string) (api.Plugin, error) { return New(ctx, h, dir) }

type state struct {
	Answer, Question, Mode string
	QA                     bool
	Tries                  int
	Messages               []int
	Started                time.Time
}
type Plugin struct {
	mu         sync.Mutex
	host       api.Host
	dir        string
	config     Config
	data       Data
	states     map[int64]*state
	outgoing   map[string]time.Time
	normalized bool
	closed     bool
}

func New(_ context.Context, h api.Host, dir string) (*Plugin, error) {
	p := &Plugin{host: h, dir: dir, states: map[int64]*state{}, outgoing: map[string]time.Time{}}
	if e := os.MkdirAll(dir, 0700); e != nil {
		return nil, e
	}
	if e := p.load(); e != nil {
		return nil, e
	}
	return p, nil
}
func (p *Plugin) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.closed = true
	clear(p.states)
	clear(p.outgoing)
}
func (p *Plugin) call(ctx context.Context, c api.Call) (api.Result, error) {
	if e := ctx.Err(); e != nil {
		return api.Result{}, e
	}
	r, e := p.host.Call(ctx, c)
	if e == nil && r.Error != "" {
		e = errors.New(r.Error)
	}
	return r, e
}
func (p *Plugin) Handle(ctx context.Context, r api.Request) api.Response {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := api.Response{Version: 1}
	if p.closed {
		out.Error = "插件已关闭"
		return out
	}
	var ev api.Event
	var err error
	if len(r.Event) > 0 {
		err = json.Unmarshal(r.Event, &ev)
	}
	if err == nil {
		switch r.Type {
		case "event":
			err = p.event(ctx, ev)
		case "tick":
			err = p.tick(ctx, time.Now())
		default:
			a := r.Args
			if strings.TrimSpace(r.Text) != "" {
				a = strings.Fields(r.Text)
				if len(a) > 0 && (strings.HasSuffix(a[0], "pmcaptcha") || strings.HasSuffix(a[0], "pmc")) {
					a = a[1:]
				}
			}
			out.Text, err = p.command(ctx, a, ev)
		}
	}
	if err != nil {
		out.Error = err.Error()
	}
	return out
}
func (p *Plugin) Tick(now time.Time) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.tick(context.Background(), now)
}
func (p *Plugin) tick(ctx context.Context, now time.Time) error {
	if p.closed || !p.config.Enabled || !p.config.Captcha {
		return nil
	}
	var errs []error
	for id, s := range p.states {
		if p.config.Timeout > 0 && !now.Before(s.Started.Add(time.Duration(p.config.Timeout)*time.Second)) {
			errs = append(errs, p.fail(ctx, id, "timeout"))
		}
	}
	return errors.Join(errs...)
}
func idText(id int64) string { return strconv.FormatInt(id, 10) }
func (p *Plugin) info(ctx context.Context, id int64) (api.Result, error) {
	r, e := p.call(ctx, api.Call{Method: "user_info", Target: idText(id)})
	if e == nil && (r.Entity == nil || id <= 0 || r.Entity.ID != idText(id)) {
		e = errors.New("无法读取用户信息")
	}
	return r, e
}
func (p *Plugin) action(ctx context.Context, id int64, action string) error {
	_, e := p.call(ctx, api.Call{Method: "chat_action", Target: func() string {
		if id == 0 {
			return "me"
		}
		return idText(id)
	}(), Action: action, FolderID: p.config.Folder})
	return e
}
func (p *Plugin) normalize(ctx context.Context) error {
	if p.normalized || p.config.Folder <= 1 || !contains(p.config.Pass, "add_folder") {
		return nil
	}
	if e := p.action(ctx, 0, "folder_normalize"); e != nil {
		return e
	}
	p.normalized = true
	return nil
}
func (p *Plugin) send(ctx context.Context, id int64, text string, img []byte) (int, error) {
	method := "send"
	if len(img) > 0 {
		method = "send_photo"
	}
	r, e := p.call(ctx, api.Call{Method: method, Target: idText(id), Text: text, HTML: true, Bytes: img, Filename: "captcha.png", MimeType: "image/png"})
	if e != nil {
		return 0, e
	}
	if r.MessageID <= 0 {
		return 0, errors.New("发送验证码未返回消息 ID")
	}
	now := time.Now()
	for k, v := range p.outgoing {
		if !v.After(now) {
			delete(p.outgoing, k)
		}
	}
	if len(p.outgoing) >= 4096 {
		for k := range p.outgoing {
			delete(p.outgoing, k)
			break
		}
	}
	p.outgoing[fmt.Sprintf("%d:%d", id, r.MessageID)] = now.Add(10 * time.Minute)
	return r.MessageID, nil
}
func (p *Plugin) cleanup(ctx context.Context, id int64, s *state) error {
	if s == nil || len(s.Messages) == 0 {
		return nil
	}
	var errs []error
	for start := 0; start < len(s.Messages); start += 100 {
		_, e := p.call(ctx, api.Call{Method: "delete", Target: idText(id), IDs: s.Messages[start:min(start+100, len(s.Messages))]})
		errs = append(errs, e)
	}
	return errors.Join(errs...)
}
func contains(a []string, s string) bool {
	for _, v := range a {
		if v == s {
			return true
		}
	}
	return false
}
