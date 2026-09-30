package monitor

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	api "github.com/OrionG-hub/laowangbot/pkg/pluginapi"
	"github.com/dlclark/regexp2"
	"html"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

var numeric = regexp.MustCompile(`^-?\d+$`)
var messageLink = regexp.MustCompile(`t\.me/(c/)?([a-zA-Z0-9_]+)/(\d+)`)
var deepLink = regexp.MustCompile(`(?i)(?:https?://)?t\.me/([a-zA-Z0-9_]+)\?(?:start|startapp)=([^&#\s]+)`)
var groupLink = regexp.MustCompile(`(?i)(?:https?://)?(?:t\.me/|telegram\.me/|@)(\+?[a-zA-Z0-9_]+)`)
var uuid = regexp.MustCompile(`[0-9a-fA-F]{8}(?:-[0-9a-fA-F]{4}){3}-[0-9a-fA-F]{12}`)

func compileKeyword(k string) (*regexp2.Regexp, error) {
	k = strings.TrimSpace(k)
	if k == "" {
		return nil, fmt.Errorf("关键词不能为空")
	}
	if !strings.HasPrefix(strings.ToLower(k), "re:") {
		return nil, nil
	}
	exp := strings.TrimSpace(k[3:])
	opts := regexp2.RegexOptions(regexp2.ECMAScript | regexp2.IgnoreCase)
	if strings.HasPrefix(exp, "/") {
		i := strings.LastIndex(exp, "/")
		if i <= 0 {
			return nil, fmt.Errorf("正则表达式无效")
		}
		flags := exp[i+1:]
		exp = exp[1:i]
		opts = regexp2.ECMAScript
		seen := map[rune]bool{}
		for _, f := range flags {
			if seen[f] {
				return nil, fmt.Errorf("重复正则标记")
			}
			seen[f] = true
			switch f {
			case 'i':
				opts |= regexp2.IgnoreCase
			case 'm':
				opts |= regexp2.Multiline
			case 's':
				opts |= regexp2.Singleline
			case 'y':
				exp = `\A(?:` + exp + `)`
			case 'g', 'u', 'd':
			case 'v':
				return nil, fmt.Errorf("暂不支持 JS v Unicode 集合标记")
			default:
				return nil, fmt.Errorf("无效正则标记")
			}
		}
	}
	if exp == "" {
		return nil, fmt.Errorf("正则表达式不能为空")
	}
	r, e := regexp2.Compile(exp, opts)
	if e == nil {
		r.MatchTimeout = 100 * time.Millisecond
	}
	return r, e
}
func keywordMatches(text, k string) bool {
	r, e := compileKeyword(k)
	if e != nil {
		return false
	}
	if r == nil {
		return strings.Contains(strings.ToLower(text), strings.ToLower(k))
	}
	v, e := r.MatchString(text)
	return e == nil && v
}
func replace(s, p, r string) string { return regexp.MustCompile(p).ReplaceAllString(s, r) }
func normalizeField(s string) string {
	s = replace(s, `\d{4}[-/]\d{1,2}[-/]\d{1,2}[^\s|]*`, "")
	s = replace(s, `\d+\s*%`, "")
	s = replace(s, `\d[\d,._\s]*`, "")
	return strings.TrimSpace(replace(s, `\s+`, " "))
}
func normalizeLotteryKey(k string) string {
	if !strings.HasPrefix(k, "lottery:") {
		return k
	}
	p, d := strings.Index(k, ":prize="), strings.Index(k, ":draw=")
	if p < 0 || d < p {
		return k
	}
	return k[:p] + ":prize=" + normalizeField(k[p+7:d]) + ":draw=" + normalizeField(k[d+6:])
}
func tagged(text string, labels string) string {
	lines := strings.Split(text, "\n")
	start := regexp.MustCompile(`(?:` + labels + `)\s*[:：]?\s*(.*)`)
	next := regexp.MustCompile(`^\s*(?:[^\w\s]{0,4}\s*)?(?:抽奖|奖品|奖励|开奖|参与|条件|口令|关键词|留言|发起|活动|备注)[^:：\n]{0,12}[:：]`)
	for i, line := range lines {
		m := start.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		var v []string
		if strings.TrimSpace(m[1]) != "" {
			v = append(v, strings.TrimSpace(m[1]))
		}
		for j := i + 1; j < len(lines) && len(v) < 4; j++ {
			l := strings.TrimSpace(lines[j])
			if l == "" {
				if len(v) > 0 {
					break
				}
				continue
			}
			if next.MatchString(l) {
				break
			}
			v = append(v, l)
		}
		return strings.Join(v, " | ")
	}
	return ""
}
func dedupKeys(msg api.Message, text, kw string) []string {
	deep, lottery := "", ""
	p := regexp.MustCompile(`(?i)(?:抽奖\s*ID|抽奖编号|活动\s*ID|开奖\s*ID)\s*[:：]\s*([0-9a-fA-F]{8}(?:-[0-9a-fA-F]{4}){3}-[0-9a-fA-F]{12}|[A-Za-z0-9_-]{10,})`)
	if m := p.FindStringSubmatch(text); m != nil {
		lottery = "lottery-id:" + strings.ToLower(m[1])
	}
	if lottery == "" {
		p = regexp.MustCompile(`(?i)(?:join|lottery|draw|take)\s*[_=-]\s*([0-9a-fA-F]{8}(?:-[0-9a-fA-F]{4}){3}-[0-9a-fA-F]{12})`)
		if m := p.FindStringSubmatch(text); m != nil {
			lottery = "lottery-id:" + strings.ToLower(m[1])
		}
	}
	for _, b := range msg.Buttons {
		if m := deepLink.FindStringSubmatch(b.URL); m != nil {
			param, e := url.QueryUnescape(m[2])
			if e != nil {
				param = m[2]
			}
			if deep == "" {
				deep = "deep:" + m[1] + ":" + param
			}
			if lottery == "" {
				if id := uuid.FindString(param); id != "" {
					lottery = "lottery-id:" + strings.ToLower(id)
				}
			}
		}
	}
	var keys []string
	if lottery != "" {
		keys = append(keys, lottery)
		if deep != "" {
			keys = append(keys, deep)
		}
		return keys
	}
	if deep != "" {
		return []string{deep}
	}
	if msg.ForwardID != "" {
		mid, date := "no-msg-id", "no-date"
		if msg.ForwardMessageID != 0 {
			mid = strconv.Itoa(msg.ForwardMessageID)
		}
		if msg.ForwardDate != 0 {
			date = strconv.Itoa(msg.ForwardDate)
		}
		keys = append(keys, fmt.Sprintf("fwd:%s:%s:%s", msg.ForwardID, mid, date))
	}
	if kw != "" && !strings.HasPrefix(kw, "/start ") {
		prize, draw := tagged(text, "奖品内容|奖品|奖励|奖项"), tagged(text, "开奖日期|开奖时间|开奖")
		if prize != "" || draw != "" {
			k := []rune("lottery:" + kw + ":prize=" + normalizeField(prize) + ":draw=" + normalizeField(draw))
			if len(k) > 200 {
				k = k[:200]
			}
			keys = append(keys, string(k))
		}
	}
	n := replace(text, `(?:剩余|倒计时|开奖倒计时|还有)\s*[^\n]*`, "")
	n = replace(n, `\d+\s*(?:天|小时|时|分钟|分|秒)(?:\s*\d+\s*(?:天|小时|时|分钟|分|秒))*`, "")
	n = strings.TrimSpace(replace(n, `\s+`, " "))
	if len([]rune(n)) > 5 {
		keys = append(keys, fmt.Sprintf("txt:%x", sha256.Sum256([]byte(n))))
	}
	return keys
}
func (m *Monitor) extract(ctx context.Context, msg api.Message, text, matched string) (string, string) {
	for _, b := range msg.Buttons {
		if strings.Contains(b.Text, "抽奖") || strings.Contains(b.Text, "参与") || matched == b.Text {
			if d := deepLink.FindStringSubmatch(b.URL); d != nil {
				return d[1], "/start " + d[2]
			}
		}
	}
	clean := strings.NewReplacer("*", "", "_", "", "`", "").Replace(text)
	p := regexp.MustCompile("(?:关键词|口令|留言)\\s*[:：]\\s*(?:[「“\"']([^「」“”\"'\\n]+)[」”\"']?|([^\\n]+))")
	k := p.FindStringSubmatch(clean)
	if k == nil {
		return msg.ChatID, ""
	}
	kw := k[1]
	if kw == "" {
		kw = k[2]
	}
	kw = strings.Trim(kw, " \t\r\n>")
	r := []rune(kw)
	if len(r) > 60 {
		kw = string(r[:60])
	}
	urls := append([]string{}, msg.URLs...)
	urls = append(urls, groupLink.FindAllString(text, -1)...)
	for _, u := range urls {
		g := groupLink.FindStringSubmatch(u)
		if g == nil {
			continue
		}
		name := g[1]
		lower := strings.ToLower(name)
		if lower == "share" || lower == "c" || strings.HasSuffix(lower, "bot") {
			continue
		}
		r, e := m.peer(ctx, name)
		if e != nil {
			return name, kw
		}
		if r.Entity != nil && r.Entity.Group && !r.Entity.Broadcast {
			return name, kw
		}
	}
	return msg.ChatID, kw
}
func (m *Monitor) origin(ctx context.Context, msg api.Message) map[string]string {
	r, _ := m.peer(ctx, msg.ChatID)
	if !strings.HasPrefix(msg.ChatID, "-") {
		if r.Entity == nil || r.Entity.Username == "" {
			return nil
		}
		label := "🔗 打开原私聊"
		if r.Entity.Bot {
			label = "🔗 打开原 Bot"
		}
		return map[string]string{"text": label, "url": "https://t.me/" + r.Entity.Username}
	}
	u := ""
	if r.Entity != nil && r.Entity.Username != "" {
		u = "https://t.me/" + r.Entity.Username + "/" + strconv.Itoa(msg.ID)
	} else if strings.HasPrefix(msg.ChatID, "-100") {
		u = "https://t.me/c/" + strings.TrimPrefix(msg.ChatID, "-100") + "/" + strconv.Itoa(msg.ID)
	}
	if u == "" {
		return nil
	}
	return map[string]string{"text": "🔗 查看原消息", "url": u}
}

