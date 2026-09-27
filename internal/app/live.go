package app

import (
	"context"
	"strconv"
	"sync"
	"time"

	"github.com/gotd/td/telegram"
	"github.com/gotd/td/tg"
)

// liveHandler 让新消息一到就派发，不等 gotd 的更新引擎排好顺序。
//
// 更新引擎按 pts 严格排序：发现缺口就先把后面的更新扣住，去补抓缺的那段。补抓失败或
// 落后太多时，扣住的消息可能过一阵才放出来，也可能根本不放——落后超过 100 条它会直接
// 跳到最新（channelDifferenceTooLong），补抓冷却期（常见 30 秒）里收到的更新也会被略过。
// 2026-09-28 凌晨一个活跃群组就这样连着几分钟每 30 秒报一次落后，在里面打的命令全没反应，
// 只能重启。MiBox 用的 teleproto 不做这种排序，收到什么就处理什么，所以没有这个问题。
//
// 这里照 MiBox 的做法，收到就派发，同时照旧交给更新引擎：它补抓回来的消息仍然会派发
// （真正漏收的那些靠它补上），重复的由 App.handle 里的 seenMessages 挡掉。
// gotd 给每个收到的数据包开一个协程，所以这里不会被卡住的更新引擎拖住。
type liveHandler struct {
	live tg.UpdateDispatcher
	next telegram.UpdateHandler
}

func (h liveHandler) Handle(ctx context.Context, updates tg.UpdatesClass) error {
	_ = h.live.Handle(ctx, updates)
	return h.next.Handle(ctx, updates)
}

// seenWindow 是去重记录的一代保留多久，两代合起来记住最近 15 到 30 分钟的消息。
// 更新引擎补抓回来的消息晚到几分钟很常见，晚到半小时的已经不当新命令执行了。
const seenWindow = 15 * time.Minute

// seenMessages 记住最近处理过的消息，同一条只处理一次。
//
// 用两代 map 轮换，而不是给每条记时间再逐条清理：活跃的账号每分钟经过几十上百条消息，
// 轮换是 O(1) 的。
type seenMessages struct {
	mu                sync.Mutex
	current, previous map[string]struct{}
	rotated           time.Time
}

// first 记下这条消息，是第一次见到时返回 true。
func (s *seenMessages) first(key string, now time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.current == nil || now.Sub(s.rotated) >= seenWindow {
		s.previous, s.current, s.rotated = s.current, map[string]struct{}{}, now
	}
	if _, ok := s.current[key]; ok {
		return false
	}
	if _, ok := s.previous[key]; ok {
		return false
	}
	s.current[key] = struct{}{}
	return true
}

func messageKey(chat string, id int) string { return chat + "/" + strconv.Itoa(id) }

// sentBeforeStart 判断消息是不是在本进程启动之前发的。
//
// 重启后更新引擎会把停机期间和重启前没处理完的消息补抓回来。这些不再执行：重启常常
// 就是为了掐断它们，补跑一遍等于白重启，重启前发的 .restart 还会再触发一次重启。
// Telegram 的消息时间只到秒，所以按秒比较；和启动同一秒发的算启动之后。
func sentBeforeStart(date int, started time.Time) bool {
	return int64(date) < started.Unix()
}
