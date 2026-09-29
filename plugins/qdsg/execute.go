package qdsg

import (
	"context"
	"errors"
	"fmt"
	api "github.com/OrionG-hub/laowangbot/pkg/pluginapi"
	"golang.org/x/text/unicode/norm"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

var successRE = regexp.MustCompile("(?i)签到成功|今日已经签到|已经签到|已签到|\\bsuccess(?:ful(?:ly)?)?\\b|获得了?\\s*[0-9]+\\s*积分")
var failureRE = regexp.MustCompile("(?i)\\bunsuccessful(?:ly)?\\b|\\bnot\\s+(?:a\\s+)?success(?:ful(?:ly)?)?\\b|未签到成功|签到未成功|签到不成功|签到失败|验证失败|回答错误|选择错误|已过期|验证超时|不正确|重新生成")

func arithmetic(s string) (string, bool) {
	s = strings.NewReplacer("×", "*", "✕", "*", "✖", "*", "÷", "/", "−", "-", "–", "-", "—", "-", "＋", "+").Replace(s)
	for _, pat := range []string{"(?:计算|算式|请算)[：:]?\\s*(-?[0-9]+)\\s*([+*/-])\\s*(-?[0-9]+)", "(-?[0-9]+)\\s*([+*/-])\\s*(-?[0-9]+)\\s*(?:=|等于)"} {
		m := regexp.MustCompile(pat).FindStringSubmatch(s)
		if m == nil {
			continue
		}
		a, _ := strconv.ParseFloat(m[1], 64)
		b, _ := strconv.ParseFloat(m[3], 64)
		v := 0.
		switch m[2] {
		case "+":
			v = a + b
		case "-":
			v = a - b
		case "*":
			v = a * b
		case "/":
			if b == 0 {
				continue
			}
			v = a / b
		}
		return strconv.FormatFloat(math.Round(v*1000)/1000, 'f', -1, 64), true
	}
	return "", false
}
func dynamic(cmd, text string) string {
	if strings.HasPrefix(cmd, "re:") {
		cmd = cmd[3:]
		if r, e := regexp.Compile(cmd); e == nil {
			m := r.FindStringSubmatch(text)
			if len(m) > 1 {
				return strings.TrimSpace(m[1])
			}
		}
	}
	return cmd
}
func matches(text, cmd string) bool {
	if cmd == "" || cmd == "auto" || cmd == "none" {
		return true
	}
	if strings.Contains(text, cmd) || strings.Contains(cmd, text) {
		return true
	}
	r, e := regexp.Compile("(?i)" + cmd)
	return e == nil && r.MatchString(text)
}
func matchAnswer(answer string, buttons []api.Button, unique bool) (api.Button, error) {
	clean := func(s string) string {
		s = norm.NFKC.String(s)
		s = strings.Map(func(r rune) rune {
			if strings.ContainsRune("`*_#“”‘’\"'。.!！,，:：;；", r) {
				return -1
			}
			return r
		}, s)
		return strings.ToLower(strings.TrimSpace(s))
	}
	a := clean(answer)
	if a == "" {
		return api.Button{}, errors.New("空 AI 答案")
	}
	hits := []api.Button{}
	for _, b := range buttons {
		if clean(b.Text) == a {
			hits = append(hits, b)
		}
	}
	if len(hits) == 1 {
		return hits[0], nil
	}
	hits = nil
	for _, b := range buttons {
		v := clean(b.Text)
		if v != "" && (strings.Contains(a, v) || strings.Contains(v, a)) {
			hits = append(hits, b)
		}
	}
	if len(hits) > 0 && (!unique || len(hits) == 1) {
		return hits[0], nil
	}
	return api.Button{}, fmt.Errorf("答案 %q 无法唯一匹配选项", answer)
}
func normalizeEmoji(s string) string {
	return strings.TrimSpace(strings.NewReplacer("\ufe0e", "", "\ufe0f", "").Replace(norm.NFC.String(s)))
}
func sequence(m api.Message) []string {
	text := m.Text
	if i := strings.Index(text, "目标序列"); i >= 0 {
		text = text[i+len("目标序列"):]
	}
	available := map[string]bool{}
	for _, b := range m.Buttons {
		available[normalizeEmoji(b.Text)] = true
	}
	out := []string{}
	for _, tok := range strings.Fields(text) {
		tok = normalizeEmoji(strings.Trim(tok, "：:，,。；;"))
		if available[tok] {
			out = append(out, tok)
		}
	}
	if regexp.MustCompile("从\\s*右往左").MatchString(m.Text) {
		for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
			out[i], out[j] = out[j], out[i]
		}
	}
	return out
}
func (p *Plugin) history(ctx context.Context, bot string) ([]api.Message, error) {
	r, e := p.host.Call(ctx, api.Call{Method: "history", Target: bot, Limit: 12})
	if e != nil {
		return nil, e
	}
	sort.SliceStable(r.Messages, func(i, j int) bool { return r.Messages[i].ID > r.Messages[j].ID })
	return r.Messages, nil
}

