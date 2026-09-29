package pmcaptcha

import (
	"context"
	"errors"
	"fmt"
	api "github.com/OrionG-hub/laowangbot/pkg/pluginapi"
	"slices"
	"strconv"
	"strings"
	"time"
)

func (p *Plugin) event(ctx context.Context, ev api.Event) error {
	if !p.config.Enabled || ev.Edited {
		return nil
	}
	if ev.ChannelDM && ev.Out && contains(p.config.Pass, "add_folder") {
		if e := p.normalize(ctx); e != nil {
			return e
		}
		_, e := p.call(ctx, api.Call{Method: "chat_action", Target: ev.ChatID, Action: "folder_add", FolderID: p.config.Folder})
		return e
	}
	id, e := strconv.ParseInt(ev.ChatID, 10, 64)
	if e != nil || id <= 0 || id == 777000 || ev.ChatID == ev.SelfID {
		return nil
	}
	if ev.Out {
		if !p.config.Initiative || p.states[id] != nil || p.outgoing[fmt.Sprintf("%d:%d", id, ev.MessageID)].After(time.Now()) || (has(p.data.Failed, id) && !p.config.InitiativeFailed) {
			return nil
		}
	}
	r, e := p.info(ctx, id)
	if e != nil {
		return e
	}
	if r.Entity.Bot || r.Entity.Self {
		return nil
	}
	if ev.Out {
		verified := has(p.data.Verified, id)
		white := slices.Contains(p.data.Whitelist, id)
		if (!verified && !white) || (verified && !white && contains(p.config.Pass, "wl")) || has(p.data.Failed, id) {
			return p.pass(ctx, id, r.Entity, false)
		}
		return nil
	}
	if !ev.SenderIsUser || slices.Contains(p.data.Whitelist, id) {
		return nil
	}
	if p.states[id] != nil {
		return errors.Join(p.restrict(ctx, id), p.reply(ctx, id, ev.Text, ev.MessageID))
	}
	if has(p.data.Verified, id) {
		return nil
	}
	if e = p.normalize(ctx); e != nil {
		return e
	}
	rule, e := p.rules(ctx, id, ev, r)
	if e != nil {
		return e
	}
	if rule == "pass" {
		return p.pass(ctx, id, r.Entity, true)
	}
	if rule == "block" {
		return p.fail(ctx, id, "max_tries")
	}
	restrictErr := p.restrict(ctx, id)
	if p.config.Captcha {
		return errors.Join(restrictErr, p.challenge(ctx, id))
	}
	return restrictErr
}
func (p *Plugin) rules(ctx context.Context, id int64, ev api.Event, user api.Result) (string, error) {
	if p.config.History > 0 {
		count, offset := 0, 0
		for count < p.config.History {
			r, e := p.call(ctx, api.Call{Method: "history", Target: idText(id), Limit: min(99, p.config.History-count) + 1, OffsetID: offset})
			if e != nil {
				return "", e
			}
			if len(r.Messages) == 0 {
				break
			}
			next := offset
			for _, m := range r.Messages {
				if m.ID != ev.MessageID {
					count++
				}
				if next == 0 || m.ID < next {
					next = m.ID
				}
			}
			if next == offset {
				break
			}
			offset = next
		}
		if count >= p.config.History {
			return "pass", nil
		}
	}
	if p.config.Groups >= 0 && user.CommonChats >= p.config.Groups {
		return "pass", nil
	}
	for _, w := range p.config.WLWords {
		if strings.Contains(ev.Text, w) {
			return "pass", nil
		}
	}
	for _, w := range p.config.BLWords {
		if strings.Contains(ev.Text, w) {
			return "block", nil
		}
	}
	switch p.config.Premium {
	case "allow":
		if user.Entity.Premium {
			return "pass", nil
		}
	case "ban":
		if user.Entity.Premium {
			return "block", nil
		}
	case "only":
		if user.Entity.Premium {
			return "pass", nil
		}
		return "block", nil
	}
	return "", nil
}
func (p *Plugin) restrict(ctx context.Context, id int64) error {
	return errors.Join(p.action(ctx, id, "archive"), p.action(ctx, id, "mute"))
}
func (p *Plugin) pass(ctx context.Context, id int64, user *api.Entity, autoWhite bool) error {
	d := cloneData(p.data)
	d.Failed = remove(d.Failed, id)
	r := Record{ID: id, Name: user.Name, Username: user.Username, Time: time.Now().UTC().Format(time.RFC3339)}
	d.Verified = upsert(d.Verified, r)
	if autoWhite || contains(p.config.Pass, "wl") {
		if !slices.Contains(d.Whitelist, id) {
			d.Whitelist = append(d.Whitelist, id)
		}
		d.Verified = remove(d.Verified, id)
	}
	if e := p.saveData(d); e != nil {
		return e
	}
	s := p.states[id]
	delete(p.states, id)
	errs := []error{p.cleanup(ctx, id, s)}
	for _, a := range p.config.Pass {
		switch a {
		case "unmute", "unarchive":
			errs = append(errs, p.action(ctx, id, a))
		case "add_folder":
			if e := p.normalize(ctx); e != nil {
				errs = append(errs, e)
			} else if p.config.Folder > 0 {
				errs = append(errs, p.action(ctx, id, "folder_add"))
			}
		}
	}
	if s != nil {
		_, e := p.send(ctx, id, "✅ 验证通过！欢迎与我对话。", nil)
		errs = append(errs, e)
	}
	return errors.Join(errs...)
}
func (p *Plugin) fail(ctx context.Context, id int64, reason string) error {
	d := cloneData(p.data)
	r := Record{ID: id, Name: idText(id), Time: time.Now().UTC().Format(time.RFC3339), Reason: reason}
	if u, e := p.info(ctx, id); e == nil {
		r.Name = u.Entity.Name
		r.Username = u.Entity.Username
	}
	d.Failed = upsert(d.Failed, r)
	d.Verified = remove(d.Verified, id)
	// Commit before clearing the pending state or claiming a recorded failure.
	if e := p.saveData(d); e != nil {
		return e
	}
	s := p.states[id]
	delete(p.states, id)
	errs := []error{p.cleanup(ctx, id, s)}
	if s != nil {
		message := "❌ 验证失败次数过多。"
		if reason == "timeout" {
			message = "⏰ 验证超时。"
		}
		message += "即将按设置处理对话。"
		_, e := p.send(ctx, id, message, nil)
		errs = append(errs, e)
	}
	errs = append(errs, p.restrict(ctx, id))
	if p.config.Folder > 1 {
		errs = append(errs, p.normalize(ctx), p.action(ctx, id, "folder_remove"))
	}
	for _, a := range p.config.Fail {
		switch a {
		case "block", "report":
			errs = append(errs, p.action(ctx, id, a))
		case "delete":
			errs = append(errs, p.action(ctx, id, "delete_history"))
		case "kick", "ban":
			errs = append(errs, fmt.Errorf("私聊不支持 %s", a))
		}
	}

	return errors.Join(errs...)
}
func (p *Plugin) reply(ctx context.Context, id int64, text string, msg int) error {
	s := p.states[id]
	if s == nil || strings.TrimSpace(text) == "" {
		return nil
	}
	if p.config.Timeout > 0 && !time.Now().Before(s.Started.Add(time.Duration(p.config.Timeout)*time.Second)) {
		return p.fail(ctx, id, "timeout")
	}
	s.Messages = append(s.Messages, msg)
	a, b := strings.ToUpper(strings.TrimSpace(text)), strings.ToUpper(strings.TrimSpace(s.Answer))
	correct := a == b
	if strings.HasPrefix(s.Mode, "img_") {
		correct = distance(a, b) <= 1
	}
	if correct {
		r, e := p.info(ctx, id)
		if e != nil {
			return e
		}
		return p.pass(ctx, id, r.Entity, false)
	}
	s.Tries++
	if p.config.Tries > 0 && s.Tries >= p.config.Tries {
		return p.fail(ctx, id, "max_tries")
	}
	hint := "❌ 答案错误，请重试。"
	if p.config.Tries > 0 {
		hint = fmt.Sprintf("❌ 答案错误，剩余 %d 次。", p.config.Tries-s.Tries)
	}
	mid, e := p.send(ctx, id, hint, nil)
	if e == nil {
		s.Messages = append(s.Messages, mid)
	}
	return e
}
func distance(a, b string) int {
	ar, br := []rune(a), []rune(b)
	if len(ar) > len(br)+1 || len(br) > len(ar)+1 {
		return 2
	}
	prev := make([]int, len(br)+1)
	for j := range prev {
		prev[j] = j
	}
	for i, x := range ar {
		cur := make([]int, len(br)+1)
		cur[0] = i + 1
		for j, y := range br {
			cost := 1
			if x == y {
				cost = 0
			}
			cur[j+1] = min(cur[j]+1, prev[j+1]+1, prev[j]+cost)
		}
		prev = cur
	}
	return prev[len(br)]
}
