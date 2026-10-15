// Package trace ports TeleBox's trace plugin to laowangbot.
//
// It automatically sends emoji reactions to messages from tracked users or
// containing tracked keywords:
//
//	.trace 👍👎            (reply to a message) track that user
//	.trace                 (reply to a message) untrack that user
//	.trace kw add <词> 👍   add keyword tracking
//	.trace kw del <词>      remove keyword tracking
//	.trace status          show tracking status
//	.trace clean           clear all traces
//	.trace reset           reset database
//	.trace log true|false  keep operation receipts
//	.trace big true|false  big reaction animation
package trace

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	api "github.com/OrionG-hub/laowangbot/pkg/pluginapi"
)

// availableReactions is the whitelist of standard emojis valid for reactions.
// Longer sequences are checked first so that e.g. ❤️‍🔥 is not split into ❤️.
var availableReactions = []string{
	"👍", "👎", "❤️", "🔥", "🥰", "👏", "😁", "🤔", "🤯", "😱",
	"🤬", "😢", "🎉", "🤩", "🤮", "💩", "🙏", "👌", "🕊", "🤡",
	"🥱", "🥴", "😍", "🐳", "❤️‍🔥", "🌚", "🌭", "💯", "🤣", "⚡️",
	"🍌", "🏆", "💔", "🤨", "😐", "🍓", "🍾", "💋", "🖕", "😈",
	"😎", "😇", "😤",
}

func init() {
	sort.Slice(availableReactions, func(i, j int) bool {
		return len(availableReactions[i]) > len(availableReactions[j])
	})
}

// parseEmojis extracts whitelisted emojis from free text, deduplicated,
// preserving first-seen order.
func parseEmojis(s string) []string {
	var out []string
	seen := map[string]bool{}
	rest := s
	for _, e := range availableReactions {
		if seen[e] {
			continue
		}
		if strings.Contains(rest, e) {
			out = append(out, e)
			seen[e] = true
			rest = strings.ReplaceAll(rest, e, "")
		}
	}
	return out
}

type traceConfig struct {
	KeepLog bool `json:"keepLog"`
	Big     bool `json:"big"`
}

type traceDB struct {
	Users    map[string][]string `json:"users"`
	Keywords map[string][]string `json:"keywords"`
	Config   traceConfig         `json:"config"`
}

func defaultDB() traceDB {
	return traceDB{
		Users:    map[string][]string{},
		Keywords: map[string][]string{},
		Config:   traceConfig{KeepLog: true, Big: true},
	}
}

// Plugin implements api.Plugin.
type Plugin struct {
	host api.Host
	dir  string
	mu   sync.Mutex
	db   traceDB
}

// New constructs the plugin and loads its persistent state.
func New(_ context.Context, host api.Host, dir string) (*Plugin, error) {
	p := &Plugin{host: host, dir: dir, db: defaultDB()}
	p.load()
	return p, nil
}

// Close releases plugin resources.
func (p *Plugin) Close() {}

func (p *Plugin) dbPath() string { return filepath.Join(p.dir, "trace.json") }

func (p *Plugin) load() {
	var db traceDB
	if err := api.LoadJSON(p.dbPath(), &db); err != nil {
		return // keep defaults on first run
	}
	if db.Users == nil {
		db.Users = map[string][]string{}
	}
	if db.Keywords == nil {
		db.Keywords = map[string][]string{}
	}
	p.db = db
}

func (p *Plugin) save() { _ = api.SaveJSON(p.dbPath(), p.db) }

// Handle dispatches command and event requests.
func (p *Plugin) Handle(ctx context.Context, req api.Request) api.Response {
	switch req.Type {
	case "command":
		return p.handleCommand(ctx, req)
	case "event":
		p.handleEvent(ctx, req)
	}
	return api.Response{Version: 1}
}

