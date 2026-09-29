// Package qdsg ports the Telegram sign-in workflows of qdsg.ts without Node.
package qdsg

import (
	"context"
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
	ID         string  `json:"id"`
	Cron       string  `json:"cron"`
	Bot        string  `json:"botUsername"`
	ChatID     string  `json:"botChatId,omitempty"`
	Command    string  `json:"command"`
	Secondary  string  `json:"secondaryCommand,omitempty"`
	Mode       string  `json:"mode"`
	SendStart  bool    `json:"sendStart"`
	Wait       int     `json:"waitTime"`
	Random     bool    `json:"randomDelay"`
	RandomMin  int     `json:"randomMin"`
	RandomMax  int     `json:"randomMax"`
	Created    string  `json:"createdAt"`
	LastRun    string  `json:"lastRunAt,omitempty"`
	LastResult string  `json:"lastResult,omitempty"`
	LastError  string  `json:"lastError,omitempty"`
	Disabled   bool    `json:"disabled"`
	Remark     string  `json:"remark,omitempty"`
	Display    string  `json:"display,omitempty"`
	AI         bool    `json:"useAI"`
	Provider   string  `json:"aiProvider,omitempty"`
	Prompt     string  `json:"aiPrompt,omitempty"`
	Steps      int     `json:"maxAiSteps"`
	Retry      int     `json:"retryCount"`
	Interval   float64 `json:"retryInterval"`
	AITarget   string  `json:"aiTarget,omitempty"`
}
type Provider struct {
	ID    string `json:"id"`
	Base  string `json:"baseUrl"`
	Model string `json:"model"`
	Key   string `json:"apiKey"`
}
type AIConfig struct {
	OpenAIKey   string     `json:"openai_key"`
	OpenAIBase  string     `json:"openai_base_url"`
	OpenAIModel string     `json:"openai_model"`
	GeminiKey   string     `json:"gemini_key"`
	GeminiBase  string     `json:"gemini_base_url"`
	GeminiModel string     `json:"gemini_model"`
	Default     string     `json:"default_provider"`
	Prompt      string     `json:"default_prompt"`
	Custom      []Provider `json:"custom_providers"`
}
type Notify struct {
	Enabled bool   `json:"enabled"`
	Target  string `json:"target"`
}
type DB struct {
	Seq    string   `json:"seq"`
	Tasks  []Task   `json:"tasks"`
	Notify Notify   `json:"notify"`
	AI     AIConfig `json:"aiConfig"`
	Bucket string   `json:"cfBucketId,omitempty"`
}
type jobState struct{ Started, Cancelled bool }
type job struct {
	ID       string
	Attempt  int
	Due      time.Time
	Progress *runProgress
	State    *jobState
}
type Plugin struct {
	mu         sync.Mutex
	host       api.Host
	dir        string
	db         DB
	ctx        context.Context
	cancel     context.CancelFunc
	wg         sync.WaitGroup
	queue      chan job
	pending    map[string]job
	jobs       map[string]job
	running    map[string]bool
	next       map[string]time.Time
	HTTP       *http.Client
	Poll       time.Duration
	ResultWait time.Duration
}

var parser = cron.NewParser(cron.Second | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)

