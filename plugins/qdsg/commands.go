package qdsg

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	api "github.com/OrionG-hub/laowangbot/pkg/pluginapi"
	"math"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const Help = "qdsg add [now/秒 分 时 日 月 星期] @bot [text/reply/inline/imagechoice/xigua/math/appcf/moon] 内容 [ai provider 提示词] [steps N] [retry N] [interval 分钟] [等待毫秒] [random 最小-最大分钟] [备注]\nlist|ls; now/rm/enable/disable ID、范围、名称、all; edit ID 属性 值; reorder|reload; notify on/off/@user/me; cfbucket ID; aiconfig list|set key value|addcustom ID URL Model Key|rmcustom ID; test provider 提示词（回复消息）"

var modes = map[string]string{"text": "text", "reply": "reply_button", "reply_button": "reply_button", "inline": "inline_button", "inline_button": "inline_button", "imagechoice": "image_choice", "image_choice": "image_choice", "xigua": "xigua_sequence", "xigua_sequence": "xigua_sequence", "math": "math", "calc": "math", "appcf": "app_cf", "app_cf": "app_cf", "app-cf": "app_cf", "moon": "moon"}

func botName(s string) string {
	if strings.Contains(s, "t.me/") {
		if u, e := url.Parse(s); e == nil && u.Path != "" {
			return strings.Split(strings.Trim(u.Path, "/"), "/")[0]
		}
	}
	return strings.TrimPrefix(s, "@")
}
func cronValue(s string) (string, error) {
	if s == "now" {
		n := time.Now()
		s = fmt.Sprintf("%d %d %d * * *", n.Second(), n.Minute(), n.Hour())
	}
	_, e := parser.Parse(s)
	return s, e
}
func randomRange(s string) (int, int, error) {
	a := strings.Split(s, "-")
	if len(a) > 2 {
		return 0, 0, errors.New("随机范围无效")
	}
	lo := 0.
	hi, e := strconv.ParseFloat(a[0], 64)
	if len(a) == 2 {
		lo = hi
		hi, e = strconv.ParseFloat(a[1], 64)
	}
	if e != nil || math.IsNaN(lo) || math.IsNaN(hi) || math.IsInf(lo, 0) || math.IsInf(hi, 0) || lo < 0 || hi < lo || hi > 525600 {
		return 0, 0, errors.New("随机范围无效（分钟）")
	}
	return int(lo * 60000), int(hi * 60000), nil
}
func selectIDs(ts []Task, args []string) map[string]bool {
	out := map[string]bool{}
	for _, raw := range args {
		for _, a := range strings.Split(raw, ",") {
			a = strings.ToLower(strings.TrimPrefix(a, "@"))
			if a == "all" {
				for _, t := range ts {
					out[t.ID] = true
				}
				continue
			}
			if regexp.MustCompile("^[0-9]+-[0-9]+$").MatchString(a) {
				v := strings.Split(a, "-")
				l, _ := strconv.Atoi(v[0])
				h, _ := strconv.Atoi(v[1])
				if l > h {
					l, h = h, l
				}
				for _, t := range ts {
					n, _ := strconv.Atoi(t.ID)
					if n >= l && n <= h {
						out[t.ID] = true
					}
				}
				continue
			}
			exact := false
			for _, t := range ts {
				if a == t.ID || a == strings.ToLower(t.Bot) || a == strings.ToLower(t.Remark) {
					out[t.ID] = true
					exact = true
				}
			}
			if !exact && a != "" {
				for _, t := range ts {
					if strings.Contains(strings.ToLower(t.Bot+" "+t.Remark+" "+t.Display), a) {
						out[t.ID] = true
					}
				}
			}
		}
	}
	return out
}
func edit(t *Task, key, value string) error {
	n, e := strconv.Atoi(value)
	switch strings.ToLower(key) {
	case "cron", "time":
		t.Cron, e = cronValue(value)
		return e
	case "bot":
		t.Bot = botName(value)
		t.ChatID = ""
	case "mode":
		m, ok := modes[value]
		if !ok {
			return errors.New("未知模式")
		}
		t.Mode = m
		t.SendStart = m != "text" && m != "image_choice"
		if m == "app_cf" {
			t.AI = false
		}
	case "cmd", "command":
		v := strings.SplitN(value, "|", 2)
		t.Command = strings.TrimSpace(v[0])
		t.Secondary = ""
		if len(v) == 2 {
			t.Secondary = strings.TrimSpace(v[1])
		}
	case "wait":
		if e != nil || n < 0 || n > 60000 {
			return errors.New("wait 必须为 0–60000 毫秒")
		}
		t.Wait = n
	case "random":
		if value == "off" || value == "false" || value == "0" {
			t.Random = false
		} else if value == "on" || value == "true" {
			t.Random = true
		} else {
			l, h, e := randomRange(value)
			if e != nil {
				return e
			}
			t.Random = true
			t.RandomMin = l
			t.RandomMax = h
		}
	case "remark", "note":
		t.Remark = value
	case "ai", "useai":
		if value != "on" && value != "off" && value != "true" && value != "false" {
			return errors.New("ai 使用 on/off")
		}
		t.AI = value == "on" || value == "true"
	case "provider", "aiprovider":
		t.Provider = value
	case "prompt", "aiprompt":
		t.Prompt = strings.Trim(value, "\"")
	case "aitype", "aitarget":
		if value != "auto" && value != "text" && value != "image" {
			return errors.New("aitype: auto/text/image")
		}
		t.AITarget = value
	case "steps":
		if e != nil || n < 1 || n > 30 {
			return errors.New("steps: 1–30")
		}
		t.Steps = n
	case "retry", "retrycount":
		if e != nil || n < 0 || n > 100 {
			return errors.New("retry: 0–100")
		}
		t.Retry = n
	case "interval", "retryinterval":
		f, e := strconv.ParseFloat(value, 64)
		if e != nil || math.IsNaN(f) || math.IsInf(f, 0) || f <= 0 || f > 525600 {
			return errors.New("interval 必须为正分钟数")
		}
		t.Interval = f
	default:
		return errors.New("未知属性")
	}
	return nil
}
func parseAdd(a []string) (Task, error) {
	t := Task{Mode: "text", Command: "签到", Wait: 2000, RandomMax: 60000, Created: strconv.FormatInt(time.Now().UnixMilli(), 10)}
	if len(a) < 2 {
		return t, errors.New(Help)
	}
	i := 6
	if a[0] == "now" {
		i = 1
	}
	if len(a) <= i {
		return t, errors.New("缺少时间或机器人")
	}
	var e error
	t.Cron, e = cronValue(strings.Join(a[:i], " "))
	if e != nil {
		return t, e
	}
	t.Bot = botName(a[i])
	a = a[i+1:]
	if len(a) == 0 {
		t.Random = true
		return t, nil
	}
	if m, ok := modes[strings.ToLower(a[0])]; ok {
		t.Mode = m
		t.SendStart = m != "text" && m != "image_choice"
		a = a[1:]
		if m == "image_choice" {
			t.Command = "/checkin"
			t.AI = true
		}
		if m == "moon" {
			t.AI = true
		}
	}
	if len(a) > 0 {
		_ = edit(&t, "cmd", a[0])
		a = a[1:]
	}
	for i = 0; i < len(a); i++ {
		key := strings.ToLower(a[i])
		switch key {
		case "ai":
			t.AI = true
			if i+1 < len(a) {
				i++
				t.Provider = a[i]
			}
			if i+1 < len(a) && strings.HasPrefix(a[i+1], "\"") {
				i++
				parts := []string{a[i]}
				for !strings.HasSuffix(a[i], "\"") && i+1 < len(a) {
					i++
					parts = append(parts, a[i])
				}
				t.Prompt = strings.Trim(strings.Join(parts, " "), "\"")
			}
		case "steps", "retry", "interval", "random", "aitype":
			if i+1 >= len(a) {
				if key == "random" {
					t.Random = true
					continue
				}
				return t, errors.New("选项缺少值")
			}
			i++
			if e := edit(&t, key, a[i]); e != nil {
				return t, e
			}
		default:
			if _, e := strconv.Atoi(key); e == nil {
				if e = edit(&t, "wait", key); e != nil {
					return t, e
				}
				if t.Mode == "reply_button" || t.Mode == "inline_button" || t.Mode == "app_cf" {
					t.SendStart = t.Wait > 0
				}
			} else {
				t.Remark = strings.TrimSpace(t.Remark + " " + a[i])
			}
		}
	}
	return t, nil
}
func masked(v string) string {
	if v != "" {
		return "********"
	}
	return "未设置"
}
func (p *Plugin) command(ctx context.Context, a []string, event json.RawMessage) (string, error) {
	if len(a) == 0 {
		return Help, nil
	}
	sub := strings.ToLower(a[0])
	a = a[1:]
	if sub == "test" {
		var ev api.Event
		_ = json.Unmarshal(event, &ev)
		if ev.ReplyToID == 0 {
			return "", errors.New("请回复图片或文本消息")
		}
		r, e := p.host.Call(ctx, api.Call{Method: "messages", Target: ev.ChatID, IDs: []int{ev.ReplyToID}})
		if e != nil {
			return "", e
		}
		if len(r.Messages) == 0 {
			return "", errors.New("回复消息不存在")
		}
		t := Task{Bot: ev.ChatID, AI: true}
		if len(a) > 0 {
			t.Provider = a[0]
			t.Prompt = strings.Trim(strings.Join(a[1:], " "), "\"")
		}
		return p.recognize(ctx, t, r.Messages[0], nil)
	}
	if sub == "now" {
		ids := selectIDs(p.snapshot().Tasks, a)
		if len(ids) == 0 {
			return "", errors.New("未匹配到任务")
		}
		lines := []string{}
		for id := range ids {
			if e := p.enqueue(id, false); e != nil {
				lines = append(lines, id+": "+e.Error())
			} else {
				lines = append(lines, id+": 已排队")
			}
		}
		p.Tick(time.Now())
		return strings.Join(lines, "\n"), nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	save := func() (string, error) {
		if e := p.save(); e != nil {
			return "", e
		}
		return "已保存", nil
	}
	switch sub {
	case "help":
		return Help, nil
	case "list", "ls":
		var lines []string
		for _, t := range p.db.Tasks {
			status := "启用"
			if t.Disabled {
				status = "禁用"
			}
			line := fmt.Sprintf("%s %s @%s [%s] %s\nCron: %s; %s | %s\nwait=%d random=%t(%d-%dms) AI=%t/%s/%s steps=%d retry=%d interval=%g\n备注: %s; 提示词: %s\n上次: %s 结果: %s 错误: %s", t.ID, status, t.Bot, t.Mode, t.Display, t.Cron, t.Command, t.Secondary, t.Wait, t.Random, t.RandomMin, t.RandomMax, t.AI, t.Provider, t.AITarget, t.Steps, t.Retry, t.Interval, t.Remark, t.Prompt, t.LastRun, t.LastResult, t.LastError)
			if n := p.next[t.ID]; !t.Disabled && !n.IsZero() {
				line += "\n下次: " + n.Format(time.RFC3339)
			}
			lines = append(lines, line)
		}
		if len(lines) == 0 {
			return "暂无签到任务", nil
		}
		return strings.Join(lines, "\n\n"), nil
	case "add":
		t, e := parseAdd(a)
		if e != nil {
			return "", e
		}
		n, _ := strconv.Atoi(p.db.Seq)
		n++
		p.db.Seq = strconv.Itoa(n)
		t.ID = p.db.Seq
		p.db.Tasks = append(p.db.Tasks, t)
		s, _ := parser.Parse(t.Cron)
		p.next[t.ID] = s.Next(time.Now())
		if e = p.save(); e != nil {
			return "", e
		}
		return "已添加任务 " + t.ID, nil
	case "reload":
		var d DB
		if e := api.LoadJSON(p.dir+"/signin_config.json", &d); e != nil {
			return "", e
		}
		p.db = d
		p.next = map[string]time.Time{}
		for _, t := range d.Tasks {
			if s, e := parser.Parse(t.Cron); e == nil {
				p.next[t.ID] = s.Next(time.Now())
			}
		}
		return "已重载", nil
	case "reorder":
		if len(p.running) > 0 {
			return "", errors.New("存在运行/排队任务，请完成后重新编号")
		}
		p.next = map[string]time.Time{}
		for i := range p.db.Tasks {
			t := &p.db.Tasks[i]
			t.ID = strconv.Itoa(i + 1)
			if s, e := parser.Parse(t.Cron); e == nil {
				p.next[t.ID] = s.Next(time.Now())
			}
		}
		p.db.Seq = strconv.Itoa(len(p.db.Tasks))
		return save()
	case "rm", "enable", "disable":
		ids := selectIDs(p.db.Tasks, a)
		if len(ids) == 0 {
			return "", errors.New("未匹配到任务")
		}
		out := []Task{}
		for _, t := range p.db.Tasks {
			if ids[t.ID] {
				if sub == "rm" {
					delete(p.next, t.ID)
					continue
				}
				t.Disabled = sub == "disable"
			}
			out = append(out, t)
		}
		p.db.Tasks = out
		if len(out) == 0 && len(p.running) == 0 {
			p.db.Seq = "0"
		}
		return save()
	case "edit", "set":
		idx := -1
		for i, x := range a {
			probe := Task{}
			if e := edit(&probe, x, "x"); e == nil || e.Error() != "未知属性" {
				idx = i
				break
			}
		}
		if idx < 1 || idx >= len(a) {
			return "", errors.New("edit ID 属性 值")
		}
		ids := selectIDs(p.db.Tasks, a[:idx])
		if len(ids) == 0 {
			return "", errors.New("未匹配到任务")
		}
		ts := append([]Task(nil), p.db.Tasks...)
		for i := range ts {
			if ids[ts[i].ID] {
				if e := edit(&ts[i], a[idx], strings.Join(a[idx+1:], " ")); e != nil {
					return "", e
				}
				if s, e := parser.Parse(ts[i].Cron); e == nil {
					p.next[ts[i].ID] = s.Next(time.Now())
				}
			}
		}
		p.db.Tasks = ts
		return save()
	case "notify":
		if len(a) == 0 {
			return fmt.Sprintf("通知: %t → %s", p.db.Notify.Enabled, p.db.Notify.Target), nil
		}
		if a[0] == "on" || a[0] == "off" {
			p.db.Notify.Enabled = a[0] == "on"
		} else {
			p.db.Notify.Target = botName(a[0])
		}
		return save()
	case "cfbucket":
		if len(a) == 0 {
			return "cfbucket: " + p.db.Bucket, nil
		}
		if a[0] == "off" {
			p.db.Bucket = ""
		} else if regexp.MustCompile("^[A-Za-z0-9_-]+$").MatchString(a[0]) {
			p.db.Bucket = a[0]
		} else {
			return "", errors.New("Bucket ID 无效")
		}
		return save()
	case "aiconfig":
		if len(a) == 0 || a[0] == "list" {
			v := p.db.AI
			v.OpenAIKey = masked(v.OpenAIKey)
			v.GeminiKey = masked(v.GeminiKey)
			v.Custom = append([]Provider(nil), v.Custom...)
			for i := range v.Custom {
				v.Custom[i].Key = masked(v.Custom[i].Key)
			}
			b, _ := json.MarshalIndent(v, "", "  ")
			return string(b), nil
		}
		c := &p.db.AI
		switch a[0] {
		case "addcustom":
			if len(a) != 5 {
				return "", errors.New("addcustom ID URL Model Key")
			}
			v := Provider{a[1], a[2], a[3], a[4]}
			for i, x := range c.Custom {
				if x.ID == v.ID {
					c.Custom = append(c.Custom[:i], c.Custom[i+1:]...)
					break
				}
			}
			c.Custom = append(c.Custom, v)
		case "rmcustom":
			if len(a) != 2 {
				return "", errors.New("rmcustom ID")
			}
			for i, x := range c.Custom {
				if x.ID == a[1] {
					c.Custom = append(c.Custom[:i], c.Custom[i+1:]...)
					break
				}
			}
		case "set":
			if len(a) < 3 {
				return "", errors.New("set key value")
			}
			v := strings.Join(a[2:], " ")
			switch a[1] {
			case "provider":
				c.Default = v
			case "prompt":
				c.Prompt = v
			case "openai_key":
				c.OpenAIKey = v
			case "openai_base", "openai_base_url":
				c.OpenAIBase = v
			case "openai_model":
				c.OpenAIModel = v
			case "gemini_key":
				c.GeminiKey = v
			case "gemini_base", "gemini_base_url":
				c.GeminiBase = v
			case "gemini_model":
				c.GeminiModel = v
			default:
				return "", errors.New("未知 AI 配置项")
			}
		default:
			return "", errors.New("未知 aiconfig 命令")
		}
		return save()
	}
	return "", errors.New("未知命令\n" + Help)
}
