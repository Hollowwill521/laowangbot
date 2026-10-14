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

// failureTrip 和 failureBackoff 处理没见过的拒绝：群里单独关掉反应开关时服务器到底
// 回什么错误码没实测过，可能是 REACTION_INVALID（和表情写错同一个码），也可能是一个
// 我们没列进白名单的新码。既不能当成群级错误直接冷却 12 小时——猜错了会白白丢掉一个
// 真能点赞的群一整天；也不能不管——那样每次命中都撞一次服务器、刷一行日志。
// 所以按群累计连续拒绝，够三次就歇一小时：够安静，又每小时给它一次自己好的机会，
// 而 react.chat_backoff 那行日志会把真正的错误码留在现场，攒到证据再进白名单。
const (
	failureTrip    = 3
	failureBackoff = time.Hour
)

// muteTracker 记下暂时别碰的群。
//
// 账号不在频道里、群里不让发言这类错误不会自己好；不冷却的话，
// 每次命中都要空跑一次 RPC 并刷一行日志。
type muteTracker struct {
	mu       sync.Mutex
	until    map[string]time.Time
	failures map[string]int
}

func newMuteTracker() *muteTracker {
	return &muteTracker{until: map[string]time.Time{}, failures: map[string]int{}}
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
	// 群级结论已经下了，之前攒的连续拒绝计数就没意义了。
	delete(m.failures, chatID)
}

// noteFailure 累计这个群的连续拒绝，够 failureTrip 次就当场冷却 failureBackoff
// 并返回 true。冷却由调用方记日志：是不是要歇一小时取决于错误码属于哪一类，
// 而"属于哪一类"这件事得由 react.go 那边的分类说了算。
func (m *muteTracker) noteFailure(chatID string, now time.Time) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failures == nil {
		m.failures = map[string]int{}
	}
	m.failures[chatID]++
	if m.failures[chatID] < failureTrip {
		return false
	}
	delete(m.failures, chatID)
	if m.until == nil {
		m.until = map[string]time.Time{}
	}
	m.until[chatID] = now.Add(failureBackoff)
	return true
}

// resetFailures 在点赞成功时清掉这个群的连续拒绝计数：一次成功就证明这里能点赞，
// 之前那些失败大概是消息本身的问题（被删、反应位满），不该继续往冷却上累。
func (m *muteTracker) resetFailures(chatID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.failures, chatID)
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
