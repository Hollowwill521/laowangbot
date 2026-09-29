// Package bh implements persistent Emby account-expiry checks.
package bh

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	api "github.com/OrionG-hub/laowangbot/pkg/pluginapi"
	"github.com/robfig/cron/v3"
	"math/rand/v2"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Task struct {
	ID         string `json:"id"`
	Cron       string `json:"cron"`
	Bot        string `json:"botUsername"`
	Mode       string `json:"mode"`
	Command    string `json:"command"`
	Regex      string `json:"regex,omitempty"`
	Random     bool   `json:"randomDelay"`
	RandomMin  *int64 `json:"randomMin,omitempty"`
	RandomMax  *int64 `json:"randomMax,omitempty"`
	ExpireDays int    `json:"expireDays,omitempty"`
	Disabled   bool   `json:"disabled,omitempty"`
	Remark     string `json:"remark,omitempty"`
	LastRun    string `json:"lastRunAt,omitempty"`
	LastActive string `json:"lastActiveTime,omitempty"`
	LastDays   int    `json:"lastExpireDays,omitempty"`
	LastDate   string `json:"lastExpireDate,omitempty"`
	LastResult string `json:"lastResult,omitempty"`
}
type Notify struct {
	Token   string `json:"botToken"`
	Target  string `json:"targetId"`
	Warning int    `json:"warningDays"`
	Danger  int    `json:"dangerDays"`
}
type DB struct {
	Seq    string `json:"seq"`
	Tasks  []Task `json:"tasks"`
	Notify Notify `json:"notify"`
}
type batch struct {
	manual      bool
	event       api.Event
	total, done int
	safe        int
	records     map[string]string
	lines       []string
	alert       bool
}
type job struct {
	task    Task
	due     time.Time
	batch   *batch
	ctx     context.Context
	cancel  context.CancelFunc
	started bool
}
type Plugin struct {
	mu         sync.Mutex
	host       api.Host
	dir        string
	db         DB
	ctx        context.Context
	cancel     context.CancelFunc
	wg         sync.WaitGroup
	jobs       map[string]*job
	next       map[string]time.Time
	HTTP       *http.Client
	Poll       time.Duration
	ResultWait time.Duration
}

var parser = cron.NewParser(cron.SecondOptional | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)

//go:embed manifest.json
var manifestJSON string

