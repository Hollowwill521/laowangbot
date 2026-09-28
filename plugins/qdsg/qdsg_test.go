package qdsg

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	api "github.com/OrionG-hub/laowangbot/pkg/pluginapi"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeHost struct {
	mu       sync.Mutex
	messages []api.Message
	calls    []api.Call
	action   func(api.Call, []api.Message) []api.Message
}

func (h *fakeHost) Call(ctx context.Context, c api.Call) (api.Result, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.calls = append(h.calls, c)
	switch c.Method {
	case "history", "messages":
		return api.Result{Messages: append([]api.Message(nil), h.messages...)}, nil
	case "send", "click", "webview_data":
		if h.action != nil {
			h.messages = h.action(c, h.messages)
		}
	case "download":
		return api.Result{Bytes: []byte("image"), MimeType: "image/png"}, nil
	case "webview":
		return api.Result{URL: c.URL}, nil
	}
	return api.Result{}, nil
}
func testPlugin(t *testing.T, h api.Host) *Plugin {
	t.Helper()
	p, e := New(context.Background(), h, t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	p.Poll = time.Millisecond
	p.ResultWait = 20 * time.Millisecond
	t.Cleanup(p.Close)
	return p
}
func TestArithmetic(t *testing.T) {
	for input, want := range map[string]string{"请计算：94 + 3 = ?": "97", "算式: -4 × 2": "-8", "5 ÷ 2 =": "2.5", "会员有效期至：2026/11/11": "", "2 / 0 =": ""} {
		got, _ := arithmetic(input)
		if got != want {
			t.Fatalf("%q: %q", input, got)
		}
	}
}
func TestParseAndSelect(t *testing.T) {
	v, e := parseAdd(strings.Fields("now @bot inline 签到|确认 ai local steps 3 retry 2 interval 5 2000 random 1-50 备注"))
	if e != nil || v.Mode != "inline_button" || v.Secondary != "确认" || v.RandomMin != 60000 || v.Retry != 2 || !v.SendStart {
		t.Fatalf("%+v %v", v, e)
	}
	v, e = parseAdd([]string{"now", "@bot"})
	if e != nil || !v.Random || v.Command != "签到" {
		t.Fatal(v, e)
	}
	ts := []Task{{ID: "1", Bot: "foo", Remark: "西瓜"}, {ID: "4", Bot: "bar"}}
	if len(selectIDs(ts, []string{"4-1"})) != 2 || len(selectIDs(ts, []string{"西瓜"})) != 1 {
		t.Fatal("selection")
	}
	if _, _, e = randomRange("4-1"); e == nil {
		t.Fatal("invalid range")
	}
}
func TestCommandsPersistence(t *testing.T) {
	p := testPlugin(t, &fakeHost{})
	commands := []string{"add now @bot text /checkin", "edit 1 retry 2", "edit 1 aitype text", "edit 1 random 1-2", "edit 1 cron 0 0 8 * * *", "disable 1", "enable 1", "notify off", "notify me", "cfbucket abcdef123", "aiconfig set openai_key secret", "aiconfig addcustom custom https://example.invalid m secret", "aiconfig set provider custom", "list", "reload", "reorder", "aiconfig rmcustom custom"}
	for _, c := range commands {
		if _, e := p.command(context.Background(), strings.Fields(c), nil); e != nil {
			t.Fatalf("%s: %v", c, e)
		}
	}
	s, e := p.command(context.Background(), []string{"aiconfig", "list"}, nil)
	if e != nil || strings.Contains(s, "secret") {
		t.Fatal(s, e)
	}
	var d DB
	if e = api.LoadJSON(filepath.Join(p.dir, "signin_config.json"), &d); e != nil || d.Tasks[0].Retry != 2 {
		t.Fatal(d, e)
	}
	if _, e = p.command(context.Background(), []string{"rm", "all"}, nil); e != nil {
		t.Fatal(e)
	}
	if len(p.snapshot().Tasks) != 0 {
		t.Fatal("rm")
	}
}
func TestFreshnessAndText(t *testing.T) {
	h := &fakeHost{messages: []api.Message{{ID: 1, Text: "签到成功"}}}
	p := testPlugin(t, h)
	if _, e := p.Execute(context.Background(), Task{Bot: "bot", Mode: "text", Command: "/checkin"}); e == nil {
		t.Fatal("old success accepted")
	}
	h.action = func(c api.Call, ms []api.Message) []api.Message {
		return append(ms, api.Message{ID: 2, Text: "签到成功：10积分"})
	}
	if _, e := p.Execute(context.Background(), Task{Bot: "bot", Mode: "text", Command: "/checkin"}); e != nil {
		t.Fatal(e)
	}
}
func TestMathIntegration(t *testing.T) {
	h := &fakeHost{messages: []api.Message{{ID: 1, Text: "菜单", Buttons: []api.Button{{Text: "签到", Data: []byte("a")}}}}}
	h.action = func(c api.Call, ms []api.Message) []api.Message {
		if c.Method == "click" {
			return []api.Message{{ID: 1, Text: "请计算：94 + 3 = ?"}}
		}
		if c.Method == "send" && c.Text == "97" {
			return []api.Message{{ID: 2, Text: "签到成功"}}
		}
		return ms
	}
	p := testPlugin(t, h)
	if _, e := p.Execute(context.Background(), Task{Bot: "bot", Mode: "math", Command: "签到"}); e != nil {
		t.Fatal(e)
	}
}
func TestInlineTwoSteps(t *testing.T) {
	h := &fakeHost{messages: []api.Message{{ID: 1, Text: "菜单", Buttons: []api.Button{{Text: "签到", Data: []byte("a")}}}}}
	n := 0
	h.action = func(c api.Call, ms []api.Message) []api.Message {
		n++
		if n == 1 {
			return []api.Message{{ID: 1, Text: "确认", Buttons: []api.Button{{Text: "确认", Data: []byte("b")}}}}
		}
		return []api.Message{{ID: 1, Text: "签到成功"}}
	}
	p := testPlugin(t, h)
	if _, e := p.Execute(context.Background(), Task{Bot: "bot", Mode: "inline_button", Command: "签到", Secondary: "确认"}); e != nil {
		t.Fatal(e)
	}
}
func TestImageChoiceIntegration(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Error(r.URL.Path)
		}
		fmt.Fprint(w, `{"choices":[{"message":{"content":"苹果"}}]}`)
	}))
	defer server.Close()
	h := &fakeHost{}
	h.action = func(c api.Call, ms []api.Message) []api.Message {
		if c.Method == "send" {
			return []api.Message{{ID: 1, Text: "选物品", HasMedia: true, Buttons: []api.Button{{Text: "苹果", Data: []byte("a")}, {Text: "梨", Data: []byte("b")}}}}
		}
		return []api.Message{{ID: 2, Text: "签到成功"}}
	}
	p := testPlugin(t, h)
	p.mu.Lock()
	p.db.AI.OpenAIBase = server.URL
	p.db.AI.OpenAIKey = "test"
	p.mu.Unlock()
	if _, e := p.Execute(context.Background(), Task{Bot: "bot", Mode: "image_choice", Command: "/checkin"}); e != nil {
		t.Fatal(e)
	}
}
func TestAIProviders(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "generateContent") {
			fmt.Fprint(w, `{"candidates":[{"content":{"parts":[{"text":"答案"}]}}]}`)
		} else {
			fmt.Fprint(w, `{"choices":[{"message":{"content":"答案"}}]}`)
		}
	}))
	defer server.Close()
	p := testPlugin(t, &fakeHost{})
	p.mu.Lock()
	p.db.AI.GeminiBase = server.URL
	p.db.AI.GeminiKey = "key"
	p.db.AI.Custom = []Provider{{"custom", server.URL, "model", "key"}}
	p.mu.Unlock()
	for _, provider := range []string{"gemini", "custom"} {
		s, e := p.recognize(context.Background(), Task{Provider: provider}, api.Message{Text: "问题"}, nil)
		if e != nil || s != "答案" {
			t.Fatal(s, e)
		}
	}
	if _, e := p.recognize(context.Background(), Task{Provider: "local"}, api.Message{Text: "text"}, nil); e == nil {
		t.Fatal("local text accepted")
	}
}
func TestChoiceAndSequence(t *testing.T) {
	bs := []api.Button{{Text: "苹果"}, {Text: "梨"}}
	if _, e := matchAnswer("苹果和梨", bs, true); e == nil {
		t.Fatal("ambiguous choice")
	}
	b, e := matchAnswer("“苹果”。", bs, true)
	if e != nil || b.Text != "苹果" {
		t.Fatal(b, e)
	}
	m := api.Message{Text: "从右往左 目标序列： 🍎 🍐 🍎", Buttons: []api.Button{{Text: "🍎"}, {Text: "🍐"}}}
	if strings.Join(sequence(m), "") != "🍎🍐🍎" {
		t.Fatal(sequence(m))
	}
}
func TestCFMailboxIntegration(t *testing.T) {
	h := &fakeHost{}
	h.action = func(c api.Call, ms []api.Message) []api.Message {
		if c.Method == "webview_data" {
			return []api.Message{{ID: 2, Text: "签到成功"}}
		}
		return ms
	}
	p := testPlugin(t, h)
	state := "eyJub25jZSI6ImFiY2RlZjEyMyIsInB1cnBvc2UiOiJjaGVja2luIn0.signature"
	done := make(chan struct{})
	go func() {
		defer close(done)
		path := filepath.Join(p.dir, "cfbox", "pending-abcdef123.json")
		for i := 0; i < 1000; i++ {
			if _, e := os.Stat(path); e == nil {
				_ = api.SaveJSON(filepath.Join(p.dir, "cfbox", "solved-abcdef123.json"), map[string]any{"ok": true, "proof": "test-proof"})
				return
			}
			time.Sleep(time.Millisecond)
		}
	}()
	s, e := p.solveCF(context.Background(), Task{Bot: "bot"}, "https://example.invalid/?state="+state, "验证", true, newCursor(nil), 3*time.Second)
	<-done
	if e != nil || s != "签到成功" {
		t.Fatal(s, e)
	}
	if _, e = os.Stat(filepath.Join(p.dir, "cfbox", "pending-abcdef123.json")); !errors.Is(e, os.ErrNotExist) {
		t.Fatal("mailbox not cleaned")
	}
}
func TestQueueBoundAndRetry(t *testing.T) {
	p := testPlugin(t, &fakeHost{})
	p.mu.Lock()
	p.db.Tasks = []Task{{ID: "1", Bot: "bot", Mode: "text", Command: "x", Retry: 1, Interval: 5}}
	p.mu.Unlock()
	if e := p.enqueue("1", false); e != nil {
		t.Fatal(e)
	}
	if e := p.enqueue("1", false); e == nil {
		t.Fatal("duplicate queued")
	}
	p.mu.Lock()
	delete(p.pending, "1")
	p.mu.Unlock()
	p.run(job{ID: "1"})
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.pending["1"].Attempt != 1 || p.db.Tasks[0].LastError == "" {
		t.Fatal("retry/result not saved")
	}
}
func TestHandleAndValidation(t *testing.T) {
	p := testPlugin(t, &fakeHost{})
	r := p.Handle(context.Background(), api.Request{Args: []string{"add", "now", "bot"}})
	if r.Error != "" {
		t.Fatal(r)
	}
	for _, args := range [][]string{{"edit", "1", "steps", "0"}, {"edit", "1", "wait", "-1"}, {"edit", "1", "aitype", "no"}, {"cfbucket", "../x"}, {"now", "404"}, {"test"}, {"unknown"}} {
		r = p.Handle(context.Background(), api.Request{Args: args})
		if r.Error == "" {
			t.Fatal(args)
		}
	}
	b, _ := json.Marshal(api.Event{ChatID: "1", ReplyToID: 2})
	_ = b
	p.Handle(context.Background(), api.Request{Type: "tick"})
	p.Handle(context.Background(), api.Request{Type: "event"})
}
func TestReplyAndMoonIntegration(t *testing.T) {
	for _, mode := range []string{"reply_button", "moon"} {
		t.Run(mode, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				fmt.Fprint(w, `{"choices":[{"message":{"content":"正确"}}]}`)
			}))
			defer server.Close()
			h := &fakeHost{messages: []api.Message{{ID: 1, Text: "菜单", Buttons: []api.Button{{Text: "签到", Data: []byte("entry")}}}}}
			stage := 0
			h.action = func(c api.Call, ms []api.Message) []api.Message {
				stage++
				if stage == 1 {
					return []api.Message{{ID: 2, Text: "问题", Buttons: []api.Button{{Text: "正确", Data: []byte("answer")}}}}
				}
				return []api.Message{{ID: 3, Text: "签到成功"}}
			}
			p := testPlugin(t, h)
			p.mu.Lock()
			p.db.AI.OpenAIKey = "test"
			p.db.AI.OpenAIBase = server.URL
			p.mu.Unlock()
			if _, e := p.Execute(context.Background(), Task{Bot: "bot", Mode: mode, Command: "签到", AI: true, Steps: 2}); e != nil {
				t.Fatal(e)
			}
		})
	}
}
func TestXiguaIntegration(t *testing.T) {
	h := &fakeHost{messages: []api.Message{{ID: 1, Text: "菜单", Buttons: []api.Button{{Text: "签到", Data: []byte("e")}}}}}
	stage := 0
	h.action = func(c api.Call, ms []api.Message) []api.Message {
		stage++
		if stage == 1 {
			return []api.Message{{ID: 2, Text: "从右往左 目标序列： 🍎 🍐", Buttons: []api.Button{{Text: "🍎", Data: []byte("a"), Column: 0}, {Text: "🍐", Data: []byte("b"), Column: 1}}}}
		}
		if stage == 2 {
			if c.Column != 1 {
				t.Error("wrong direction")
			}
			return ms
		}
		return []api.Message{{ID: 3, Text: "签到成功"}}
	}
	p := testPlugin(t, h)
	if _, e := p.Execute(context.Background(), Task{Bot: "bot", Mode: "xigua_sequence", Command: "签到"}); e != nil {
		t.Fatal(e)
	}
}
func TestAppCFDirectBotConfirmation(t *testing.T) {
	h := &fakeHost{messages: []api.Message{{ID: 1, Buttons: []api.Button{{Text: "签到", Kind: "url", URL: "https://example.invalid/checkin"}}}}}
	p := testPlugin(t, h)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 1000; i++ {
			es, _ := os.ReadDir(filepath.Join(p.dir, "cfbox"))
			if len(es) > 0 {
				h.mu.Lock()
				h.messages = []api.Message{{ID: 2, Text: "签到成功"}}
				h.mu.Unlock()
				return
			}
			time.Sleep(time.Millisecond)
		}
	}()
	if _, e := p.Execute(context.Background(), Task{Bot: "bot", Mode: "app_cf", Command: "签到"}); e != nil {
		t.Fatal(e)
	}
	<-done
}
func TestCancellation(t *testing.T) {
	p := testPlugin(t, &fakeHost{})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e := p.Execute(ctx, Task{Bot: "bot", Mode: "text", Command: "test"}); !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
}
