package bh

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	api "github.com/OrionG-hub/laowangbot/pkg/pluginapi"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"
)

// remaining mirrors the legacy bh.ts getRemainInfo precedence: manual days win,
// then a parsed fixed date, then parsed days, then the 30-day floor. ok=false
// means no usable activity timestamp, which the report renders as 时间解析异常.
func remaining(t Task, now time.Time) (int, string, bool) {
	if t.LastActive == "" {
		return 0, "", false
	}
	act, e := parseDate(t.LastActive)
	if e != nil {
		return 0, "", false
	}
	if t.ExpireDays > 0 {
		return floorDays(act.Unix(), now, t.ExpireDays), fmt.Sprintf("%d天[手动配置]", t.ExpireDays), true
	}
	if t.LastDate != "" {
		if exp, e := parseDate(t.LastDate); e == nil {
			return int(math.Floor(float64(exp.Unix()-now.Unix()) / 86400)), "固定日期[自动识别]", true
		}
	}
	days, source := 30, "默认保底"
	if t.LastDays > 0 {
		days, source = t.LastDays, "自动识别"
	}
	return floorDays(act.Unix(), now, days), fmt.Sprintf("%d天[%s]", days, source), true
}
func floorDays(active int64, now time.Time, days int) int {
	return int(math.Floor(float64(active+int64(days)*86400-now.Unix()) / 86400))
}
func activity(t Task, now time.Time) string {
	if t.LastActive == "" {
		return "无记录"
	}
	n, _, ok := remaining(t, now)
	if !ok {
		return t.LastActive + "（解析异常）"
	}
	a, _ := parseDate(t.LastActive)
	return fmt.Sprintf("%s (已隔 %d 天 / 剩 %d 天)", t.LastActive, int(math.Floor(now.Sub(a).Hours()/24)), n)
}

// reportLine returns one HTML line plus whether it needs attention; the legacy
// plugin hides safe accounts, so a false result is counted instead of shown.
func reportLine(t Task, err error, n Notify, now time.Time) (string, bool) {
	name := t.Remark
	if name == "" {
		name = t.Bot
	}
	link := `<a href="https://t.me/` + t.Bot + `"><b>` + esc(name) + `</b></a>`
	failure := ""
	if err != nil {
		failure = esc(err.Error())
	}
	if err == nil {
		days, limit, ok := remaining(t, now)
		if !ok {
			return "⚪️ " + link + ": 时间解析异常 (" + esc(t.LastActive) + ")", true
		}
		if days < 0 {
			return "⚫️ " + link + ": 已过期 / 可能被禁用 (超期 " + strconv.Itoa(-days) + " 天)", true
		}
		icon, alert := "🟢", false
		if days <= n.Danger {
			icon, alert = "🔴", true
		} else if days <= n.Warning {
			icon, alert = "🟡", true
		}
		return fmt.Sprintf("%s %s: 将在 %d 天后禁用 (上限:%s, 上次:%s)", icon, link, days, limit, dateOnly(t.LastActive)), alert
	}
	if t.LastActive != "" {
		if days, limit, ok := remaining(t, now); ok {
			if days > n.Warning {
				return fmt.Sprintf("🟢 %s: 抓取失败但历史推算安全 (剩 %d 天, 上次:%s)", link, days, dateOnly(t.LastActive)), false
			}
			icon := "🟡"
			if days <= n.Danger {
				icon = "🔴"
			}
			return fmt.Sprintf("%s %s: 抓取失败 (剩 %d 天, 上限:%s, 上次:%s) | %s", icon, link, days, limit, dateOnly(t.LastActive), failure), true
		}
	}
	return "❌ " + link + ": " + failure, true
}
func dateOnly(s string) string {
	r := []rune(s)
	if len(r) > 10 {
		r = r[:10]
	}
	return esc(string(r))
}

// reportText assembles the aggregate report exactly as the legacy bh.ts did,
// including the 单点/聚合 title and the italic hidden-safe footnote.
func reportText(single bool, lines []string, safe int, now time.Time) string {
	kind := "聚合"
	if single {
		kind = "单点"
	}
	out := append([]string(nil), lines...)
	if len(out) == 0 {
		out = append(out, "✅ 所有账号均处于安全期，暂无需要提醒的任务！")
	}
	if safe > 0 {
		out = append(out, fmt.Sprintf("\n<i>(另有 %d 个账号状态正常 🟢，已隐藏)</i>", safe))
	}
	return fmt.Sprintf("📊 <b>保号检测%s报告</b>\n时间: %s\n\n%s", kind, now.Format("2006/01/02 15:04:05"), strings.Join(out, "\n"))
}