// alertMaxAge bounds how old a message may be and still earn an alert. It is
// deliberately far above Telegram's worst observed delivery delay: the old 60s
// gate silently discarded exactly the late channel posts this plugin exists to
// catch. Anything older than this is a replay after a long outage.
const alertMaxAge = 15 * time.Minute

func (m *Monitor) event(ctx context.Context, e api.Event) error {
	if e.Date <= 0 || time.Since(time.Unix(int64(e.Date), 0)) > alertMaxAge {
		return nil
	}
	if strings.HasPrefix(e.Text, ".monitor sync ") {
		return m.syncCommand(ctx, e)
	}
	if strings.HasPrefix(e.Text, ".") {
		return nil
	}
	s := m.state.Settings
	if !s.IsGlobalEnabled {
		return nil
	}
	ids := []string{e.ChatID}
	if !strings.HasPrefix(e.ChatID, "-") {
		ids = append(ids, "-100"+e.ChatID)
	}
	allowed := s.MonitorAllGroups
	for _, id := range ids {
		if slices.Contains(s.ExcludedGroups, id) {
			return nil
		}
		allowed = allowed || slices.Contains(s.EnabledGroups, id)
	}
	if !allowed {
		return nil
	}
	// Late updates are normal for channels, so age alone cannot decide; only a
	// message that is not newer than this chat's watermark is a replay.
	if m.alreadySeen(e.ChatID, e.MessageID, int64(e.Date)) {
		return nil
	}
	msg := m.eventMessage(ctx, e)
	if s.BotID != "" && msg.SenderID == s.BotID {
		return nil
	}
	text := msg.Text
	for _, b := range msg.Buttons {
		text += " " + b.Text
	}
	specific := false
	keys := append([]string{}, s.Keywords...)
	for _, id := range ids {
		specific = specific || slices.Contains(s.GroupUsers[id], msg.SenderID)
		keys = append(keys, s.GroupKeywords[id]...)
	}
	matched := ""
	for _, k := range keys {
		if keywordMatches(text, k) {
			matched = k
			break
		}
	}
	if matched == "" {
		if !specific {
			return nil
		}
		matched = "👤 指定人发言"
	}
	if !specific && !(s.MonitorAdminsMessages && s.MonitorUsersMessages && !s.IgnoreBotMessages) {
		identity, err := m.identity(ctx, msg)
		if err != nil {
			return err
		}
		if identity != "owner" && !(identity == "bot" && !s.IgnoreBotMessages) && !(identity == "admin" && s.MonitorAdminsMessages) && !(identity == "user" && s.MonitorUsersMessages) {
			return nil
		}
	}
	if len(s.TargetGroups) == 0 {
		return nil
	}
	target, kw := m.extract(ctx, msg, text, matched)
	dedup := dedupKeys(msg, text, kw)
	for _, k := range dedup {
		if s.EnableDedup && m.state.Dedup[k] > time.Now().Add(-24*time.Hour).UnixMilli() {
			return nil
		}
	}
	for _, k := range dedup {
		m.state.Dedup[k] = time.Now().UnixMilli()
		if strings.HasPrefix(k, "lottery:") {
			m.dumpSample(msg, text)
		}
	}
	keyboard := [][]map[string]string{}
	tip := "🚨 <b>关键词提醒</b> [<code>" + html.EscapeString(shortText(matched, 256)) + "</code>]"
	if kw != "" {
		// The one-tap join button is gone on purpose: its callback only reaches us
		// through per-second Bot API polling, which held the plugin's single caller
		// for seconds at a time. Copying this command does the same thing.
		command := ".monitor sync " + target + " " + msg.ChatID + " " + kw
		if len(command) < 2000 {
			tip += "\n\n🛡️ <b>群友代发指令 (点击复制):</b>\n<code>" + html.EscapeString(command) + "</code>"
		} else {
			tip += "\n\n🛡️ 代发指令过长，已省略。"
		}
	}
	if origin := m.origin(ctx, msg); origin != nil {
		keyboard = append(keyboard, []map[string]string{origin})
	}
	if matched == "👤 指定人发言" {
		tip = "🚨 <b>指定监控人发言</b>"
	}
	// Delivery is queued rather than inline: one slow Bot API answer must not
	// hold the plugin's only caller while new messages overflow the host queue.
	// The same tick that flushes these jobs persists dedup and pending state.
	now := time.Now().UnixMilli()
	var errs []error
	for _, t := range s.TargetGroups {
		j := Job{Kind: "notify", Due: now, Target: t.ID, Source: msg.ChatID, IDs: []int{msg.ID}, Text: tip, Fallback: text}
		if s.BotToken != "" {
			payload, err := json.Marshal(map[string]any{"chat_id": t.ID, "text": tip, "parse_mode": "HTML", "disable_web_page_preview": true, "reply_markup": map[string]any{"inline_keyboard": keyboard}})
			if err != nil {
				return err
			}
			j.Payload = payload
		}
		if m.inlineLeft > 0 {
			// Ordinary alerts are delivered inline: waiting for the next tick would
			// add latency to every one of them. Only once this tick's budget is gone
			// does a burst fall back to the queue, which keeps the caller bounded.
			m.inlineLeft--
			if err := m.deliver(ctx, j); err != nil {
				errs = append(errs, err)
			}
			continue
		}
		m.state.Jobs = append(m.state.Jobs, j)
	}
	return errors.Join(errs...)
}

