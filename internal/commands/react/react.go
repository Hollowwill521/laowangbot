package react

import (
	"context"
	"log/slog"
	"math/rand/v2"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"

	"github.com/OrionG-hub/laowangbot/internal/app"
	"github.com/OrionG-hub/laowangbot/internal/bot"
	"github.com/OrionG-hub/laowangbot/internal/command"
	"github.com/OrionG-hub/laowangbot/internal/commands/kit"
	"github.com/OrionG-hub/laowangbot/internal/store"
)

// queueSize 是等待点赞的条数上限。命中得比发得快的时候，多出来的直接丢掉：
// 点赞是"顺手记一笔"的动作，为一堆旧消息排队没有意义，还会把限流全撞完。
const queueSize = 128

// settingsTTL 是配置快照的有效期。见 settings。
const settingsTTL = 2 * time.Second

// 两条点赞之间的间隔下限和抖动上限。连发最容易撞 FLOOD_WAIT，
// 而整秒节奏比随机间隔更好被服务器判成机器行为。
const (
	minHold      = 700 * time.Millisecond
	maxJitterRMS = 1300 * time.Millisecond
)

// chatLevelErrors 是不会自己好的错误：账号不在频道里、群里不让发言、
// 这个 peer 根本点不了赞。撞上一次就冷却这个群，别再空跑 RPC。
var chatLevelErrors = []string{
	"PEER_ID_INVALID", "CHANNEL_INVALID", "CHANNEL_PRIVATE", "CHAT_WRITE_FORBIDDEN", "USER_BANNED_IN_CHANNEL",
}

// ignorableErrors 是单条消息层面的失败：消息被删了、反应位已经满了。
// 重试只会更糟，安静丢掉就行。
var ignorableErrors = []string{
	"MSG_ID_INVALID", "MESSAGE_ID_INVALID", "REACTIONS_TOO_MANY", "MESSAGE_NOT_MODIFIED",
}

// emojiErrors 一看就知道是配置写错了，报出来时要带上那个表情本身。
var emojiErrors = []string{
	"REACTION_INVALID", "CUSTOM_REACTIONS_TOO_MANY", "DOCUMENT_INVALID", "PREMIUM_ACCOUNT_REQUIRED",
}

// task 是一次待执行的点赞。
type task struct {
	chatID  string
	msgID   int
	emoji   string
	big     bool
	keyword string
}

// service 把订阅、配置、发送三件事串起来。
type service struct {
	store   *store.Store[Config]
	match   *matcher
	mutes   *muteTracker
	seen    seenTracker
	queue   chan task
	reacted atomic.Int64
	dropped atomic.Int64

	// mu 只护住配置快照，不护 store 本身（store 自己有锁）。
	mu       sync.Mutex
	cached   *Config
	cachedAt time.Time
}

// Register 接入 .react，并登记消息监听和后台发送任务。
func Register(a *app.App) {
	s := &service{
		store: kit.NewStore(a, "react.json", defaults),
		match: newMatcher(),
		mutes: newMuteTracker(),
		queue: make(chan task, queueSize),
	}
	// 手改过的、或者更早版本写出的文件在这里一次补齐缺省值，
	// 之后每条消息的读取路径都不用再防空表情和重复条目。
	_ = s.store.Update(func(cfg *Config) error {
		cfg.normalize()
		return nil
	})

	a.Registry.Register(&command.Command{
		Name:        "react",
		Description: "命中关键词就给那条消息点赞",
		Usage:       "on|off|all|big|emoji|keyword|group|status",
		Help:        helpText,
		Handle:      s.handle,
	})

	// 用 OnIncoming，不用 OnForeign：OnForeign 按登记顺序逐个问，谁先说"处理了"
	// 消息就归谁，会跟 .sudo 抢；OnIncoming 的返回值宿主根本不看。
	a.OnIncoming(func(_ context.Context, client *bot.Client, message *bot.Message) bool {
		s.observe(client, message, time.Now())
		return false
	})

	a.Registry.AddJob(s.run)
}