func ManifestJSON() string                                                 { return manifestJSON }
func Open(ctx context.Context, h api.Host, dir string) (api.Plugin, error) { return New(ctx, h, dir) }
func New(ctx context.Context, h api.Host, dir string) (*Plugin, error) {
	ctx, cancel := context.WithCancel(ctx)
	p := &Plugin{host: h, dir: dir, ctx: ctx, cancel: cancel, jobs: map[string]*job{}, next: map[string]time.Time{}, HTTP: &http.Client{Timeout: 20 * time.Second}, Poll: time.Second, ResultWait: 12 * time.Second, db: DB{Seq: "0", Tasks: []Task{}, Notify: Notify{Warning: 7, Danger: 3}}}
	if e := os.MkdirAll(dir, 0700); e != nil {
		cancel()
		return nil, e
	}
	if e := api.LoadJSON(filepath.Join(dir, "baohao_config.json"), &p.db); e != nil && !errors.Is(e, os.ErrNotExist) {
		cancel()
		return nil, e
	}
	if e := validateDB(p.db); e != nil {
		cancel()
		return nil, fmt.Errorf("保号配置无效: %w", e)
	}
	p.rebuild(time.Now())
	p.wg.Add(1)
	go p.worker()
	return p, nil
}
func (p *Plugin) Close()       { p.cancel(); p.wg.Wait() }
func (p *Plugin) snapshot() DB { p.mu.Lock(); defer p.mu.Unlock(); return clone(p.db) }
func clone(d DB) DB            { d.Tasks = append([]Task(nil), d.Tasks...); return d }
func (p *Plugin) commit(d DB) error {
	if e := validateDB(d); e != nil {
		return e
	}
	if e := api.SaveJSON(filepath.Join(p.dir, "baohao_config.json"), d); e != nil {
		return e
	}
	p.db = d
	p.rebuild(time.Now())
	return nil
}
func (p *Plugin) rebuild(now time.Time) {
	old := p.next
	p.next = map[string]time.Time{}
	for _, t := range p.db.Tasks {
		if !t.Disabled {
			if n, ok := old[t.Cron]; ok {
				p.next[t.Cron] = n
			} else if s, e := parser.Parse(t.Cron); e == nil {
				p.next[t.Cron] = s.Next(now)
			}
		}
	}
}
func (p *Plugin) Handle(ctx context.Context, r api.Request) api.Response {
	if r.Type == "tick" {
		p.Tick(time.Now())
		return api.Response{Version: 1}
	}
	if r.Type == "event" {
		return api.Response{Version: 1}
	}
	var a []string
	var e error
	if strings.TrimSpace(r.Text) != "" {
		a, e = splitArgs(r.Text)
		if len(a) > 0 && (a[0] == "bh" || strings.HasSuffix(a[0], "bh")) {
			a = a[1:]
		}
	} else {
		a = append([]string(nil), r.Args...)
	}
	if e != nil {
		return api.Response{Version: 1, Error: e.Error()}
	}
	var ev api.Event
	if len(r.Event) > 0 {
		if e = json.Unmarshal(r.Event, &ev); e != nil {
			return api.Response{Version: 1, Error: "命令事件无效"}
		}
	}
	text, e := p.command(ctx, a, ev)
	if e != nil {
		return api.Response{Version: 1, Error: e.Error()}
	}
	panel := len(a) == 0 || a[0] == "help" || a[0] == "list" || a[0] == "ls" || a[0] == "info" || a[0] == "config"
	if panel {
		prefix := "."
		if fields := strings.Fields(r.Text); len(fields) > 0 && strings.HasSuffix(fields[0], "bh") {
			prefix = strings.TrimSuffix(fields[0], "bh")
		}
		text = api.PanelHTML(text, "bh", prefix)
	}
	return api.Response{Version: 1, Text: text, HTML: panel}
}
func (p *Plugin) enqueue(tasks []Task, manual bool, ev api.Event, now time.Time) error {
	if len(tasks) == 0 {
		return errors.New("没有可执行的任务")
	}
	for _, t := range tasks {
		if p.jobs[t.ID] != nil {
			return fmt.Errorf("任务 %s 正在执行或等待，未重复入队", t.ID)
		}
	}
	b := &batch{manual: manual, event: ev, total: len(tasks), records: map[string]string{}}
	for _, t := range tasks {
		due := now
		if !manual && t.Random {
			lo, hi := randomBounds(t)
			due = due.Add(time.Duration(lo+rand.Int64N(hi-lo+1)) * time.Millisecond)
		}
		ctx, cancel := context.WithCancel(p.ctx)
		p.jobs[t.ID] = &job{task: t, due: due, batch: b, ctx: ctx, cancel: cancel}
	}
	return nil
}
func (p *Plugin) Tick(now time.Time) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for c, n := range p.next {
		if !now.Before(n) {
			var ts []Task
			for _, t := range p.db.Tasks {
				if t.Cron == c && !t.Disabled && p.jobs[t.ID] == nil {
					ts = append(ts, t)
				}
			}
			_ = p.enqueue(ts, false, api.Event{}, now)
			s, e := parser.Parse(c)
			if e == nil {
				p.next[c] = s.Next(now)
			}
		}
	}
}
func (p *Plugin) worker() {
	defer p.wg.Done()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-p.ctx.Done():
			return
		case <-ticker.C:
		}
		p.mu.Lock()
		var picked *job
		for _, j := range p.jobs {
			if !j.started && (j.ctx.Err() != nil || !time.Now().Before(j.due)) && (picked == nil || j.due.Before(picked.due)) {
				picked = j
			}
		}
		if picked != nil {
			picked.started = true
		}
		p.mu.Unlock()
		if picked == nil {
			continue
		}
		result := p.execute(picked.ctx, picked.task)
		p.finish(picked, result)
	}
}
func (p *Plugin) finish(j *job, r checkResult) {
	p.mu.Lock()
	delete(p.jobs, j.task.ID)
	cancelled := j.ctx.Err() != nil
	storageFailed := false
	j.cancel()
	b := j.batch
	b.done++
	t := j.task
	if r.Err == nil {
		t.LastActive = r.Active
		if r.Days > 0 {
			t.LastDays = r.Days
			t.LastDate = ""
		} else if r.Date != "" {
			t.LastDate = r.Date
			t.LastDays = 0
		}
		t.LastResult = "成功抓取时间: " + r.Active
	} else {
		t.LastResult = r.Err.Error()
	}
	t.LastRun = strconv.FormatInt(time.Now().UnixMilli(), 10)
	if cancelled {
		t.LastResult = "任务已取消"
		r.Err = context.Canceled
	} else {
		d := clone(p.db)
		for i, v := range d.Tasks {
			if v.ID == t.ID {
				d.Tasks[i] = t
			}
		}
		if e := p.commit(d); e != nil {
			t.LastResult += "；结果保存失败: " + e.Error()
			r.Err = e
			storageFailed = true
		}
	}
	if !cancelled {
		if b.records == nil {
			b.records = map[string]string{}
		}
		b.records[t.ID] = t.LastRun
	}
	line, alert := reportLine(t, r.Err, p.db.Notify, time.Now())
	if storageFailed {
		alert = true
		line += "；结果保存失败"
	}
	if alert {
		b.lines = append(b.lines, line)
	} else {
		b.safe++
	}
	b.alert = b.alert || alert
	report := fmt.Sprintf("保号检测报告（%d/%d）\n%s\n\n%s", b.done, b.total, time.Now().Format("2006/01/02 15:04:05"), strings.Join(b.lines, "\n")+fmt.Sprintf("\n安全账号: %d 个（已隐藏）", b.safe))
	done := b.done == b.total
	notify := p.db.Notify
	p.mu.Unlock()
	if done && (b.manual || b.alert) {
		status := p.notify(p.ctx, notify, report)
		report += "\n\n" + status
		if !strings.HasPrefix(status, "报告已通过 Bot 发送") {
			p.recordDelivery(b.records, status)
		}
	}
	if b.manual {
		var err error
		for attempt := 0; attempt < 3; attempt++ {
			err = p.progress(p.ctx, b.event, report)
			if err == nil || p.ctx.Err() != nil {
				break
			}
			_ = sleep(p.ctx, 100*time.Millisecond)
		}
		if err != nil {
			p.recordDelivery(b.records, "原消息报告更新失败: "+err.Error())
		}
	}
}
func (p *Plugin) cancelIDs(ids map[string]bool) {
	for id := range ids {
		if j := p.jobs[id]; j != nil {
			j.cancel()
		}
	}
}
func sleep(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
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

// Delivery failures remain visible through info, without overwriting newer runs.
func (p *Plugin) recordDelivery(records map[string]string, message string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	d := clone(p.db)
	for i := range d.Tasks {
		if run, ok := records[d.Tasks[i].ID]; ok && run == d.Tasks[i].LastRun {
			d.Tasks[i].LastResult += "；" + message
		}
	}
	_ = p.commit(d)
}
