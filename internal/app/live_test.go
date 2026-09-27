package app

import (
	"context"
	"testing"
	"time"

	"github.com/gotd/td/tg"
)

// 同一条消息会从 liveHandler 和更新引擎各来一次，只能处理一次；两代轮换之后才忘掉。
func TestSeenMessagesKeepsTwoWindows(t *testing.T) {
	var seen seenMessages
	start := time.Unix(1_790_000_000, 0)
	if !seen.first("-100/1", start) {
		t.Fatal("a new message was taken for a repeat")
	}
	if seen.first("-100/1", start.Add(time.Second)) {
		t.Error("the second copy was not caught")
	}
	if !seen.first("-100/2", start.Add(time.Second)) || !seen.first("-200/1", start.Add(time.Second)) {
		t.Error("another message or another chat was taken for a repeat")
	}
	// 轮换一次后还在上一代里，照样挡住。
	if seen.first("-100/1", start.Add(seenWindow+time.Second)) {
		t.Error("a message was forgotten after one rotation")
	}
	// 再轮换一次才忘掉。
	if !seen.first("-100/2", start.Add(2*seenWindow+2*time.Second)) {
		t.Error("a message was remembered past two windows")
	}
}

// 重启前发的消息不再执行；启动那一秒发的算启动之后（消息时间只到秒）。
func TestSentBeforeStart(t *testing.T) {
	started := time.Unix(1_790_000_000, 700_000_000)
	for date, want := range map[int]bool{1_789_999_990: true, 1_789_999_999: true, 1_790_000_000: false, 1_790_000_005: false} {
		if got := sentBeforeStart(date, started); got != want {
			t.Errorf("sentBeforeStart(%d) = %v, want %v", date, got, want)
		}
	}
}

// blockedEngine 冒充卡住的更新引擎：Handle 一直等到测试放行。
type blockedEngine struct{ release chan struct{} }

func (b blockedEngine) Handle(context.Context, tg.UpdatesClass) error {
	<-b.release
	return nil
}

// 更新引擎卡住时，新消息照样派发；消息也照样交给引擎。
func TestLiveHandlerDispatchesAheadOfTheEngine(t *testing.T) {
	got := make(chan int, 1)
	live := tg.NewUpdateDispatcher()
	live.OnNewChannelMessage(func(_ context.Context, _ tg.Entities, update *tg.UpdateNewChannelMessage) error {
		got <- update.Message.(*tg.Message).ID
		return nil
	})
	engine := blockedEngine{release: make(chan struct{})}
	handler := liveHandler{live: live, next: engine}
	done := make(chan error, 1)
	go func() {
		done <- handler.Handle(context.Background(), &tg.Updates{Updates: []tg.UpdateClass{
			&tg.UpdateNewChannelMessage{Message: &tg.Message{ID: 484083, PeerID: &tg.PeerChannel{ChannelID: 3469942831}}, Pts: 10, PtsCount: 1},
		}})
	}()
	select {
	case id := <-got:
		if id != 484083 {
			t.Errorf("dispatched message %d", id)
		}
	case <-time.After(time.Second):
		t.Fatal("the message waited for the stuck engine")
	}
	select {
	case <-done:
		t.Fatal("the update never reached the engine")
	default:
	}
	close(engine.release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
