package monitor

import (
	"context"
	"encoding/json"
	"fmt"
	api "github.com/OrionG-hub/laowangbot/pkg/pluginapi"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type fakeHost struct {
	calls       []api.Call
	identity    string
	failForward bool
}

func (f *fakeHost) Call(_ context.Context, c api.Call) (api.Result, error) {
	f.calls = append(f.calls, c)
	switch c.Method {
	case "self":
		return api.Result{Entity: &api.Entity{ID: "1"}}, nil
	case "identity":
		return api.Result{Identity: f.identity}, nil
	case "resolve":
		return api.Result{Entity: &api.Entity{ID: c.Target, Group: true, Username: "source"}}, nil
	case "messages":
		return api.Result{}, nil
	case "send":
		return api.Result{MessageID: 77}, nil
	}
	return api.Result{}, nil
}
func setup(t *testing.T) (*Monitor, *fakeHost) {
	t.Helper()
	h := &fakeHost{identity: "admin"}
	m, e := New(h, filepath.Join(t.TempDir(), "monitor.json"))
	if e != nil {
		t.Fatal(e)
	}
	return m, h
}
func TestRegex(t *testing.T) {
	for _, k := range []string{"hello", "re:hello", "re:/(?<=a)b/i"} {
		if _, e := compileKeyword(k); e != nil {
			t.Fatal(e)
		}
	}
	if _, e := compileKeyword("re:/[/i"); e == nil {
		t.Fatal("bad regex accepted")
	}
	if !keywordMatches("HELLO", "hello") || !keywordMatches("ab", "re:/(?<=a)b/i") {
		t.Fatal("matching")
	}
}
func TestCommandPersistence(t *testing.T) {
	m, _ := setup(t)
	e := api.Event{ChatID: "-1005", Out: true, MessageID: 4}
	for _, a := range [][]string{{"on"}, {"set", "keyword", "add", "抽奖"}, {"set", "group_user", "add", "-1005", "10"}, {"set", "target", "add", "-1009", "通知"}} {
		if _, err := m.command(context.Background(), e, a); err != nil {
			t.Fatal(err)
		}
	}
	n, err := New(m.host, m.path)
	if err != nil {
		t.Fatal(err)
	}
	if len(n.state.Settings.Keywords) != 1 || len(n.state.Settings.GroupUsers["-1005"]) != 1 || len(n.state.Settings.TargetGroups) != 1 {
		t.Fatal("state not persisted")
	}
}
func TestEventDedup(t *testing.T) {
	m, h := setup(t)
	m.state.Settings.MonitorAllGroups = true
	m.state.Settings.Keywords = []string{"抽奖"}
	m.state.Settings.TargetGroups = []Target{{ID: "-1009"}}
	e := api.Event{ChatID: "-1005", SenderID: 9, MessageID: 10, Text: "抽奖 ID: abcdef12345678\n口令：好运", Date: int(time.Now().Unix())}
	for i := 0; i < 2; i++ {
		if err := m.event(context.Background(), e); err != nil {
			t.Fatal(err)
		}
	}
	n := 0
	for _, c := range h.calls {
		if c.Method == "forward" {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("forward count %d", n)
	}
	if len(m.state.Pending) != 1 {
		t.Fatal("missing callback")
	}
}
func TestUnauthorizedSync(t *testing.T) {
	m, h := setup(t)
	e := api.Event{ChatID: "-1", SenderID: 7, Text: ".monitor sync -2 -1 hi", Date: int(time.Now().Unix())}
	if err := m.event(context.Background(), e); err != nil {
		t.Fatal(err)
	}
	if len(m.state.Jobs) != 0 {
		t.Fatal("unauthorized queued")
	}
	_ = h
}
func TestBotCallbackAuthorization(t *testing.T) {
	m, h := setup(t)
	m.state.Settings.BotToken = "test"
	m.owner = "1"
	m.state.Pending["a"] = Action{TargetChatID: "-2", SourceChatID: "-1", Keyword: "hi", Time: time.Now().UnixMilli()}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": []any{}})
	}))
	defer srv.Close()
	m.botBase = srv.URL
	cb := Callback{ID: "cb", Data: "send_a"}
	cb.From.ID = json.Number("7")
	if err := m.callback(context.Background(), cb); err != nil {
		t.Fatal(err)
	}
	if len(m.state.Pending) != 1 || len(h.calls) != 0 {
		t.Fatal("unauthorized callback changed state")
	}
}