func New(ctx context.Context, h api.Host, dir string) (*Plugin, error) {
	ctx, cancel := context.WithCancel(ctx)
	p := &Plugin{host: h, dir: dir, ctx: ctx, cancel: cancel, queue: make(chan job, 64), pending: map[string]job{}, jobs: map[string]job{}, running: map[string]bool{}, next: map[string]time.Time{}, HTTP: &http.Client{Timeout: 60 * time.Second}, Poll: 300 * time.Millisecond, ResultWait: 12 * time.Second}
	p.db = DB{Seq: "0", Notify: Notify{true, "me"}, AI: AIConfig{OpenAIBase: "https://api.openai.com", OpenAIModel: "gpt-4o", GeminiBase: "https://generativelanguage.googleapis.com", GeminiModel: "gemini-2.0-flash", Default: "openai", Prompt: "请识别内容，只返回正确答案或一个匹配的按钮文本。"}}
	if err := os.MkdirAll(dir, 0700); err != nil {
		cancel()
		return nil, err
	}
	if err := api.LoadJSON(filepath.Join(dir, "signin_config.json"), &p.db); err != nil && !errors.Is(err, os.ErrNotExist) {
		cancel()
		return nil, err
	}
	for _, t := range p.db.Tasks {
		if s, e := parser.Parse(t.Cron); e == nil {
			p.next[t.ID] = s.Next(time.Now())
		}
	}
	p.wg.Add(1)
	go p.worker()
	return p, nil
}
func (p *Plugin) Close()      { p.cancel(); p.wg.Wait() }
func (p *Plugin) save() error { return api.SaveJSON(filepath.Join(p.dir, "signin_config.json"), p.db) }
func (p *Plugin) snapshot() DB {
	p.mu.Lock()
	defer p.mu.Unlock()
	d := p.db
	d.Tasks = append([]Task(nil), d.Tasks...)
	d.AI.Custom = append([]Provider(nil), d.AI.Custom...)
	return d
}
func (p *Plugin) enqueue(id string, random bool) error { return p.enqueueProgress(id, random, nil) }
func (p *Plugin) enqueueProgress(id string, random bool, progress *runProgress) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.running[id] {
		return fmt.Errorf("任务 %s 已在队列中", id)
	}
	for _, t := range p.db.Tasks {
		if t.ID == id {
			if t.Disabled {
				return errors.New("任务已禁用")
			}
			if len(p.running) >= 64 {
				return errors.New("队列已满（64）")
			}
			due := time.Now()
			if random && t.Random {
				hi := t.RandomMax
				if hi == 0 {
					hi = 60000
				}
				if hi < t.RandomMin || t.RandomMin < 0 || hi > 31536000000 {
					return errors.New("随机范围无效")
				}
				due = due.Add(time.Duration(t.RandomMin+rand.IntN(hi-t.RandomMin+1)) * time.Millisecond)
			}
			j := job{ID: id, Due: due, Progress: progress, State: &jobState{}}
			p.pending[id] = j
			p.jobs[id] = j
			p.running[id] = true
			return nil
		}
	}
	return errors.New("任务不存在")
}
func (p *Plugin) Tick(now time.Time) {
	d := p.snapshot()
	for _, t := range d.Tasks {
		p.mu.Lock()
		n := p.next[t.ID]
		p.mu.Unlock()
		if !t.Disabled && !n.IsZero() && !now.Before(n) {
			_ = p.enqueue(t.ID, true)
			if s, e := parser.Parse(t.Cron); e == nil {
				p.mu.Lock()
				p.next[t.ID] = s.Next(now)
				p.mu.Unlock()
			}
		}
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	for id, j := range p.pending {
		if !now.Before(j.Due) {
			select {
			case p.queue <- j:
				delete(p.pending, id)
			default:
			}
		}
	}
}
func (p *Plugin) worker() {
	defer p.wg.Done()
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		select {
		case <-p.ctx.Done():
			return
		case n := <-tick.C:
			p.Tick(n)
		case j := <-p.queue:
			p.run(j)
		}
	}
}
func (p *Plugin) run(j job) {
	p.mu.Lock()
	if j.State != nil {
		if j.State.Cancelled {
			p.mu.Unlock()
			return
		}
		j.State.Started = true
	}
	p.mu.Unlock()
	var t Task
	found := false
	for _, v := range p.snapshot().Tasks {
		if v.ID == j.ID && !v.Disabled {
			t = v
			found = true
			break
		}
	}
	if !found {
		j.Progress.update(p, j.ID, "已取消（任务删除或禁用）")
		p.mu.Lock()
		delete(p.running, j.ID)
		delete(p.jobs, j.ID)
		p.mu.Unlock()
		return
	}
	j.Progress.update(p, j.ID, "执行中")
	ctx, cancel := context.WithTimeout(p.ctx, 4*time.Minute)
	result, err := p.Execute(ctx, t)
	cancel()
	p.mu.Lock()
	retryAllowed := false
	for i := range p.db.Tasks {
		v := &p.db.Tasks[i]
		if v.ID == j.ID && v.Created == t.Created {
			retryAllowed = !v.Disabled && (j.State == nil || !j.State.Cancelled)
			v.LastRun = strconv.FormatInt(time.Now().UnixMilli(), 10)
			if err != nil {
				v.LastError = err.Error()
			} else {
				v.LastResult = result
				v.LastError = ""
			}
		}
	}
	saveErr := p.save()
	retrying := err != nil && retryAllowed && j.Attempt < t.Retry && p.ctx.Err() == nil
	if retrying {
		interval := t.Interval
		if interval <= 0 {
			interval = 5
		}
		p.pending[j.ID] = job{ID: j.ID, Attempt: j.Attempt + 1, Due: time.Now().Add(time.Duration(interval * float64(time.Minute))), Progress: j.Progress, State: j.State}
		if j.State != nil {
			j.State.Started = false
		}
		p.jobs[j.ID] = p.pending[j.ID]
	} else {
		delete(p.running, j.ID)
		delete(p.jobs, j.ID)
	}
	notify := p.db.Notify
	p.mu.Unlock()
	if saveErr != nil {
		err = fmt.Errorf("保存执行结果失败: %w", saveErr)
	}
	if j.Progress != nil {
		status := "完成: " + result
		if err != nil {
			status = "失败: " + err.Error()
		}
		if retrying {
			status += fmt.Sprintf("；等待第 %d 次重试", j.Attempt+1)
		}
		j.Progress.update(p, j.ID, status)
	}
	if j.Progress == nil && notify.Enabled && p.ctx.Err() == nil {
		msg := fmt.Sprintf("qdsg %s @%s: %s", t.ID, t.Bot, result)
		if err != nil {
			msg = fmt.Sprintf("qdsg %s @%s 失败: %v", t.ID, t.Bot, err)
		}
		ctx, cancel := context.WithTimeout(p.ctx, 20*time.Second)
		defer cancel()
		_, _ = p.host.Call(ctx, api.Call{Method: "send", Target: notify.Target, Text: msg})
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
	a := r.Args
	if len(a) == 0 {
		a = strings.Fields(r.Text)
		if len(a) > 0 && strings.HasSuffix(a[0], "qdsg") {
			a = a[1:]
		}
	}
	out, err := p.command(ctx, a, r.Event)
	if err != nil {
		return api.Response{Version: 1, Error: err.Error()}
	}
	return api.Response{Version: 1, Text: out}
}
func sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
func fingerprint(m api.Message) string {
	b, _ := json.Marshal(struct {
		Text    string
		Buttons []api.Button
		Media   bool
	}{m.Text, m.Buttons, m.HasMedia})
	return string(b)
}