// alreadySeen rejects a chat's replayed updates without dropping same-second
// siblings: IDs advance within a chat even when dates are equal, so a date-only
// watermark would silently discard real messages.
func (m *Monitor) alreadySeen(chat string, msgID int, date int64) bool {
	w := m.state.Watermarks[chat]
	if msgID <= w.Msg && date <= w.Date {
		return true
	}
	if m.state.Watermarks == nil {
		m.state.Watermarks = map[string]ChatMark{}
	}
	if len(m.state.Watermarks) >= 4096 {
		m.state.Watermarks = map[string]ChatMark{}
	}
	m.state.Watermarks[chat] = ChatMark{Msg: max(w.Msg, msgID), Date: max(w.Date, date)}
	return false
}

// eventMessage prefers the message the host already serialized into the event;
// the "messages" round trip only runs for hosts that send a bare event.
func (m *Monitor) eventMessage(ctx context.Context, e api.Event) api.Message {
	msg := api.Message{ID: e.MessageID, ChatID: e.ChatID, SenderID: strconv.FormatInt(e.SenderID, 10), Text: e.Text, Date: e.Date, Out: e.Out}
	if e.Message != nil && e.Message.ID == e.MessageID {
		return *e.Message
	}
	r, err := m.call(ctx, api.Call{Method: "messages", Target: e.ChatID, IDs: []int{e.MessageID}})
	if err == nil && len(r.Messages) > 0 {
		msg = r.Messages[0]
	}
	return msg
}