func TestCommandFamilies(t *testing.T) {
	m, _ := setup(t)
	ctx := context.Background()
	e := api.Event{ChatID: "-1005", Out: true}
	commands := [][]string{{"global", "off"}, {"global", "on"}, {"set", "leader", "123"}, {"set", "leader", "del"}, {"set", "bot_token", "test"}, {"set", "bot_token", "del"}, {"set", "bot_id", "12"}, {"set", "bot_id", "del"}, {"set", "monitor_all_groups", "on"}, {"set", "dedup", "off"}, {"set", "ignore_bot_messages", "on"}, {"set", "monitor_admins_messages", "off"}, {"set", "monitor_users_messages", "on"}, {"set", "exclude_group", "add", "-123"}, {"set", "exclude_group", "del", "1"}, {"set", "monitor_group", "add", "-123"}, {"set", "monitor_group", "del", "1"}, {"set", "group_keyword", "add", "-123", "hello"}, {"set", "group_keyword", "del", "-123", "1"}, {"set", "group_keyword", "clear", "-123"}, {"set", "group_user", "add", "-123", "456"}, {"set", "group_user", "del", "-123", "456"}, {"set", "group_user", "clear", "-123"}, {"set", "target", "add", "-5", "name"}, {"set", "target", "del", "1"}, {"set", "keyword", "add", "hello"}, {"set", "keyword", "del", "1"}, {"on"}, {"off"}, {"list"}, {"list_groups"}, {"clean"}, {"send", "-1", "hello"}, {}}
	for _, a := range commands {
		if _, err := m.command(ctx, e, a); err != nil {
			t.Fatalf("%v: %v", a, err)
		}
	}
	for _, a := range [][]string{{"set", "leader", "bad"}, {"set", "group_user", "add", "-1", "x"}, {"set", "keyword", "add", "re:["}, {"set", "unknown"}, {"send"}, {"set", "target", "add"}} {
		if _, err := m.command(ctx, e, a); err == nil {
			t.Fatalf("accepted %v", a)
		}
	}
}
func TestDedupVariants(t *testing.T) {
	cases := []struct {
		m                api.Message
		text, kw, prefix string
	}{{api.Message{}, "抽奖ID: abcdef123456", "", "lottery-id:"}, {api.Message{Buttons: []api.Button{{URL: "https://t.me/bot?start=foo"}}}, "", "", "deep:"}, {api.Message{Buttons: []api.Button{{URL: "https://t.me/bot?start=draw_12345678-abcd-abcd-abcd-123456789012"}}}, "", "", "lottery-id:"}, {api.Message{ForwardID: "channel:5", ForwardDate: 1, ForwardMessageID: 2}, "hello world", "", "fwd:"}, {api.Message{}, "奖品：礼物 123\n开奖：2026/01/01\n口令：hello", "hello", "lottery:"}, {api.Message{}, "hello world 剩余 10秒", "", "txt:"}}
	for _, c := range cases {
		k := dedupKeys(c.m, c.text, c.kw)
		if len(k) == 0 || !strings.HasPrefix(k[0], c.prefix) {
			t.Fatalf("%+v %v", c, k)
		}
	}
	if normalizeLotteryKey("lottery:x:prize=gift 100:draw=2026/01/01") != "lottery:x:prize=gift:draw=" {
		t.Fatal("legacy normalization")
	}
}
func TestIdentityFiltersAndSpecifiedUser(t *testing.T) {
	m, h := setup(t)
	m.state.Settings.MonitorAllGroups = true
	m.state.Settings.Keywords = []string{"hello"}
	m.state.Settings.TargetGroups = []Target{{ID: "-8"}}
	h.identity = "user"
	e := api.Event{ChatID: "-5", SenderID: 7, MessageID: 1, Text: "hello world", Date: int(time.Now().Unix())}
	if err := m.event(context.Background(), e); err != nil {
		t.Fatal(err)
	}
	if len(m.state.Dedup) != 0 {
		t.Fatal("ordinary user should be filtered")
	}
	m.state.Settings.GroupUsers["-5"] = []string{"7"}
	e.Text = "specific person only"
	if err := m.event(context.Background(), e); err != nil {
		t.Fatal(err)
	}
	if len(m.state.Dedup) == 0 {
		t.Fatal("specified sender should bypass filters")
	}
}
func TestSyncTickAndRestart(t *testing.T) {
	m, h := setup(t)
	e := api.Event{ChatID: "-5", SenderID: 1, SenderIsUser: true, Text: ".monitor sync ExampleBot -5 hi", Date: int(time.Now().Unix())}
	if err := m.syncCommand(context.Background(), e); err != nil {
		t.Fatal(err)
	}
	if len(m.state.Jobs) != 1 {
		t.Fatal("job")
	}
	m.state.Jobs[0].Due = 1
	if err := m.tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, c := range h.calls {
		found = found || (c.Method == "send" && c.Target == "@ExampleBot" && c.Text == "hi")
	}
	if !found {
		t.Fatal("sync send missing")
	}
	n, err := New(h, m.path)
	if err != nil || len(n.state.Jobs) == 0 {
		t.Fatal("delete persistence", err)
	}
	for i := range n.state.Jobs {
		n.state.Jobs[i].Due = 1
	}
	if err = n.tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(n.state.Jobs) != 0 {
		t.Fatal("deletes not drained")
	}
}
func TestBotPollingAndCallback(t *testing.T) {
	m, h := setup(t)
	m.owner = "1"
	m.state.Settings.BotToken = "test"
	m.state.Pending["a"] = Action{TargetChatID: "Bot", SourceChatID: "-2", Keyword: "hi", Time: time.Now().UnixMilli()}
	methods := []string{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		methods = append(methods, r.URL.Path)
		if strings.HasSuffix(r.URL.Path, "getUpdates") {
			fmt.Fprint(w, `{"ok":true,"result":[{"update_id":5,"callback_query":{"id":"c","data":"send_a","from":{"id":1},"message":{"message_id":7,"chat":{"id":-2},"reply_markup":{"inline_keyboard":[[{"text":"send","callback_data":"send_a"}],[{"text":"link","url":"https://t.me/bot"}]]}}}}]}`)
		} else {
			fmt.Fprint(w, `{"ok":true,"result":true}`)
		}
	}))
	defer srv.Close()
	m.botBase = srv.URL
	if err := m.poll(context.Background()); err != nil {
		t.Fatal(err)
	}
	if m.state.UpdateID != 5 || len(m.state.Pending) != 0 || len(m.state.Jobs) != 1 {
		t.Fatal("callback state")
	}
	if len(methods) != 3 {
		t.Fatal(methods)
	}
	before := len(m.state.Jobs)
	if err := m.syncCommand(context.Background(), api.Event{SenderID: 1, SenderIsUser: true, Text: ".monitor sync Bot -2 hi"}); err != nil {
		t.Fatal(err)
	}
	if len(m.state.Jobs) != before {
		t.Fatal("local echo duplicate")
	}
	if len(h.calls) != 1 {
		t.Fatal(h.calls)
	}
}
func TestBotConflictAndInvalidResponse(t *testing.T) {
	m, _ := setup(t)
	m.state.Settings.BotToken = "test"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, `{"ok":false,"error_code":409}`) }))
	defer srv.Close()
	m.botBase = srv.URL
	if err := m.poll(context.Background()); err == nil || m.conflicts != 1 || m.nextPoll <= time.Now().UnixMilli() {
		t.Fatal("conflict handling")
	}
}
func TestHandleAuthorizationAndReply(t *testing.T) {
	m, h := setup(t)
	e := api.Event{SelfID: "1", SenderID: 1, SenderIsUser: true, ChatID: "-2", MessageID: 3, Out: true}
	b, _ := json.Marshal(e)
	r := m.Handle(context.Background(), api.Request{Type: "command", Args: []string{"on"}, Event: b})
	if r.Error != "" || len(h.calls) != 1 || h.calls[0].Method != "edit" {
		t.Fatal(r, h.calls)
	}
	e.SenderID = 2
	e.Out = false
	b, _ = json.Marshal(e)
	r = m.Handle(context.Background(), api.Request{Type: "command", Args: []string{"off"}, Event: b})
	if r.Error == "" {
		t.Fatal("owner guard")
	}
	r = m.Handle(context.Background(), api.Request{Type: "event", Event: []byte("invalid")})
	if r.Error == "" {
		t.Fatal("invalid JSON")
	}
}

