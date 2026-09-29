package qdsg

import (
	"context"
	"errors"
	"fmt"
	api "github.com/OrionG-hub/laowangbot/pkg/pluginapi"
	"strings"
	"sync"
	"time"
)

type progressLine struct{ ID, Bot, Status string }
type runProgress struct {
	mu      sync.Mutex
	Chat    string
	Message int
	Lines   []progressLine
}

func clipProgress(s string, limit int) (string, bool) {
	n := 0
	for i, r := range s {
		width := 1
		if r > 0xffff {
			width = 2
		}
		if n+width > limit-1 {
			return s[:i] + "…", true
		}
		n += width
	}
	return s, false
}
func (r *runProgress) text() string {
	lines := make([]string, 0, len(r.Lines))
	truncated := false
	limit := 3900/max(1, len(r.Lines)) - 1
	for _, line := range r.Lines {
		s := fmt.Sprintf("qdsg %s @%s: %s", line.ID, line.Bot, strings.Join(strings.Fields(line.Status), " "))
		v, cut := clipProgress(s, limit)
		truncated = truncated || cut
		lines = append(lines, v)
	}
	if truncated {
		lines = append(lines, "详情请查看 qdsg list")
	}
	text, cut := clipProgress(strings.Join(lines, "\n"), 4000)
	if cut {
		text += "\n详情请查看 qdsg list"
	}
	return text
}
func (r *runProgress) write(ctx context.Context, p *Plugin) error {
	result, err := p.host.Call(ctx, api.Call{Method: "edit", Target: r.Chat, MessageID: r.Message, Text: r.text()})
	if err == nil && result.Error != "" {
		err = errors.New(result.Error)
	}
	return err
}
func (r *runProgress) recordError(p *Plugin, id string, err error) {
	if err == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	for i := range p.db.Tasks {
		t := &p.db.Tasks[i]
		if t.ID == id {
			detail := "原消息状态更新失败: " + err.Error()
			if !strings.Contains(t.LastError, detail) {
				if t.LastError != "" {
					t.LastError += "；"
				}
				t.LastError += detail
			}
		}
	}
	if saveErr := p.save(); saveErr != nil {
		for i := range p.db.Tasks {
			if p.db.Tasks[i].ID == id {
				p.db.Tasks[i].LastError += "；保存状态失败: " + saveErr.Error()
			}
		}
	}
}
func (r *runProgress) update(p *Plugin, id, status string) error {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	for i := range r.Lines {
		if r.Lines[i].ID == id {
			r.Lines[i].Status = status
		}
	}
	ctx, cancel := context.WithTimeout(p.ctx, 20*time.Second)
	defer cancel()
	err := r.write(ctx, p)
	if err != nil && ctx.Err() == nil {
		if sleep(ctx, 100*time.Millisecond) == nil {
			err = r.write(ctx, p)
		}
	}
	r.mu.Unlock()
	if err != nil {
		r.recordError(p, id, err)
	}
	return err
}

func (p *Plugin) manualNow(ctx context.Context, tasks []Task, ids map[string]bool, ev api.Event) (string, error) {
	progress := &runProgress{Chat: ev.ChatID, Message: ev.MessageID}
	for _, t := range tasks {
		if ids[t.ID] {
			progress.Lines = append(progress.Lines, progressLine{t.ID, t.Bot, "已排队"})
		}
	}
	// Write before reserving any jobs; a failed initial edit cannot start a task.
	if err := progress.write(ctx, p); err != nil {
		return "", err
	}
	progress.mu.Lock()
	rejected := false
	accepted := 0
	for i := range progress.Lines {
		line := &progress.Lines[i]
		if err := p.enqueueProgress(line.ID, false, progress); err != nil {
			line.Status = "失败: " + err.Error()
			rejected = true
		} else {
			accepted++
		}
	}
	var editErr error
	if rejected {
		editErr = progress.write(ctx, p)
	}
	progress.mu.Unlock()
	if editErr != nil {
		for _, task := range tasks {
			if ids[task.ID] {
				progress.recordError(p, task.ID, editErr)
			}
		}
		if accepted == 0 {
			return "", editErr
		}
	}
	p.Tick(time.Now())
	// The plugin owns subsequent edits, so the host must not overwrite progress.
	return "", nil
}
