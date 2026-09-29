package bh

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	api "github.com/OrionG-hub/laowangbot/pkg/pluginapi"
	"strconv"
	"strings"
	"time"
)

func (p *Plugin) command(ctx context.Context, a []string, ev api.Event) (string, error) {
	if len(a) == 0 || a[0] == "help" {
		return Help, nil
	}
	sub := strings.ToLower(a[0])
	a = a[1:]
	if sub == "tpm" {
		return p.transfer(ctx, a, ev)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	d := clone(p.db)
	switch sub {
	case "config":
		if len(a) != 0 {
			return "", errors.New("用法: bh config")
		}
		token := "未设置"
		if d.Notify.Token != "" {
			token = "********"
		}
		target := d.Notify.Target
		if target == "" {
			target = "未设置"
		}
		return fmt.Sprintf("通知配置\nToken: %s\n接收人: %s\n预警阈值: %d 天\n危险阈值: %d 天", token, target, d.Notify.Warning, d.Notify.Danger), nil
	case "bottoken", "target":
		if len(a) != 1 || a[0] == "" {
			return "", fmt.Errorf("%s 需要一个非空参数", sub)
		}
		if sub == "bottoken" {
			if !strings.Contains(a[0], ":") {
				return "", errors.New("Bot Token 格式无效")
			}
			d.Notify.Token = a[0]
		} else {
			d.Notify.Target = a[0]
		}
		if e := p.commit(d); e != nil {
			return "", e
		}
		return "通知配置已保存", nil
	case "threshold":
		if len(a) != 2 {
			return "", errors.New("用法: bh threshold 7 3")
		}
		w, e := integer(a[0])
		if e != nil {
			return "", e
		}
		g, e := integer(a[1])
		if e != nil {
			return "", e
		}
		d.Notify.Warning = w
		d.Notify.Danger = g
		if e = p.commit(d); e != nil {
			return "", e
		}
		return "通知阈值已保存", nil
	case "add", "badd", "batchadd":
		c, rest, e := cronArgs(a)
		if e != nil {
			return "", e
		}
		var added []Task
		if sub == "add" {
			if len(rest) < 4 {
				return "", errors.New("用法: bh add [Cron] [Bot] [模式] [命令或-] [正则或-] [选项] [备注]")
			}
			bot, e := botName(rest[0])
			if e != nil {
				return "", e
			}
			t := Task{Cron: c, Bot: bot, Mode: rest[1], Command: rest[2], Regex: rest[3], Remark: "未命名"}
			if t.Command == "-" {
				t.Command = ""
			}
			if t.Regex == "-" {
				t.Regex = ""
			}
			rest = rest[4:]
			for len(rest) > 0 {
				switch strings.ToLower(rest[0]) {
				case "random":
					rest = rest[1:]
					v := "on"
					if len(rest) > 0 && rest[0] == "" {
						return "", errors.New("random 参数不能为空")
					}
					if len(rest) > 0 && (rangePattern.MatchString(rest[0]) || rest[0] == "on" || rest[0] == "off" || rest[0] == "true" || rest[0] == "false") {
						v = rest[0]
						rest = rest[1:]
					} else if len(rest) > 0 && len(rest[0]) > 0 && strings.ContainsAny(rest[0][:1], "0123456789-") {
						return "", errors.New("random 范围格式无效")
					}
					if e = setRandom(&t, v); e != nil {
						return "", e
					}
				case "expire":
					if len(rest) < 2 {
						return "", errors.New("expire 缺少天数")
					}
					t.ExpireDays, e = integer(rest[1])
					if e != nil {
						return "", e
					}
					rest = rest[2:]
				default:
					t.Remark = strings.Join(rest, " ")
					rest = nil
				}
			}
			added = []Task{t}
		} else {
			if len(rest) == 0 {
				return "", errors.New("至少提供一个 Bot")
			}
			for _, v := range rest {
				bot, e := botName(v)
				if e != nil {
					return "", e
				}
				t := Task{Cron: c, Bot: bot, Mode: "embyboss", Remark: bot}
				_ = setRandom(&t, "0-15")
				res, e := p.call(ctx, api.Call{Method: "resolve", Target: bot})
				if e == nil && res.Entity != nil && res.Entity.Name != "" {
					t.Remark = res.Entity.Name
				}
				added = append(added, t)
			}
		}
		n, _ := strconv.ParseUint(d.Seq, 10, 63)
		for i := range added {
			n++
			if n > 1<<63-1 {
				return "", errors.New("任务序号超出范围")
			}
			added[i].ID = strconv.FormatUint(n, 10)
			d.Tasks = append(d.Tasks, added[i])
		}
		d.Seq = strconv.FormatUint(n, 10)
		if e = p.commit(d); e != nil {
			return "", e
		}
		var ids []string
		for _, t := range added {
			ids = append(ids, t.ID)
		}
		return fmt.Sprintf("已添加 %d 个任务，ID: %s", len(added), strings.Join(ids, ", ")), nil
	case "edit", "set":
		props := map[string]bool{"cron": true, "bot": true, "mode": true, "cmd": true, "regex": true, "random": true, "expire": true, "remark": true}
		idx := -1
		for i, v := range a {
			if props[strings.ToLower(v)] {
				idx = i
				break
			}
		}
		if idx < 1 || idx+1 >= len(a) {
			return "", errors.New("用法: bh edit [ID/范围/all] [cron/bot/mode/cmd/regex/random/expire/remark] [值]")
		}
		ids, missing := resolve(d.Tasks, a[:idx], false)
		if len(ids) == 0 || len(missing) > 0 {
			return "", fmt.Errorf("任务不存在: %s", strings.Join(missing, ", "))
		}
		prop := strings.ToLower(a[idx])
		v := strings.Join(a[idx+1:], " ")
		for i := range d.Tasks {
			t := &d.Tasks[i]
			if !ids[t.ID] {
				continue
			}
			var e error
			switch prop {
			case "cron":
				if v == "now" {
					t.Cron, _, e = cronArgs([]string{"now"})
				} else {
					t.Cron = v
				}
			case "bot":
				t.Bot, e = botName(v)
			case "mode":
				t.Mode = v
			case "cmd":
				t.Command = v
				if v == "-" {
					t.Command = ""
				}
			case "regex":
				t.Regex = v
				if v == "-" {
					t.Regex = ""
				}
			case "random":
				e = setRandom(t, v)
			case "expire":
				t.ExpireDays, e = integer(v)
			case "remark":
				t.Remark = v
			}
			if e != nil {
				return "", e
			}
			if prop == "bot" || prop == "mode" || prop == "cmd" || prop == "regex" {
				t.LastActive = ""
				t.LastDays = 0
				t.LastDate = ""
				t.LastResult = ""
				t.LastRun = ""
			}
		}
		if e := p.commit(d); e != nil {
			return "", e
		}
		p.cancelIDs(ids)
		return fmt.Sprintf("已修改 %d 个任务的 %s", len(ids), prop), nil
	case "list", "ls", "info":
		if sub != "info" && len(a) != 0 {
			return "", errors.New("list/ls 不接受参数")
		}
		ts := d.Tasks
		if sub == "info" {
			if len(a) != 1 {
				return "", errors.New("用法: bh info [ID/all]")
			}
			ids, _ := resolve(d.Tasks, a, false)
			ts = nil
			for _, t := range d.Tasks {
				if ids[t.ID] {
					ts = append(ts, t)
				}
			}
			if len(ts) == 0 {
				return "", errors.New("找不到指定任务")
			}
		}
		if len(ts) == 0 {
			return "暂无保号任务", nil
		}
		var lines []string
		for _, t := range ts {
			state := "启用"
			if t.Disabled {
				state = "暂停"
			}
			line := fmt.Sprintf("%s %s - %s (@%s)\n上次活动: %s", t.ID, state, t.Remark, t.Bot, activity(t, time.Now()))
			if sub == "info" {
				random := "关"
				if t.Random {
					lo, hi := randomBounds(t)
					random = fmt.Sprintf("%.4g-%.4g 分钟", float64(lo)/60000, float64(hi)/60000)
				}
				expire := "自动识别"
				if t.ExpireDays > 0 {
					expire = fmt.Sprintf("%d 天", t.ExpireDays)
				}
				line += fmt.Sprintf("\nCron: %s\n模式: %s\n命令: %s\n正则: %s\n随机延迟: %s\n手动过期: %s\n上次执行: %s\n上次结果: %s", t.Cron, t.Mode, t.Command, t.Regex, random, expire, t.LastRun, t.LastResult)
			}
			lines = append(lines, line)
		}
		return strings.Join(lines, "\n\n"), nil
	case "rm", "disable", "enable":
		if len(a) == 0 || (sub != "rm" && len(a) != 1) {
			return "", errors.New("请提供任务 ID（rm 也支持范围/机器人名/备注/all）")
		}
		ids, missing := resolve(d.Tasks, a, sub == "rm")
		if len(ids) == 0 {
			return "", fmt.Errorf("找不到任务: %s", strings.Join(a, " "))
		}
		var ts []Task
		for _, t := range d.Tasks {
			if ids[t.ID] {
				if sub == "rm" {
					continue
				}
				t.Disabled = sub == "disable"
			}
			ts = append(ts, t)
		}
		d.Tasks = ts
		if sub == "rm" && len(ts) == 0 {
			d.Seq = "0"
		}
		if e := p.commit(d); e != nil {
			return "", e
		}
		if sub != "enable" {
			p.cancelIDs(ids)
		}
		out := fmt.Sprintf("%s 完成，影响 %d 个任务", sub, len(ids))
		if len(missing) > 0 {
			out += "；未找到: " + strings.Join(missing, ", ")
		}
		return out, nil
	case "now":
		if len(a) > 1 {
			return "", errors.New("用法: bh now [ID]")
		}
		var ts []Task
		for _, t := range d.Tasks {
			if len(a) == 1 && t.ID == a[0] || len(a) == 0 && !t.Disabled {
				ts = append(ts, t)
			}
		}
		if len(ts) == 0 {
			return "", errors.New("没有可执行的任务或 ID 不存在")
		}
		for _, t := range ts {
			if p.jobs[t.ID] != nil {
				return "", fmt.Errorf("任务 %s 正在执行或等待", t.ID)
			}
		}
		if ev.ChatID == "" || ev.MessageID == 0 {
			return "", errors.New("缺少原命令消息，无法展示检查进度")
		}
		if e := p.progress(ctx, ev, fmt.Sprintf("正在检测 %d 个任务，请稍候…", len(ts))); e != nil {
			return "", fmt.Errorf("无法更新进度: %w", e)
		}
		return "", p.enqueue(ts, true, ev, time.Now())
	default:
		return "", fmt.Errorf("未知子命令 %s；使用 bh 查看帮助", sub)
	}
}

const maxConfigBytes = 512 * 1024

func (p *Plugin) transfer(ctx context.Context, a []string, ev api.Event) (string, error) {
	if len(a) != 1 || (a[0] != "i" && a[0] != "ul") {
		return "", errors.New("用法: bh tpm ul 或回复配置文件执行 bh tpm i")
	}
	if ev.ChatID == "" {
		return "", errors.New("缺少目标会话")
	}
	if a[0] == "ul" {
		d := p.snapshot()
		d.Notify.Token = ""
		d.Notify.Target = ""
		b, e := json.MarshalIndent(d, "", "  ")
		if e != nil {
			return "", e
		}
		if len(b) > maxConfigBytes {
			return "", errors.New("配置超过512KiB导出上限")
		}
		_, e = p.call(ctx, api.Call{Method: "send_file", Target: ev.ChatID, Filename: "baohao_config.json", MimeType: "application/json", Bytes: b, Text: "保号配置备份（Token/ChatID 已去除）", ReplyTo: ev.MessageID})
		if e != nil {
			return "", e
		}
		return "已导出脱敏配置", nil
	}
	if ev.ReplyToID == 0 {
		return "", errors.New("请回复 baohao_config.json 文件")
	}
	r, e := p.call(ctx, api.Call{Method: "messages", Target: ev.ChatID, IDs: []int{ev.ReplyToID}})
	if e != nil {
		return "", e
	}
	var found *api.Message
	for i := range r.Messages {
		if r.Messages[i].ID == ev.ReplyToID {
			found = &r.Messages[i]
		}
	}
	if found == nil || !found.HasMedia || found.Filename != "baohao_config.json" {
		return "", errors.New("回复文件名必须为 baohao_config.json")
	}
	r, e = p.call(ctx, api.Call{Method: "download", Target: ev.ChatID, MessageID: ev.ReplyToID})
	if e != nil {
		return "", e
	}
	if len(r.Bytes) == 0 || len(r.Bytes) > maxConfigBytes {
		return "", errors.New("配置文件为空或超过512KiB")
	}
	var raw map[string]json.RawMessage
	if e = json.Unmarshal(r.Bytes, &raw); e != nil {
		return "", errors.New("配置 JSON 格式无效")
	}
	for _, key := range []string{"seq", "tasks", "notify"} {
		if len(raw[key]) == 0 || string(raw[key]) == "null" {
			return "", fmt.Errorf("配置缺少 %s", key)
		}
	}
	var d DB
	if e = json.Unmarshal(r.Bytes, &d); e != nil {
		return "", errors.New("配置字段类型无效")
	}
	if e = validateDB(d); e != nil {
		return "", e
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	d.Notify.Token = p.db.Notify.Token
	d.Notify.Target = p.db.Notify.Target
	if e = p.commit(d); e != nil {
		return "", e
	}
	for _, j := range p.jobs {
		j.cancel()
	}
	return "配置已导入，保留本地 Token/ChatID，定时任务已重载", nil
}
