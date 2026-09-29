package bh

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	api "github.com/OrionG-hub/laowangbot/pkg/pluginapi"
	"html"
	"io"
	"math"
	"net/http"
	"time"
)

func remaining(t Task, now time.Time) (int, string, error) {
	active, e := parseDate(t.LastActive)
	if e != nil {
		return 0, "", e
	}
	days := t.ExpireDays
	source := "手动配置"
	var expiry time.Time
	if days == 0 && t.LastDate != "" {
		expiry, e = parseDate(t.LastDate)
		source = "固定日期/自动识别"
		if e != nil {
			return 0, "", e
		}
	} else {
		if days == 0 {
			days = t.LastDays
			source = "自动识别"
			if days == 0 {
				days = 30
				source = "默认保底"
			}
		}
		expiry = time.Unix(active.Unix()+int64(days)*86400, int64(active.Nanosecond()))
		source = fmt.Sprintf("%d天/%s", days, source)
	}
	return int(math.Floor(float64(expiry.Unix()-now.Unix()) / 86400)), source, nil
}
func activity(t Task, now time.Time) string {
	if t.LastActive == "" {
		return "无记录"
	}
	n, _, e := remaining(t, now)
	if e != nil {
		return t.LastActive + "（解析异常）"
	}
	a, _ := parseDate(t.LastActive)
	return fmt.Sprintf("%s（已隔 %d 天 / 剩 %d 天）", t.LastActive, int(math.Floor(now.Sub(a).Hours()/24)), n)
}
func reportLine(t Task, err error, n Notify, now time.Time) (string, bool) {
	name := t.Remark
	if name == "" {
		name = t.Bot
	}
	prefix := fmt.Sprintf("%s (@%s)", name, t.Bot)
	days, limit, e := remaining(t, now)
	if e != nil {
		if err != nil {
			return "❌ " + prefix + ": " + err.Error(), true
		}
		return "❌ " + prefix + ": 日期解析失败", true
	}
	icon := "🟢"
	alert := days <= n.Warning
	if days < 0 {
		icon = "⚫️"
	} else if days <= n.Danger {
		icon = "🔴"
	} else if alert {
		icon = "🟡"
	}
	line := fmt.Sprintf("%s %s: 剩余 %d 天（上限:%s，上次:%s）", icon, prefix, days, limit, t.LastActive)
	if err != nil {
		line += "；本次抓取失败，按历史推算: " + err.Error()
	}
	return line, alert
}
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
func (p *Plugin) progress(ctx context.Context, ev api.Event, text string) error {
	for i, part := range pages(text, 3500) {
		c := api.Call{Target: ev.ChatID, Text: part}
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
	ps := pages(report, 3000)
	for i, part := range ps {
		body, _ := json.Marshal(map[string]string{"chat_id": n.Target, "text": "<pre>" + html.EscapeString(part) + "</pre>", "parse_mode": "HTML"})
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