func (p *Plugin) handleCommand(ctx context.Context, req api.Request) api.Response {
	var ev api.Event
	if len(req.Event) > 0 {
		_ = json.Unmarshal(req.Event, &ev)
	}
	args := req.Args
	if len(args) == 0 {
		if ev.ReplyToID > 0 {
			p.untrackUser(ctx, ev)
			return api.Response{Version: 1}
		}
		return api.Response{Version: 1, HTML: true, Text: helpHTML()}
	}
	switch strings.ToLower(args[0]) {
	case "kw":
		if len(args) < 3 {
			return api.Response{Version: 1, HTML: true, Text: helpHTML()}
		}
		switch strings.ToLower(args[1]) {
		case "add":
			p.trackKeyword(ctx, ev, args[2], parseEmojis(strings.Join(args[3:], " ")))
		case "del":
			p.untrackKeyword(ctx, ev, args[2])
		default:
			return api.Response{Version: 1, HTML: true, Text: helpHTML()}
		}
		return api.Response{Version: 1}
	case "status":
		p.showStatus(ctx, ev)
		return api.Response{Version: 1}
	case "clean":
		p.mu.Lock()
		nu, nk := len(p.db.Users), len(p.db.Keywords)
		p.db.Users = map[string][]string{}
		p.db.Keywords = map[string][]string{}
		p.save()
		p.mu.Unlock()
		p.editAndDelete(ctx, ev, fmt.Sprintf("🗑️ <b>清理完成</b>\n├ 👥 清除用户: %d 个\n├ 🔑 清除关键字: %d 个\n└ ✅ 所有追踪已清空", nu, nk))
		return api.Response{Version: 1}
	case "reset":
		p.mu.Lock()
		nu, nk := len(p.db.Users), len(p.db.Keywords)
		p.db = defaultDB()
		p.save()
		p.mu.Unlock()
		p.editAndDelete(ctx, ev, fmt.Sprintf("⚠️ <b>数据库重置</b>\n├ 👥 清除用户: %d 个\n├ 🔑 清除关键字: %d 个\n└ ✅ 恢复默认设置", nu, nk))
		return api.Response{Version: 1}
	case "log", "big":
		if len(args) < 2 || (args[1] != "true" && args[1] != "false") {
			p.editMsg(ctx, ev, "❌ <b>参数错误</b>\n└ 请使用 <code>true</code> 或 <code>false</code>", true)
			return api.Response{Version: 1}
		}
		v := args[1] == "true"
		key := "keepLog"
		name := "保留日志"
		p.mu.Lock()
		if args[0] == "big" {
			key, name = "big", "大号动画"
			p.db.Config.Big = v
		} else {
			p.db.Config.KeepLog = v
		}
		p.save()
		p.mu.Unlock()
		onoff := "❌ 禁用"
		if v {
			onoff = "✅ 启用"
		}
		p.editAndDelete(ctx, ev, fmt.Sprintf("⚙️ <b>配置更新</b>\n├ 项目: %s (%s)\n├ 新值: %s\n└ 💾 配置已保存", name, key, onoff))
		return api.Response{Version: 1}
	default:
		// Anything else while replying is treated as user tracking.
		if ev.ReplyToID > 0 {
			p.trackUser(ctx, ev, parseEmojis(strings.Join(args, " ")))
			return api.Response{Version: 1}
		}
		return api.Response{Version: 1, HTML: true, Text: helpHTML()}
	}
}

// handleEvent reacts to incoming messages from tracked users or containing
// tracked keywords. Own messages are never delivered here (no "outgoing"
// event subscription).
func (p *Plugin) handleEvent(ctx context.Context, req api.Request) {
	var ev api.Event
	if len(req.Event) == 0 {
		return
	}
	_ = json.Unmarshal(req.Event, &ev)
	if ev.Out || ev.SenderID == 0 || ev.ChatID == "" || ev.MessageID <= 0 {
		return
	}
	if ev.SelfID != "" && strconv.FormatInt(ev.SenderID, 10) == ev.SelfID {
		return
	}
	p.mu.Lock()
	emojis, ok := p.db.Users[strconv.FormatInt(ev.SenderID, 10)]
	var kwEmojis []string
	if !ok && ev.Text != "" {
		for kw, em := range p.db.Keywords {
			if strings.Contains(ev.Text, kw) {
				kwEmojis = em
				break
			}
		}
	}
	p.mu.Unlock()
	if ok {
		p.react(ctx, ev.ChatID, ev.MessageID, emojis)
	} else if kwEmojis != nil {
		p.react(ctx, ev.ChatID, ev.MessageID, kwEmojis)
	}
}

// react sends one reaction per emoji to the given message.
func (p *Plugin) react(ctx context.Context, chatID string, msgID int, emojis []string) {
	p.mu.Lock()
	big := p.db.Config.Big
	p.mu.Unlock()
	for _, e := range emojis {
		_, _ = p.host.Call(ctx, api.Call{
			Method: "react", Target: chatID, MessageID: msgID, Text: e, Big: big,
		})
	}
}