type cursor struct {
	max  int
	seen map[int]string
}

func newCursor(ms []api.Message) *cursor {
	c := &cursor{seen: map[int]string{}}
	for _, m := range ms {
		if m.ID > c.max {
			c.max = m.ID
		}
		c.seen[m.ID] = fingerprint(m)
	}
	return c
}
func (c *cursor) fresh(m api.Message) bool {
	if m.Out {
		return false
	}
	old, ok := c.seen[m.ID]
	return m.ID > c.max || (ok && old != fingerprint(m))
}
func (p *Plugin) waitFresh(ctx context.Context, bot string, c *cursor, d time.Duration, predicate func(api.Message) bool) ([]api.Message, error) {
	end := time.Now().Add(d)
	for {
		ms, e := p.history(ctx, bot)
		if e != nil {
			return nil, e
		}
		fresh := []api.Message{}
		matched := false
		for _, m := range ms {
			if c.fresh(m) {
				fresh = append(fresh, m)
				if predicate == nil || predicate(m) {
					matched = true
				}
			}
		}
		if matched {
			return fresh, nil
		}
		if time.Now().After(end) {
			return nil, errors.New("等待新消息或消息更新超时")
		}
		if e = sleep(ctx, p.Poll); e != nil {
			return nil, e
		}
	}
}
func (p *Plugin) send(ctx context.Context, bot, text string) error {
	_, e := p.host.Call(ctx, api.Call{Method: "send", Target: bot, Text: text})
	return e
}

// callbackSuccess unwinds nested button workflows when Telegram confirms in a toast only.
type callbackSuccess string

func (s callbackSuccess) Error() string { return string(s) }

var errButtonNotFound = errors.New("找不到按钮")