// progressText is the in-place edit shown while a manual batch is still running;
// the finished message uses reportText so it matches the legacy layout.
func progressText(b *batch) string {
	if len(b.lines) == 0 {
		return fmt.Sprintf("\U0001F680 保号检测中 %d/%d", b.done, b.total)
	}
	return fmt.Sprintf("\U0001F680 保号检测中 %d/%d\n\n%s", b.done, b.total, strings.Join(b.lines, "\n"))
}

// esc escapes only the markup characters Telegram parses, matching the legacy
// plugin, which left quotes in account names literal.
func esc(s string) string { return escapeReplacer.Replace(s) }

var escapeReplacer = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")

func utf16Len(s string) int { return len(utf16.Encode([]rune(s))) }

// pages splits raw text by Telegram UTF-16 length; it cuts markup, so it is only
// used for single lines that are longer than a message.
func pages(s string, size int) []string {
	var out []string
	var chunk []rune
	units := 0
	for _, r := range s {
		n := 1
		if r > 0xffff {
			n = 2
		}
		if units+n > size && len(chunk) > 0 {
			out = append(out, string(chunk))
			chunk = nil
			units = 0
		}
		chunk = append(chunk, r)
		units += n
	}
	if len(chunk) > 0 {
		out = append(out, string(chunk))
	}
	return out
}

// htmlPages packs whole lines so an anchor or <i> tag is never cut in half.
func htmlPages(s string, size int) []string {
	var out []string
	var cur []string
	curLen := 0
	flush := func() {
		if len(cur) > 0 {
			out = append(out, strings.Join(cur, "\n"))
			cur = nil
			curLen = 0
		}
	}
	for _, line := range strings.Split(s, "\n") {
		n := utf16Len(line)
		if n > size {
			flush()
			out = append(out, pages(line, size)...)
			continue
		}
		if curLen+n+1 > size {
			flush()
		}
		cur = append(cur, line)
		if curLen > 0 {
			curLen++
		}
		curLen += n
	}
	flush()
	return out
}
func (p *Plugin) progress(ctx context.Context, ev api.Event, text string) error {
	for i, part := range htmlPages(text, 3500) {
		c := api.Call{Target: ev.ChatID, Text: part, HTML: true}
		if i == 0 {
			c.Method = "edit"
			c.MessageID = ev.MessageID
		} else {
			c.Method = "send"
			c.ReplyTo = ev.MessageID
		}
		if _, e := p.call(ctx, c); e != nil {
			return e
		}
	}
	return nil
}
func (p *Plugin) notify(ctx context.Context, n Notify, report string) string {
	if n.Token == "" || n.Target == "" {
		return "通知未发送：未配置 Bot Token 或接收人"
	}
	ps := htmlPages(report, 3000)
	for i, part := range ps {
		body, _ := json.Marshal(map[string]string{"chat_id": n.Target, "text": part, "parse_mode": "HTML"})
		req, e := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.telegram.org/bot"+n.Token+"/sendMessage", bytes.NewReader(body))
		if e != nil {
			return "通知未发送：Bot Token 格式无效"
		}
		req.Header.Set("Content-Type", "application/json")
		res, e := p.HTTP.Do(req)
		if e != nil {
			return fmt.Sprintf("通知发送失败（已发送 %d/%d 页）：网络请求失败", i, len(ps))
		}
		data, e := io.ReadAll(io.LimitReader(res.Body, 65536))
		res.Body.Close()
		var reply struct {
			OK bool `json:"ok"`
		}
		parseErr := json.Unmarshal(data, &reply)
		if e != nil || res.StatusCode != 200 || parseErr != nil || !reply.OK {
			return fmt.Sprintf("通知发送失败（已发送 %d/%d 页，HTTP %d）", i, len(ps), res.StatusCode)
		}
	}
	return "报告已通过 Bot 发送（" + fmt.Sprint(len(ps)) + " 页）"
}