// replySender returns the sender ID of the message the command replies to.
func (p *Plugin) replySender(ctx context.Context, ev api.Event) string {
	if ev.ReplyToID <= 0 {
		return ""
	}
	r, err := p.host.Call(ctx, api.Call{Method: "messages", Target: ev.ChatID, IDs: []int{ev.ReplyToID}})
	if err != nil || len(r.Messages) == 0 {
		return ""
	}
	return r.Messages[0].SenderID
}

// userName resolves a user ID to a display name.
func (p *Plugin) userName(ctx context.Context, userID string) string {
	r, err := p.host.Call(ctx, api.Call{Method: "resolve", Target: userID})
	if err != nil || r.Entity == nil {
		return "<code>" + html.EscapeString(userID) + "</code>"
	}
	name := r.Entity.Name
	if name == "" {
		name = userID
	}
	if r.Entity.Username != "" {
		name += " (@" + r.Entity.Username + ")"
	}
	return html.EscapeString(name)
}

func (p *Plugin) trackUser(ctx context.Context, ev api.Event, emojis []string) {
	if len(emojis) == 0 {
		p.editMsg(ctx, ev, "❌ <b>操作失败</b>\n├ 未找到有效的表情符号\n└ 请检查帮助中的可用列表", true)
		return
	}
	sender := p.replySender(ctx, ev)
	if sender == "" {
		p.editMsg(ctx, ev, "❌ <b>操作失败</b>\n└ 无法获取被回复消息的用户", true)
		return
	}
	p.mu.Lock()
	p.db.Users[sender] = emojis
	p.save()
	n := len(p.db.Users)
	p.mu.Unlock()
	p.react(ctx, ev.ChatID, ev.ReplyToID, emojis)
	p.editAndDelete(ctx, ev, fmt.Sprintf("✅ <b>追踪成功</b>\n├ 👤 用户: %s\n├ 🎯 表情: %s\n└ 📊 当前追踪: %d 个用户",
		p.userName(ctx, sender), html.EscapeString(strings.Join(emojis, " ")), n))
}

func (p *Plugin) untrackUser(ctx context.Context, ev api.Event) {
	sender := p.replySender(ctx, ev)
	p.mu.Lock()
	_, ok := p.db.Users[sender]
	if ok {
		delete(p.db.Users, sender)
		p.save()
	}
	n := len(p.db.Users)
	p.mu.Unlock()
	if !ok {
		p.editAndDelete(ctx, ev, "ℹ️ <b>提示</b>\n└ 该用户未被追踪")
		return
	}
	p.editAndDelete(ctx, ev, fmt.Sprintf("🗑️ <b>取消追踪</b>\n├ 👤 用户: %s\n└ 📊 剩余追踪: %d 个用户",
		p.userName(ctx, sender), n))
}

func (p *Plugin) trackKeyword(ctx context.Context, ev api.Event, keyword string, emojis []string) {
	if len(emojis) == 0 {
		p.editMsg(ctx, ev, "❌ <b>操作失败</b>\n├ 未找到有效的表情符号\n└ 请检查帮助中的可用列表", true)
		return
	}
	p.mu.Lock()
	_, isUpdate := p.db.Keywords[keyword]
	p.db.Keywords[keyword] = emojis
	p.save()
	n := len(p.db.Keywords)
	p.mu.Unlock()
	verb := "添加"
	if isUpdate {
		verb = "更新"
	}
	p.editAndDelete(ctx, ev, fmt.Sprintf("✅ <b>%s关键字追踪</b>\n├ 🔑 关键字: <code>%s</code>\n├ 🎯 表情: %s\n└ 📊 当前追踪: %d 个关键字",
		verb, html.EscapeString(keyword), html.EscapeString(strings.Join(emojis, " ")), n))
}

func (p *Plugin) untrackKeyword(ctx context.Context, ev api.Event, keyword string) {
	p.mu.Lock()
	_, ok := p.db.Keywords[keyword]
	if ok {
		delete(p.db.Keywords, keyword)
		p.save()
	}
	n := len(p.db.Keywords)
	p.mu.Unlock()
	if !ok {
		p.editAndDelete(ctx, ev, fmt.Sprintf("ℹ️ <b>提示</b>\n└ 关键字 \"<code>%s</code>\" 未被追踪", html.EscapeString(keyword)))
		return
	}
	p.editAndDelete(ctx, ev, fmt.Sprintf("🗑️ <b>删除关键字追踪</b>\n├ 🔑 关键字: <code>%s</code>\n└ 📊 剩余追踪: %d 个关键字",
		html.EscapeString(keyword), n))
}