func TestExtractAndOrigin(t *testing.T) {
	m, _ := setup(t)
	ctx := context.Background()
	msg := api.Message{ID: 3, ChatID: "-1005", Buttons: []api.Button{{Text: "参与抽奖", URL: "https://t.me/ExampleBot?start=hello"}}}
	target, kw := m.extract(ctx, msg, "抽奖", "抽奖")
	if target != "ExampleBot" || kw != "/start hello" {
		t.Fatal(target, kw)
	}
	msg.Buttons = nil
	target, kw = m.extract(ctx, msg, "口令：「好运」\nhttps://t.me/mygroup", "好运")
	if target != "mygroup" || kw != "好运" {
		t.Fatal(target, kw)
	}
	if m.origin(ctx, msg)["url"] != "https://t.me/source/3" {
		t.Fatal("origin")
	}
	msg.ChatID = "123"
	if m.origin(ctx, msg)["url"] != "https://t.me/source" {
		t.Fatal("private origin")
	}
	if keywordMatches("ba", "re:/a/y") {
		t.Fatal("sticky must match at start")
	}
}
func TestCleanupAndLegacyPairs(t *testing.T) {
	m, _ := setup(t)
	now := time.Now().UnixMilli()
	m.state.Pending["old"] = Action{Time: now - 86400001}
	m.state.Pending["new"] = Action{Time: now}
	m.state.Dedup["old"] = now - 86400001
	m.state.Suppress["old"] = now - 120001
	if err := m.save(); err != nil {
		t.Fatal(err)
	}
	n, err := New(m.host, m.path)
	if err != nil {
		t.Fatal(err)
	}
	if len(n.state.Pending) != 1 || len(n.state.Dedup) != 0 || len(n.state.Suppress) != 0 {
		t.Fatal("cleanup and pair restore")
	}
}

