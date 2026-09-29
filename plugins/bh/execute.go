package bh

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	api "github.com/OrionG-hub/laowangbot/pkg/pluginapi"
	"regexp"
	"strings"
	"time"
)

type checkResult struct {
	Active string
	Days   int
	Date   string
	Err    error
}

var activePattern = regexp.MustCompile(`(?:上次(?:活动|观看)|活跃状态).*?(\d{4}[-/]\d{2}[-/]\d{2}(?:\s+\d{2}:\d{2}:\d{2})?)`)
var daysPattern = regexp.MustCompile(`(?:到期时间|保号说明).*?(\d+)\s*天`)
var datePattern = regexp.MustCompile(`(?:到期时间|保号说明).*?(\d{4}[-/]\d{2}[-/]\d{2}(?:\s+\d{2}:\d{2}:\d{2})?)`)

func extract(text string, t Task) checkResult {
	r := checkResult{}
	if t.Mode == "embyboss" {
		if m := activePattern.FindStringSubmatch(text); len(m) > 1 {
			r.Active = m[1]
		}
		if m := daysPattern.FindStringSubmatch(text); len(m) > 1 {
			var e error
			r.Days, e = integer(m[1])
			if e != nil || r.Days == 0 || r.Days > 365000 {
				return checkResult{Err: errors.New("到期天数无效")}
			}
		} else if m := datePattern.FindStringSubmatch(text); len(m) > 1 {
			r.Date = m[1]
		}
	} else {
		re, e := compile(t.Regex)
		if e != nil {
			return checkResult{Err: e}
		}
		m, e := re.FindStringMatch(text)
		if e != nil {
			return checkResult{Err: fmt.Errorf("正则匹配失败: %w", e)}
		}
		if m != nil && m.GroupByNumber(1) != nil {
			r.Active = strings.TrimSpace(m.GroupByNumber(1).String())
		}
	}
	if r.Active == "" {
		r.Err = errors.New("未匹配到上次活动时间")
	} else if _, e := parseDate(r.Active); e != nil {
		r.Err = fmt.Errorf("活动时间无效: %s", r.Active)
	}
	if r.Date != "" {
		if _, e := parseDate(r.Date); e != nil {
			r.Err = errors.New("到期日期无效")
		}
	}
	return r
}
func fingerprint(m api.Message) string {
	b, _ := json.Marshal(struct {
		Text    string
		Buttons []api.Button
	}{m.Text, m.Buttons})
	return string(b)
}
func snapshotMessages(ms []api.Message) map[int]string {
	m := map[int]string{}
	for _, v := range ms {
		m[v.ID] = fingerprint(v)
	}
	return m
}
func fresh(ms []api.Message, base map[int]string, maxID int) []api.Message {
	var out []api.Message
	for _, m := range ms {
		if m.Out {
			continue
		}
		fp, exists := base[m.ID]
		if m.ID > maxID || exists && fp != fingerprint(m) {
			out = append(out, m)
		}
	}
	return out
}
func (p *Plugin) history(ctx context.Context, target string) ([]api.Message, error) {
	r, e := p.call(ctx, api.Call{Method: "history", Target: target, Limit: 20})
	return r.Messages, e
}
func (p *Plugin) execute(ctx context.Context, t Task) checkResult {
	fail := func(e error) checkResult { return checkResult{Err: e} }
	if e := ctx.Err(); e != nil {
		return fail(e)
	}
	target := t.Bot
	r, e := p.call(ctx, api.Call{Method: "resolve", Target: target})
	if e != nil {
		return fail(fmt.Errorf("解析 Bot 失败: %w", e))
	}
	if r.Entity != nil && r.Entity.ID != "" {
		target = r.Entity.ID
	}
	before, e := p.history(ctx, target)
	if e != nil {
		return fail(e)
	}
	base := snapshotMessages(before)
	maxID := 0
	for _, m := range before {
		if m.ID > maxID {
			maxID = m.ID
		}
	}
	commands := []string{"/myinfo"}
	if t.Mode == "custom" {
		commands = strings.Split(t.Command, "|")
	}
	_, e = p.call(ctx, api.Call{Method: "send", Target: target, Text: strings.TrimSpace(commands[0])})
	if e != nil {
		return fail(e)
	}
	for stage := 1; stage < len(commands); stage++ {
		deadline := time.Now().Add(p.ResultWait)
		clicked := false
		for !time.Now().After(deadline) {
			ms, e := p.history(ctx, target)
			if e != nil {
				return fail(e)
			}
			for _, m := range fresh(ms, base, maxID) {
				for _, b := range m.Buttons {
					if b.Kind != "callback" {
						continue
					}
					match := strings.Contains(b.Text, strings.TrimSpace(commands[stage])) || strings.Contains(strings.TrimSpace(commands[stage]), b.Text)
					if !match {
						re, e := compile("(?i)" + strings.TrimSpace(commands[stage]))
						if e == nil {
							match, _ = re.MatchString(b.Text)
						}
					}
					if !match {
						continue
					}
					// The next stage must observe a new message or changed text/buttons, not a stale keyboard/result.
					base = snapshotMessages(ms)
					for _, v := range ms {
						if v.ID > maxID {
							maxID = v.ID
						}
					}
					clicked = true
					r, e = p.call(ctx, api.Call{Method: "click", Target: target, MessageID: m.ID, Row: b.Row, Column: b.Column})
					if e != nil {
						return fail(e)
					}
					if stage == len(commands)-1 && r.Text != "" {
						result := extract(r.Text, t)
						if result.Err == nil {
							return result
						}
					}
					break
				}
				if clicked {
					break
				}
			}
			if clicked {
				break
			}
			if e = sleep(ctx, p.Poll); e != nil {
				return fail(e)
			}
		}
		if !clicked {
			return fail(fmt.Errorf("未找到内联按钮: %s", commands[stage]))
		}
	}
	deadline := time.Now().Add(p.ResultWait)
	last := errors.New("未获取到新的 Bot 回复内容")
	for !time.Now().After(deadline) {
		ms, e := p.history(ctx, target)
		if e != nil {
			return fail(e)
		}
		for _, m := range fresh(ms, base, maxID) {
			result := extract(m.Text, t)
			if result.Err == nil {
				return result
			}
			last = result.Err
		}
		if e = sleep(ctx, p.Poll); e != nil {
			return fail(e)
		}
	}
	return fail(last)
}
func parseDate(s string) (time.Time, error) {
	s = strings.ReplaceAll(strings.TrimSpace(s), "/", "-")
	for _, layout := range []string{"2006-01-02 15:04:05", "2006-01-02", "2006-01-02 15:04", time.RFC3339} {
		if t, e := time.ParseInLocation(layout, s, time.Local); e == nil {
			return t, nil
		}
	}
	return time.Time{}, errors.New("日期格式无效")
}
