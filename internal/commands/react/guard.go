package react

import (
	"sync"
	"time"
)

// seenWindow 是"这条消息已经点过了"记多久。
//
// 只需要盖住更新重放和 event 重复投递的窗口：本进程启动之前发的消息宿主根本不会
// 交给监听者，所以不需要像 monitor 的去重那样落盘。
const seenWindow = 10 * time.Minute

// seenTracker 用两代 map 轮换，跟 internal/app/live.go 一个做法：
// 活跃的账号每分钟几十上百条消息，逐条记时间再清理太贵。
type seenTracker struct {
	mu                sync.Mutex
	current, previous map[string]bool
	rotated           time.Time
}

// first 记下这条消息，第一次见到时返回 true。
func (s *seenTracker) first(key string, now time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.current == nil || now.Sub(s.rotated) >= seenWindow {
		s.previous, s.current, s.rotated = s.current, map[string]bool{}, now
	}
	if s.current[key] || s.previous[key] {
		return false
	}
	s.current[key] = true
	return true
}

// chatCooldown 是一个"这个群点不了赞"的结论能用多久。
const chatCooldown = 12 * time.Hour

// muteTracker 记下暂时别碰的群。
//
// 账号不在频道里、群里不让发言这类错误不会自己好；不冷却的话，
// 每次命中都要空跑一次 RPC 并刷一行日志。
type muteTracker struct {
	mu    sync.Mutex
	until map[string]time.Time
}

func newMuteTracker() *muteTracker {
	return &muteTracker{until: map[string]time.Time{}}
}

func (m *muteTracker) blocked(chatID string, now time.Time) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	until, ok := m.until[chatID]
	if !ok {
		return false
	}
	if until.After(now) {
		return true
	}
	delete(m.until, chatID)
	return false
}

func (m *muteTracker) block(chatID string, now time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.until == nil {
		m.until = map[string]time.Time{}
	}
	m.until[chatID] = now.Add(chatCooldown)
}

// blocked 的快照，给状态面板显示"哪些群暂时跳过了"。
func (m *muteTracker) list(now time.Time) []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	var chats []string
	for chatID, until := range m.until {
		if !until.After(now) {
			delete(m.until, chatID)
			continue
		}
		chats = append(chats, chatID)
	}
	return chats
}