func TestSyncSenderNamespace(t *testing.T) {
	m, _ := setup(t)
	m.state.Settings.TrustedLeaderID = "77"
	base := api.Event{ChatID: "-5", MessageID: 19, SenderID: 77, SenderPeerID: "-10077", Text: ".monitor sync -2 -5 hi", Date: int(time.Now().Unix()), SelfID: "1"}
	raw, _ := json.Marshal(base)
	r := m.Handle(context.Background(), api.Request{Type: "event", Event: raw})
	if r.Error != "" || len(m.state.Jobs) != 0 {
		t.Fatal("channel impersonated trusted user", r)
	}
	base.SenderPeerID = "77"
	base.SenderIsUser = true
	raw, _ = json.Marshal(base)
	r = m.Handle(context.Background(), api.Request{Type: "event", Event: raw})
	if r.Error != "" || len(m.state.Jobs) != 1 {
		t.Fatal("trusted user rejected", r)
	}
	base.MessageID++
	base.SenderID = 1
	base.SenderPeerID = "-1001"
	base.SenderIsUser = false
	base.Out = true
	raw, _ = json.Marshal(base)
	r = m.Handle(context.Background(), api.Request{Type: "event", Event: raw})
	if r.Error != "" || len(m.state.Jobs) != 1 {
		t.Fatal("channel impersonated owner", r)
	}
}
func TestSyncEventCommandOnce(t *testing.T) {
	for _, commandFirst := range []bool{false, true} {
		t.Run(fmt.Sprint(commandFirst), func(t *testing.T) {
			m, _ := setup(t)
			e := api.Event{ChatID: "-5", MessageID: 20, SenderID: 1, SenderPeerID: "1", SenderIsUser: true, Out: true, SelfID: "1", Text: ".monitor sync -2 -5 hello", Date: int(time.Now().Unix())}
			b, _ := json.Marshal(e)
			event := api.Request{Type: "event", Event: b}
			command := api.Request{Type: "command", Command: "monitor", Args: []string{"sync", "-2", "-5", "hello"}, Event: b}
			requests := []api.Request{event, command}
			if commandFirst {
				requests = []api.Request{command, event}
			}
			for _, r := range requests {
				if response := m.Handle(context.Background(), r); response.Error != "" {
					t.Fatal(response)
				}
			}
			if len(m.state.Jobs) != 1 {
				t.Fatalf("queued %d jobs", len(m.state.Jobs))
			}
		})
	}
}