// observe 挂在宿主的更新流上，所以只做便宜的判断：命中就丢进队列，
// RPC 留给 run 去发。队列满了就丢弃并计数，绝不把更新流堵住。
func (s *service) observe(client *bot.Client, message *bot.Message, now time.Time) {
	cfg := s.settings(client)
	task, ok := s.decide(cfg, message, now)
	if !ok {
		return
	}
	select {
	case s.queue <- task:
	default:
		if count := s.dropped.Add(1); count == 1 || count%20 == 0 {
			client.Logger().Warn("react.queue_full", slog.String("chat", task.chatID), slog.Int64("dropped", count))
		}
	}
}

// decide 判断这条消息要不要点赞，并把要做的动作描述出来。
// 除了两个冷却表，这里不碰网络和磁盘，方便单测把范围条件一条条压一遍。
func (s *service) decide(cfg Config, message *bot.Message, now time.Time) (task, bool) {
	if !cfg.Enabled || message.Out || message.Edited || len(cfg.Keywords) == 0 {
		return task{}, false
	}
	// Telegram 只在超级群和频道里支持点赞：普通小群连反应列表都没有，
	// 私聊虽然能点赞，但不是这个功能要的东西。
	if !supportsReactions(message.ChatType) {
		return task{}, false
	}
	if !cfg.inScope(message.ChatID) || s.mutes.blocked(message.ChatID, now) {
		return task{}, false
	}
	keyword, ok := s.match.match(message.Text, cfg.Keywords)
	if !ok {
		return task{}, false
	}
	if !s.seen.first(message.ChatID+"/"+strconv.Itoa(message.ID), now) {
		return task{}, false
	}
	// 表情跟着词走；词上没写表情才用默认那个。
	emoji := keyword.Emoji
	if emoji == "" {
		emoji = cfg.Emoji
	}
	return task{
		chatID:  message.ChatID,
		msgID:   message.ID,
		emoji:   emoji,
		big:     cfg.Big,
		keyword: keyword.Text,
	}, true
}

// settings 返回配置快照，最多每两秒回一次源。
//
// store.Read 每次都要把整份配置 JSON 往返一遍，而这里是每条经过的消息都要走的路；
// 活跃账号一分钟几十上百条消息时，这一步就够把更新流拖住了。
func (s *service) settings(client *bot.Client) Config {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cached != nil && time.Since(s.cachedAt) < settingsTTL {
		return *s.cached
	}
	cfg, err := s.store.Read()
	// 读失败也照样把时间记下去：否则配置一坏，每条消息都要再去敲一次磁盘。
	s.cachedAt = time.Now()
	if err != nil {
		if client != nil {
			client.Logger().Warn("react.config_read_failed", slog.String("error", err.Error()))
		}
		if s.cached != nil {
			return *s.cached
		}
		return defaults()
	}
	cfg.normalize()
	s.cached = &cfg
	return cfg
}

// forget 让快照作废：.react 刚改完配置就该立刻生效，不能等满两秒。
func (s *service) forget() {
	s.mu.Lock()
	s.cached = nil
	s.mu.Unlock()
}

// run 是唯一的发送路径：一条一条发，条与条之间随机歇一下。
// 宿主在账号连上之后才跑它，退出时取消上下文，这里就跟着收工。
func (s *service) run(ctx context.Context, client *bot.Client) {
	for {
		select {
		case <-ctx.Done():
			return
		case current := <-s.queue:
			s.apply(ctx, client, current)
			hold := minHold + time.Duration(rand.IntN(int(maxJitterRMS/time.Millisecond)))*time.Millisecond
			if err := kit.Sleep(ctx, hold); err != nil {
				return
			}
		}
	}
}

// apply 发一次点赞。
func (s *service) apply(ctx context.Context, client *bot.Client, current task) {
	if ctx.Err() != nil {
		return
	}
	peer, err := client.InputPeerFromChatID(current.chatID)
	if err != nil {
		// 刚见过的消息按理都解析得出来；解析不出来多半是对话被清理了，
		// 下一次命中还会再试，所以这里不冷却整个群。
		client.Logger().Debug("react.peer_unresolved", slog.String("chat", current.chatID), slog.String("error", err.Error()))
		return
	}
	request := &tg.MessagesSendReactionRequest{Peer: peer, MsgID: current.msgID, Big: current.big}
	request.SetReaction([]tg.ReactionClass{&tg.ReactionEmoji{Emoticon: current.emoji}})
	if _, err := client.API().MessagesSendReaction(ctx, request); err != nil {
		s.failed(client, current, err, time.Now())
		return
	}
	s.reacted.Add(1)
	s.mutes.resetFailures(current.chatID)
	client.Logger().Info("react.done", slog.String("chat", current.chatID), slog.Int("message", current.msgID),
		slog.String("emoji", current.emoji), slog.String("keyword", current.keyword), slog.Bool("big", current.big))
}