// lookupTTL is short enough that a renamed group or a promoted member only costs
// a stale link or one missed alert for a few minutes.
const lookupTTL = 10 * time.Minute

type lookup struct {
	text   string
	result api.Result
	until  time.Time
}

func (m *Monitor) remember(key, text string, result api.Result) {
	if m.lookups == nil {
		m.lookups = map[string]lookup{}
	}
	if len(m.lookups) >= 4096 {
		m.lookups = map[string]lookup{}
	}
	m.lookups[key] = lookup{text: text, result: result, until: time.Now().Add(lookupTTL)}
}

// identity caches the sender lookup that every alert used to pay for.
func (m *Monitor) identity(ctx context.Context, msg api.Message) (string, error) {
	if msg.Out {
		return "owner", nil
	}
	key := "id:" + msg.ChatID + "|" + msg.SenderID
	if v, ok := m.lookups[key]; ok && time.Now().Before(v.until) {
		return v.text, nil
	}
	r, err := m.call(ctx, api.Call{Method: "identity", Target: msg.ChatID, User: msg.SenderID})
	if err != nil {
		return "", err
	}
	m.remember(key, r.Identity, api.Result{})
	return r.Identity, nil
}

// peer caches entity lookups used for origin links and target-group checks.
func (m *Monitor) peer(ctx context.Context, target string) (api.Result, error) {
	key := "peer:" + target
	if v, ok := m.lookups[key]; ok && time.Now().Before(v.until) {
		return v.result, nil
	}
	r, err := m.call(ctx, api.Call{Method: "resolve", Target: target})
	if err != nil {
		return r, err
	}
	m.remember(key, "", r)
	return r, nil
}
func (m *Monitor) dumpSample(msg api.Message, text string) {
	path := filepath.Join(filepath.Dir(m.path), "lottery-samples.jsonl")
	if st, e := os.Stat(path); e == nil && st.Size() > 512*1024 {
		_ = os.Remove(path)
	}
	r := []rune(text)
	if len(r) > 900 {
		text = string(r[:900])
	}
	b, _ := json.Marshal(map[string]any{"at": time.Now().UTC().Format(time.RFC3339), "why": "no-lottery-id", "chatId": msg.ChatID, "msgId": msg.ID, "text": text})
	f, e := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if e == nil {
		defer f.Close()
		_, _ = f.Write(append(b, '\n'))
	}
}
