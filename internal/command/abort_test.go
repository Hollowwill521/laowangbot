package command

import (
	"context"
	"testing"
	"time"

	"github.com/MiCat-S/mibot-lite/internal/bot"
)

// 重启要能掐断卡住的命令：Abort 之后正在跑的命令拿到取消，不回「执行失败」
// （连接正在关，发不出去，而且测试里 Client 是 nil，一编辑就会 panic），
// 之后收到的命令也不再执行。
func TestAbortCancelsRunningCommands(t *testing.T) {
	r := registry()
	started, finished := make(chan struct{}), make(chan error, 1)
	r.Register(&Command{Name: "hang", Timeout: -1, Handle: func(ctx context.Context, inv *Invocation) error {
		close(started)
		<-ctx.Done()
		finished <- ctx.Err()
		return ctx.Err()
	}})
	calls := 0
	r.Register(&Command{Name: "later", Handle: func(context.Context, *Invocation) error { calls++; return nil }})

	if !r.Dispatch(context.WithoutCancel(context.Background()), nil, &bot.Message{ID: 1, ChatID: "1", Text: ".hang"}) {
		t.Fatal(".hang was not dispatched")
	}
	<-started
	r.Abort()
	select {
	case err := <-finished:
		if err == nil {
			t.Error("the command finished without seeing a cancellation")
		}
	case <-time.After(time.Second):
		t.Fatal("Abort did not reach the running command")
	}
	if !r.Wait(time.Second) {
		t.Error("an aborted command was still counted as running")
	}

	r.Dispatch(context.Background(), nil, &bot.Message{ID: 2, ChatID: "1", Text: ".later"})
	r.Wait(time.Second)
	if calls != 0 {
		t.Error("a command received after Abort still ran")
	}
}