func (p *Plugin) showStatus(ctx context.Context, ev api.Event) {
	p.mu.Lock()
	users := make(map[string][]string, len(p.db.Users))
	for k, v := range p.db.Users {
		users[k] = v
	}
	keywords := make(map[string][]string, len(p.db.Keywords))
	for k, v := range p.db.Keywords {
		keywords[k] = v
	}
	keepLog, big := p.db.Config.KeepLog, p.db.Config.Big
	p.mu.Unlock()

	var b strings.Builder
	b.WriteString("📊 <b>Trace 追踪状态面板</b>\n━━━━━━━━━━━━━━━━\n")
	b.WriteString(fmt.Sprintf("📈 <b>统计</b> 👥 %d 个用户 · 🔑 %d 个关键字\n\n", len(users), len(keywords)))
	if len(users) > 0 {
		b.WriteString("👤 <b>追踪的用户</b>\n")
		i := 0
		for uid, em := range users {
			prefix := "├"
			if i == len(users)-1 {
				prefix = "└"
			}
			b.WriteString(fmt.Sprintf("%s %s\n%s   └ %s\n", prefix, p.userName(ctx, uid),
				strings.Replace(prefix, "├", "│", 1), html.EscapeString(strings.Join(em, " "))))
			i++
		}
		b.WriteString("\n")
	}
	if len(keywords) > 0 {
		b.WriteString("🔑 <b>追踪的关键字</b>\n")
		i := 0
		for kw, em := range keywords {
			prefix := "├"
			if i == len(keywords)-1 {
				prefix = "└"
			}
			b.WriteString(fmt.Sprintf("%s <code>%s</code>\n%s   └ %s\n", prefix, html.EscapeString(kw),
				strings.Replace(prefix, "├", "│", 1), html.EscapeString(strings.Join(em, " "))))
			i++
		}
		b.WriteString("\n")
	}
	b.WriteString("⚙️ <b>配置</b>\n")
	b.WriteString(fmt.Sprintf("├ 📝 保留日志: %s\n└ 🎭 大号动画: %s",
		onoff(keepLog), onoff(big)))
	p.editMsg(ctx, ev, b.String(), true)
}

func onoff(v bool) string {
	if v {
		return "✅ 启用"
	}
	return "❌ 禁用"
}

func (p *Plugin) editMsg(ctx context.Context, ev api.Event, text string, isHTML bool) {
	_, _ = p.host.Call(ctx, api.Call{
		Method: "edit", Target: ev.ChatID, MessageID: ev.MessageID, Text: text, HTML: isHTML,
	})
}

// editAndDelete edits the trigger message and deletes it after a while when
// the keep-log option is off.
func (p *Plugin) editAndDelete(ctx context.Context, ev api.Event, text string) {
	p.editMsg(ctx, ev, text, true)
	p.mu.Lock()
	keep := p.db.Config.KeepLog
	p.mu.Unlock()
	if !keep {
		go func() {
			time.Sleep(10 * time.Second)
			_, _ = p.host.Call(context.Background(), api.Call{
				Method: "delete", Target: ev.ChatID, IDs: []int{ev.MessageID},
			})
		}()
	}
}

func helpHTML() string {
	return `🎯 <b>自动回应插件 (Trace)</b>
<i>给指定用户或含关键字的消息自动贴表情回应</i>

📌 <b>用户追踪</b>（先回复一条消息）
<code>.trace 👍👎🥰</code> 追踪该用户
<code>.trace</code> 取消追踪该用户

🔍 <b>关键字追踪</b>
<code>.trace kw add &lt;词&gt; 👍👎</code> 添加
<code>.trace kw del &lt;词&gt;</code> 删除

📊 <b>管理</b>
<code>.trace status</code> 查看追踪统计
<code>.trace clean</code> 清除所有追踪
<code>.trace reset</code> 重置全部数据
<code>.trace log true|false</code> 操作回执保留（默认 true）
<code>.trace big true|false</code> 大号表情动画（默认 true）

💡 仅支持标准 emoji；会员自定义表情暂不支持`
}