// failureKind 是服务器错误的归类：它决定还要不要理这个群，也决定日志该写成什么。
type failureKind int

const (
	// failureOther 是没见过的 RPC 错误：照实报出来，别自作聪明丢掉。
	failureOther failureKind = iota
	// failureTransport 不是 RPC 错误，是连接层面的问题：下一个命中还会再试。
	failureTransport
	// failureIgnored 是单条消息层面的失败：被删了、反应位已经满了。重试只会更糟。
	failureIgnored
	// failureChat 是这个对话本身点不了：账号不在里面、不让发言、peer 认不出来。
	// 这类不会自己好，所以冷却整个群。
	failureChat
	// failureEmoji 是表情本身的问题：多半是配置写错了，得让用户看见是哪个表情。
	failureEmoji
	// failureFlood 是被限流。60 秒以内的 FLOOD_WAIT 连接层已经自己等完并重试过了，
	// 还能走到这里说明这个等待等不起。
	failureFlood
)

// classify 给错误归类。分成纯函数是因为这几类的判断错了会静默掉一整个群的点赞，
// 值得有测试压一遍，而不是等到在真群里发现没反应。
func classify(err error) failureKind {
	rpcErr, isRPC := tgerr.As(err)
	switch {
	case !isRPC:
		return failureTransport
	case rpcErr.IsOneOf(ignorableErrors...):
		return failureIgnored
	case rpcErr.IsOneOf(chatLevelErrors...):
		return failureChat
	case rpcErr.IsOneOf(emojiErrors...):
		return failureEmoji
	case rpcErr.IsType("FLOOD_WAIT"):
		return failureFlood
	default:
		return failureOther
	}
}

// cooldown 在错误说明"这个对话点不了"时记下冷却。单独拆出来是因为这一步
// 漏掉了不会报错，只会让每个命中都再撞一次服务器——值得被测试直接压到。
func (s *service) cooldown(kind failureKind, chatID string, now time.Time) {
	if kind == failureChat {
		s.mutes.block(chatID, now)
	}
}

// backoff 处理"知道被拒绝了，但不知道为什么"的两类错误：表情被拒（REACTION_INVALID
// 既可能是配置写错，也可能是那个群把反应开关关了）和没列进白名单的新码。攒够连续三次
// 就把这个群歇一小时，返回是不是刚刚歇上。
//
// 连接抖动（failureTransport）和单条消息层面的失败（failureIgnored）不计数：前者是
// 我们自己的线路问题，后者每条评论各不相干，谁都不该让一个群背上黑名单。
func (s *service) backoff(kind failureKind, chatID string, now time.Time) bool {
	switch kind {
	case failureEmoji, failureOther:
		return s.mutes.noteFailure(chatID, now)
	default:
		return false
	}
}

// failed 按分类记下这一次失败，并在该冷却的时候冷却这个群。
func (s *service) failed(client *bot.Client, current task, err error, now time.Time) {
	label := err.Error()
	if rpcErr, isRPC := tgerr.As(err); isRPC {
		label = rpcErr.Message
	}
	kind := classify(err)
	s.cooldown(kind, current.chatID, now)
	tripped := s.backoff(kind, current.chatID, now)
	fields := []any{
		slog.String("chat", current.chatID),
		slog.Int("message", current.msgID),
		slog.String("emoji", current.emoji),
		slog.String("error", label),
	}
	switch kind {
	case failureIgnored:
		client.Logger().Debug("react.skipped", fields...)
	case failureChat:
		client.Logger().Warn("react.chat_skipped", append(fields, slog.Duration("cooldown", chatCooldown))...)
	case failureEmoji:
		client.Logger().Warn("react.emoji_rejected", fields...)
	case failureFlood:
		client.Logger().Warn("react.flood", fields...)
	default:
		client.Logger().Warn("react.failed", fields...)
	}
	if tripped {
		client.Logger().Warn("react.chat_backoff", append(fields,
			slog.Int("streak", failureTrip), slog.Duration("cooldown", failureBackoff))...)
	}
}
