package extensions

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// Keep build callbacks independent of Telegram latency. One worker owns edits,
// and Stop joins it before the final result can replace the progress message.
type tpmProgress struct {
	mu          sync.Mutex
	item, stage string
	started     time.Time
	wake        chan struct{}
	cancel      context.CancelFunc
	done        chan struct{}
}

func newTPMProgress(ctx context.Context, edit func(context.Context, string) error) *tpmProgress {
	ctx, cancel := context.WithCancel(ctx)
	p := &tpmProgress{started: time.Now(), wake: make(chan struct{}, 1), cancel: cancel, done: make(chan struct{})}
	go func() {
		defer close(p.done)
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		var last time.Time
		for {
			select {
			case <-ctx.Done():
				return
			case <-p.wake:
				if time.Since(last) < 3*time.Second {
					continue
				}
			case <-ticker.C:
			}
			p.mu.Lock()
			item, stage := p.item, p.stage
			p.mu.Unlock()
			if item == "" && stage == "" {
				continue
			}
			text := fmt.Sprintf("📦 TPM · 已用 %s\n%s\n%s", time.Since(p.started).Truncate(time.Second), item, stage)
			editCtx, cancelEdit := context.WithTimeout(ctx, 5*time.Second)
			// A transient progress-edit failure must not discard a valid build.
			_ = edit(editCtx, text)
			cancelEdit()
			last = time.Now()
		}
	}()
	return p
}

func (p *tpmProgress) set(item, stage string) {
	p.mu.Lock()
	if item != "" {
		p.item = item
	}
	p.stage = stage
	p.mu.Unlock()
	select {
	case p.wake <- struct{}{}:
	default:
	}
}

func (p *tpmProgress) Item(text string) error { p.set(text, ""); return nil }
func (p *tpmProgress) Stage(text string)      { p.set("", text) }
func (p *tpmProgress) Stop()                  { p.cancel(); <-p.done }