func callbackOutcome(result string, err error) (string, error) {
	var success callbackSuccess
	if errors.As(err, &success) {
		return string(success), nil
	}
	return result, err
}
func (p *Plugin) click(ctx context.Context, bot string, m api.Message, b api.Button) error {
	if len(b.Data) == 0 && (b.Kind == "reply" || b.Kind == "KeyboardButton" || b.Kind == "text") {
		return p.send(ctx, bot, b.Text)
	}
	r, e := p.host.Call(ctx, api.Call{Method: "click", Target: bot, MessageID: m.ID, Row: b.Row, Column: b.Column})
	if e != nil {
		return e
	}
	result, err := terminal([]api.Message{{Text: r.Text}})
	if err != nil {
		return err
	}
	if result != "" {
		return callbackSuccess(result)
	}
	return nil
}
func (p *Plugin) entry(ctx context.Context, t Task, ms []api.Message, cmd string, app bool) (api.Message, api.Button, error) {
	for _, m := range ms {
		if m.Out {
			continue
		}
		want := dynamic(cmd, m.Text)
		for _, b := range m.Buttons {
			if app && b.URL == "" {
				continue
			}
			if !app && b.URL != "" {
				continue
			}
			if matches(b.Text, want) {
				if !app {
					if e := p.click(ctx, t.Bot, m, b); e != nil {
						return m, b, e
					}
				}
				return m, b, nil
			}
		}
	}
	return api.Message{}, api.Button{}, fmt.Errorf("%w %q", errButtonNotFound, cmd)
}
func cfURL(m api.Message) string {
	for _, b := range m.Buttons {
		if strings.Contains(b.URL, "tgbot.lyrebirdemby.com/turnstile") {
			return b.URL
		}
	}
	for _, u := range m.URLs {
		if strings.Contains(u, "tgbot.lyrebirdemby.com/turnstile") {
			return u
		}
	}
	return regexp.MustCompile("https://tgbot\\.lyrebirdemby\\.com/turnstile[^\\s<>]+").FindString(m.Text)
}
func terminal(ms []api.Message) (string, error) {
	for _, m := range ms {
		if failureRE.MatchString(m.Text) {
			return "", errors.New(m.Text)
		}
		if successRE.MatchString(m.Text) {
			return m.Text, nil
		}
	}
	return "", nil
}
func (p *Plugin) result(ctx context.Context, t Task, c *cursor) (string, error) {
	ms, e := p.waitFresh(ctx, t.Bot, c, p.ResultWait, func(m api.Message) bool {
		return successRE.MatchString(m.Text) || failureRE.MatchString(m.Text) || cfURL(m) != ""
	})
	if e != nil {
		return "", e
	}
	if s, e := terminal(ms); s != "" || e != nil {
		return s, e
	}
	for _, m := range ms {
		if u := cfURL(m); u != "" {
			return p.solveCF(ctx, t, u, "", false, c, 60*time.Second)
		}
	}
	return "", errors.New("机器人未明确确认签到成功")
}
func (p *Plugin) Execute(ctx context.Context, t Task) (string, error) {
	if t.Bot == "" {
		return "", errors.New("机器人为空")
	}
	if t.Mode == "moon" && !t.AI {
		return "", errors.New("Moon 模式必须启用 AI")
	}
	if t.Mode == "inline_button" && t.AI {
		steps := t.Steps
		if steps < 1 {
			steps = 5
		}
		var e error
		for i := 0; i < steps; i++ {
			var s string
			s, e = callbackOutcome(p.executeAttempt(ctx, t))
			if e == nil {
				return s, nil
			}
			if ctx.Err() != nil {
				break
			}
			if i+1 < steps {
				if e = sleep(ctx, p.Poll); e != nil {
					break
				}
			}
		}
		return "", e
	}
	return callbackOutcome(p.executeAttempt(ctx, t))
}
func (p *Plugin) executeAttempt(ctx context.Context, t Task) (string, error) {
	ms, e := p.history(ctx, t.Bot)
	if e != nil {
		return "", e
	}
	base := newCursor(ms)
	initial := base
	if t.SendStart {
		if e = p.send(ctx, t.Bot, "/start"); e != nil {
			return "", e
		}
		if e = sleep(ctx, time.Duration(t.Wait)*time.Millisecond); e != nil {
			return "", e
		}
		ms, e = p.waitFresh(ctx, t.Bot, base, p.ResultWait, nil)
		if e != nil {
			return "", e
		}
		if s, e := terminal(ms); s != "" || e != nil {
			return s, e
		}
	}
	actionCursor := newCursor(ms)
	switch t.Mode {
	case "text", "image_choice":
		if e = p.send(ctx, t.Bot, t.Command); e != nil {
			return "", e
		}
	case "reply_button":
		cmd := t.Command
		for _, m := range ms {
			for _, b := range m.Buttons {
				if matches(b.Text, t.Command) {
					cmd = b.Text
					break
				}
			}
		}
		if e = p.send(ctx, t.Bot, cmd); e != nil {
			return "", e
		}
	case "app_cf":
		if t.Secondary != "" {
			if _, _, e = p.entry(ctx, t, ms, t.Command, false); e != nil {
				return "", e
			}
			ms, e = p.waitFresh(ctx, t.Bot, actionCursor, p.ResultWait, nil)
			if e != nil {
				return "", e
			}
			if s, e := terminal(ms); s != "" || e != nil {
				return s, e
			}
		}
		cmd := t.Command
		if t.Secondary != "" {
			cmd = t.Secondary
		}
		m, b, e := p.entry(ctx, t, ms, cmd, true)
		if e != nil {
			return "", e
		}
		_ = m
		u := b.URL
		if b.Kind != "url" && b.Kind != "KeyboardButtonUrl" {
			r, e := p.host.Call(ctx, api.Call{Method: "webview", Target: t.Bot, URL: u, Simple: b.Kind == "simple_webview" || b.Kind == "KeyboardButtonSimpleWebView"})
			if e != nil {
				return "", e
			}
			u = r.URL
		}
		if u == "" {
			return "", errors.New("Telegram 未返回应用 URL")
		}
		return p.solveCF(ctx, t, u, b.Text, t.Secondary != "", initial, 120*time.Second)
	case "inline_button", "moon", "math", "xigua_sequence":
		if t.Command != "" && t.Command != "auto" && t.Command != "none" {
			if _, _, e = p.entry(ctx, t, ms, t.Command, false); e != nil && (t.Mode != "moon" || !errors.Is(e, errButtonNotFound)) {
				return "", e
			}
		}
		if t.Secondary != "" {
			ms, e = p.waitFresh(ctx, t.Bot, actionCursor, p.ResultWait, nil)
			if e != nil {
				return "", e
			}
			if s, e := terminal(ms); s != "" || e != nil {
				return s, e
			}
			actionCursor = newCursor(ms)
			if _, _, e = p.entry(ctx, t, ms, t.Secondary, false); e != nil {
				return "", e
			}
		}
	default:
		return "", fmt.Errorf("未知模式 %s", t.Mode)
	}
	if t.Wait > 0 && t.Mode != "image_choice" {
		if e = sleep(ctx, time.Duration(t.Wait)*time.Millisecond); e != nil {
			return "", e
		}
	}
	if t.Mode == "math" {
		fresh, e := p.waitFresh(ctx, t.Bot, actionCursor, p.ResultWait, func(m api.Message) bool {
			_, ok := arithmetic(m.Text)
			return ok || successRE.MatchString(m.Text) || failureRE.MatchString(m.Text)
		})
		if e != nil {
			return "", e
		}
		if s, e := terminal(fresh); s != "" || e != nil {
			return s, e
		}
		for _, m := range fresh {
			if a, ok := arithmetic(m.Text); ok {
				c := newCursor(fresh)
				if e = p.send(ctx, t.Bot, a); e != nil {
					return "", e
				}
				return p.result(ctx, t, c)
			}
		}
		return "", errors.New("未解析到算式")
	}
	if t.Mode == "xigua_sequence" {
		fresh, e := p.waitFresh(ctx, t.Bot, actionCursor, p.ResultWait, func(m api.Message) bool {
			return len(sequence(m)) >= 2 || successRE.MatchString(m.Text) || failureRE.MatchString(m.Text)
		})
		if e != nil {
			return "", e
		}
		if s, e := terminal(fresh); s != "" || e != nil {
			return s, e
		}
		for _, m := range fresh {
			seq := sequence(m)
			if len(seq) < 2 {
				continue
			}
			var c *cursor
			for _, token := range seq {
				now, e := p.history(ctx, t.Bot)
				if e != nil {
					return "", e
				}
				for _, x := range now {
					if x.ID == m.ID {
						m = x
					}
				}
				c = newCursor(now)
				found := false
				for _, b := range m.Buttons {
					if normalizeEmoji(b.Text) == token {
						if e = p.click(ctx, t.Bot, m, b); e != nil {
							return "", e
						}
						found = true
						break
					}
				}
				if !found {
					return "", fmt.Errorf("序列按钮 %s 不存在", token)
				}
				reaction, e := p.waitFresh(ctx, t.Bot, c, max(p.Poll, time.Duration(t.Wait)*time.Millisecond), nil)
				if e != nil && ctx.Err() != nil {
					return "", e
				}
				if s, e := terminal(reaction); s != "" || e != nil {
					return s, e
				}
			}
			return p.result(ctx, t, c)
		}
		return "", errors.New("无法解析目标序列")
	}
	if t.AI || t.Mode == "image_choice" || t.Mode == "moon" {
		return p.aiSteps(ctx, t, actionCursor)
	}
	return p.result(ctx, t, actionCursor)
}
func (p *Plugin) aiSteps(ctx context.Context, t Task, c *cursor) (string, error) {
	steps := 1
	if t.Mode == "moon" {
		steps = t.Steps
		if steps < 1 {
			steps = 3
		}
	}
	start := time.Now()
	for i := 0; i < steps; i++ {
		fresh, e := p.waitFresh(ctx, t.Bot, c, p.ResultWait, func(m api.Message) bool {
			if t.Mode == "image_choice" {
				return m.HasMedia && len(m.Buttons) >= 2 || successRE.MatchString(m.Text) || failureRE.MatchString(m.Text)
			}
			return true
		})
		if e != nil {
			return "", e
		}
		if s, e := terminal(fresh); s != "" || e != nil {
			return s, e
		}
		for _, m := range fresh {
			if u := cfURL(m); u != "" {
				return p.solveCF(ctx, t, u, "", false, c, 60*time.Second)
			}
		}
		var target, keyboard api.Message
		for _, m := range fresh {
			if target.ID == 0 {
				target = m
			}
			if m.HasMedia {
				target = m
			}
			if keyboard.ID == 0 && len(m.Buttons) > 0 {
				keyboard = m
			}
		}
		rt := t
		if t.Mode == "image_choice" {
			rt.AITarget = "image"
			if rt.Prompt == "" {
				rt.Prompt = "识别照片主体物品。忽略字幕水印，只输出一个与可选按钮完全一致的文字。"
			}
		}
		recognitionCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
		if t.Mode == "image_choice" {
			cancel()
			recognitionCtx, cancel = context.WithTimeout(ctx, 15*time.Second)
		}
		answer, e := p.recognize(recognitionCtx, rt, target, keyboard.Buttons)
		cancel()
		if e != nil {
			return "", e
		}
		if t.Mode == "image_choice" && time.Since(start) > 26*time.Second {
			return "", errors.New("图片题接近30秒时限，未点击")
		}
		now, e := p.history(ctx, t.Bot)
		if e != nil {
			return "", e
		}
		for _, m := range now {
			if c.fresh(m) && len(m.Buttons) > 0 {
				keyboard = m
				break
			}
		}
		c = newCursor(now)
		if len(keyboard.Buttons) > 0 {
			b, e := matchAnswer(answer, keyboard.Buttons, t.Mode == "image_choice")
			if e != nil {
				return "", e
			}
			if e = p.click(ctx, t.Bot, keyboard, b); e != nil {
				return "", e
			}
		} else {
			if e = p.send(ctx, t.Bot, strings.Join(strings.Fields(answer), "")); e != nil {
				return "", e
			}
		}
	}
	return p.result(ctx, t, c)
}
