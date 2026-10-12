package react

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/gotd/td/tg"

	"github.com/OrionG-hub/laowangbot/internal/bot"
	"github.com/OrionG-hub/laowangbot/internal/command"
	"github.com/OrionG-hub/laowangbot/internal/commands/kit"
)

// handle 是 .react 的全部入口。参数不对就当场报错，一个字都不存：
// 半截配置比没配置更难查。
func (s *service) handle(ctx context.Context, inv *command.Invocation) error {
	switch strings.ToLower(inv.Arg(0)) {
	case "":
		return inv.EditHelp(ctx, helpText(inv.Prefix)+s.status(inv))
	case "help", "h":
		return inv.EditHelp(ctx, helpText(inv.Prefix))
	case "status", "state":
		return inv.EditHelp(ctx, s.status(inv))
	case "on", "off":
		return s.setSwitch(ctx, inv, strings.EqualFold(inv.Arg(0), "on"))
	case "all":
		return s.setAllGroups(ctx, inv)
	case "big":
		return s.setBig(ctx, inv)
	case "emoji":
		return s.setDefaultEmoji(ctx, inv)
	case "keyword", "keywords":
		return s.keyword(ctx, inv)
	case "group", "groups":
		return s.group(ctx, inv)
	default:
		return s.fail(ctx, inv, fmt.Errorf("不认识的参数 %s，用 %sreact 看帮助", inv.Arg(0), inv.Prefix))
	}
}

// setSwitch 管总开关。开着但关键词还是空的，得说一句——
// 不然用户等半天没反应，会以为功能坏了。
func (s *service) setSwitch(ctx context.Context, inv *command.Invocation, enabled bool) error {
	if err := s.mutate(func(cfg *Config) error {
		cfg.Enabled = enabled
		return nil
	}); err != nil {
		return s.fail(ctx, inv, err)
	}
	detail := ""
	if enabled && len(s.settings(inv.Client).Keywords) == 0 {
		detail = "关键词还是空的，现在一条都不会点"
	}
	return inv.Edit(ctx, kit.Feedback("success", "点赞已"+onOffWord(enabled), detail))
}

func (s *service) setAllGroups(ctx context.Context, inv *command.Invocation) error {
	all, err := switchValue(inv.Arg(1))
	if err != nil {
		return s.fail(ctx, inv, err)
	}
	if err := s.mutate(func(cfg *Config) error {
		cfg.AllGroups = all
		return nil
	}); err != nil {
		return s.fail(ctx, inv, err)
	}
	scope := "只在列出的群里"
	if all {
		scope = "所有超级群和频道"
	}
	return inv.Edit(ctx, kit.Feedback("success", "范围已改为"+scope, groupListHint(s.settings(inv.Client))))
}

func (s *service) setBig(ctx context.Context, inv *command.Invocation) error {
	big, err := switchValue(inv.Arg(1))
	if err != nil {
		return s.fail(ctx, inv, err)
	}
	if err := s.mutate(func(cfg *Config) error {
		cfg.Big = big
		return nil
	}); err != nil {
		return s.fail(ctx, inv, err)
	}
	return inv.Edit(ctx, kit.Feedback("success", "大表情已"+onOffWord(big), ""))
}

// setDefaultEmoji 改的是"兜底"表情：命中的词自己没写表情时才会用到它。
func (s *service) setDefaultEmoji(ctx context.Context, inv *command.Invocation) error {
	emoji, err := normalizeEmoji(inv.Rest(1))
	if err != nil {
		return s.fail(ctx, inv, err)
	}
	if err := s.mutate(func(cfg *Config) error {
		cfg.Emoji = emoji
		return nil
	}); err != nil {
		return s.fail(ctx, inv, err)
	}
	return inv.Edit(ctx, kit.Feedback("success", "默认表情已设为 "+emoji, "词上写了表情的不受影响：keyword emoji 序号 表情"))
}

// keyword 处理关键词表：加词可以顺手带表情，也可以事后单独改某个词的表情。
func (s *service) keyword(ctx context.Context, inv *command.Invocation) error {
	switch action := strings.ToLower(inv.Arg(1)); action {
	case "add":
		return s.keywordAdd(ctx, inv)
	case "emoji", "set":
		return s.keywordEmoji(ctx, inv)
	case "del", "delete", "rm", "remove":
		var removed Keyword
		if err := s.mutate(func(cfg *Config) error {
			var err error
			removed, err = cfg.removeKeyword(inv.Rest(2))
			return err
		}); err != nil {
			return s.fail(ctx, inv, err)
		}
		s.match.reset()
		return inv.Edit(ctx, kit.Feedback("success", "已删掉 "+removed.Text, ""))
	case "clear":
		if err := s.mutate(func(cfg *Config) error {
			cfg.Keywords = nil
			return nil
		}); err != nil {
			return s.fail(ctx, inv, err)
		}
		s.match.reset()
		return inv.Edit(ctx, kit.Feedback("success", "关键词已清空", ""))
	default:
		return s.fail(ctx, inv, errors.New("关键词要加 add、emoji、del 还是 clear"))
	}
}

// keywordAdd 加词。参数是 `keyword add <词> [表情]`：词可以含空格，表情只能是最后一个参数。
func (s *service) keywordAdd(ctx context.Context, inv *command.Invocation) error {
	keyword, emoji, err := splitKeywordArgs(inv.Rest(2))
	if err != nil {
		return s.fail(ctx, inv, err)
	}
	if err := validateKeyword(keyword); err != nil {
		return s.fail(ctx, inv, err)
	}
	var added bool
	if err := s.mutate(func(cfg *Config) error {
		var err error
		added, err = cfg.addKeyword(Keyword{Text: keyword, Emoji: emoji})
		return err
	}); err != nil {
		return s.fail(ctx, inv, err)
	}
	s.match.reset()
	rank, _ := s.settings(inv.Client).keywordIndex(keyword)
	position := "第 " + strconv.Itoa(rank) + " 项"
	if !added {
		return inv.Edit(ctx, kit.Feedback("success", position+"已经是它了", "要换它的表情：keyword emoji "+strconv.Itoa(rank)+" 表情"))
	}
	return inv.Edit(ctx, kit.Feedback("success", "已加上 "+position, keywordDetail(s.settings(inv.Client), Keyword{Text: keyword, Emoji: emoji})+"；"+describeKeyword(keyword)))
}

// keywordEmoji 改某个已有关键词的表情：`keyword emoji <序号|词> <表情>`。
func (s *service) keywordEmoji(ctx context.Context, inv *command.Invocation) error {
	argument, emoji := strings.TrimSpace(inv.Arg(2)), strings.TrimSpace(inv.Rest(3))
	if argument == "" || emoji == "" {
		return s.fail(ctx, inv, errors.New("要写成 keyword emoji 序号 表情"))
	}
	var updated Keyword
	if err := s.mutate(func(cfg *Config) error {
		// 先按序号定位，再把原文当别名试一遍，和删词的做法保持一致。
		if index, err := parseIndex(argument, len(cfg.Keywords)); err == nil {
			argument = cfg.Keywords[index].Text
		}
		var err error
		updated, err = cfg.setKeywordEmoji(argument, emoji)
		return err
	}); err != nil {
		return s.fail(ctx, inv, err)
	}
	return inv.Edit(ctx, kit.Feedback("success", updated.Text+" 现在点 "+updated.Emoji, ""))
}

// group 处理群名单：add 可以点名某个群，也可以不带参数，表示"我此刻所在的这个群"。
// 表情不挂在群上，所以这里没有表情参数。
func (s *service) group(ctx context.Context, inv *command.Invocation) error {
	switch action := strings.ToLower(inv.Arg(1)); action {
	case "add":
		argument := strings.TrimSpace(inv.Arg(2))
		chatID, err := s.resolveGroup(ctx, inv, argument)
		if err != nil {
			return s.fail(ctx, inv, err)
		}
		var added bool
		if err := s.mutate(func(cfg *Config) error {
			var err error
			added, err = cfg.addGroup(chatID)
			return err
		}); err != nil {
			return s.fail(ctx, inv, err)
		}
		title := "已加入 " + chatID
		if !added {
			title = chatID + " 本来就在列表里"
		}
		return inv.Edit(ctx, kit.Feedback("success", title, groupNote(s.settings(inv.Client), inv.Client, chatID, argument)))
	case "del", "delete", "rm", "remove":
		var removed string
		if err := s.mutate(func(cfg *Config) error {
			var err error
			removed, err = cfg.removeGroup(inv.Rest(2))
			return err
		}); err != nil {
			return s.fail(ctx, inv, err)
		}
		return inv.Edit(ctx, kit.Feedback("success", "已删掉 "+removed, ""))
	case "clear":
		if err := s.mutate(func(cfg *Config) error {
			cfg.Groups = nil
			return nil
		}); err != nil {
			return s.fail(ctx, inv, err)
		}
		return inv.Edit(ctx, kit.Feedback("success", "群列表已清空", ""))
	default:
		return s.fail(ctx, inv, errors.New("群要加 add、del 还是 clear"))
	}
}

// resolveGroup 定出群 ID。不带参数时用当前对话：在群里发一句 `.react group add`
// 就该把这个群加进来，不该逼人先去抄一个带 -100 的号码。
func (s *service) resolveGroup(ctx context.Context, inv *command.Invocation, argument string) (string, error) {
	if argument == "" {
		if !supportsReactions(inv.Message.ChatType) {
			return "", errors.New("当前对话不是超级群或频道；Telegram 只在这两种对话里支持点赞")
		}
		return inv.Message.ChatID, nil
	}
	peer, err := inv.Client.ResolveTarget(ctx, normalizeTarget(argument))
	if err != nil {
		if errors.Is(err, bot.ErrUnaddressablePeer) {
			return "", fmt.Errorf("没见过 %s：账号得先在这个群里，或者改用 @公开用户名", argument)
		}
		return "", fmt.Errorf("找不到 %s：%w", argument, err)
	}
	return chatIDOf(peer)
}

// chatIDOf 把解析出来的 peer 换成配置里存的那种带标记的十进制 ID。
func chatIDOf(peer tg.InputPeerClass) (string, error) {
	switch value := peer.(type) {
	case *tg.InputPeerChannel:
		return bot.PeerID(&tg.PeerChannel{ChannelID: value.ChannelID}), nil
	case *tg.InputPeerChat:
		return "", errors.New("旧式普通小群不支持点赞，先在 Telegram 里把它升级成超级群")
	case *tg.InputPeerUser:
		return "", errors.New("这是私聊；点赞只在超级群和频道里做")
	case *tg.InputPeerSelf:
		return "", errors.New("这是收藏夹；点赞只在超级群和频道里做")
	}
	return "", errors.New("认不出这是哪个群")
}

// normalizeTarget 允许直接粘 t.me 链接，只取链接里的公开用户名那一段。
func normalizeTarget(raw string) string {
	value := strings.TrimSpace(raw)
	index := strings.Index(value, "t.me/")
	if index < 0 {
		return value
	}
	rest := value[index+len("t.me/"):]
	parts := strings.FieldsFunc(rest, func(r rune) bool { return r == '/' || r == '?' || r == '#' })
	if len(parts) == 0 {
		return value
	}
	return "@" + parts[0]
}

// supportsReactions 是 Telegram 的硬限制：只有超级群和频道有反应列表，
// 普通小群连开关都没有；私聊虽然能点赞，但不是这个功能要的。
func supportsReactions(chatType bot.ChatType) bool {
	return chatType == bot.ChatSupergroup || chatType == bot.ChatBroadcast
}

// splitKeywordArgs 把 `keyword add` 后面的整段拆成词和表情。
//
// 表情只能是最后一个不带空白的片段：词本身可以含空格（"我要 红包"），
// 而一个合法表情里绝不会出现空白，所以按最后一个字段切最稳。
func splitKeywordArgs(rest string) (keyword, emoji string, err error) {
	rest = strings.TrimSpace(rest)
	if rest == "" {
		return "", "", errors.New("要写关键词，后面可以跟一个表情")
	}
	fields := strings.Fields(rest)
	candidate := fields[len(fields)-1]
	if len(fields) > 1 && looksLikeEmoji(candidate) {
		keyword = strings.TrimSpace(strings.Join(fields[:len(fields)-1], " "))
		emoji, err = normalizeEmoji(candidate)
		return keyword, emoji, err
	}
	return rest, "", nil
}

// looksLikeEmoji 只在最后一个片段整段都是表情码位时，才把它当表情。
//
// 中文、拉丁字母和数字都不在表情区，所以 `keyword add 我要 抽奖` 里的"抽奖"
// 仍然是关键词的一部分，而 `keyword add 我要 ❤️` 里的心形才是表情。
// 真正的对错还是由 normalizeEmoji 和服务器（REACTION_INVALID）把关。
func looksLikeEmoji(candidate string) bool {
	runes := []rune(candidate)
	if len(runes) == 0 || len(runes) > 8 {
		return false
	}
	for _, value := range runes {
		if !isEmojiRune(value) {
			return false
		}
	}
	return true
}

// isEmojiRune 覆盖常用的表情区：图形符号主区、杂项符号和箭头、星号方框区、
// 国旗的区域指示符，以及拼组合表情要用的变体选择符 16 和零宽连接符。
func isEmojiRune(value rune) bool {
	switch {
	case value >= 0x1F000 && value <= 0x1FAFF: // 牌类、表情、手势、图标、符号扩展
		return true
	case value >= 0x2600 && value <= 0x27BF: // 杂项符号和箭头：☀ ✅ ❌ ❤
		return true
	case value >= 0x2B00 && value <= 0x2BFF: // ⭐ ⬆ 等
		return true
	case value >= 0x1F1E6 && value <= 0x1F1FF: // 区域指示符（国旗）
		return true
	case value == 0xFE0F || value == 0x200D: // 变体选择符 16、零宽连接符
		return true
	}
	return false
}

// status 是面板的下半截：现在到底会点什么、在哪些群点、点成功了几个。
func (s *service) status(inv *command.Invocation) string {
	cfg := s.settings(inv.Client)
	var panel strings.Builder
	panel.WriteString("\n\n<b>当前状态</b>\n")
	fmt.Fprintf(&panel, "• 总开关：%s\n", onOffWord(cfg.Enabled))
	if cfg.AllGroups {
		panel.WriteString("• 范围：所有超级群和频道\n")
	} else {
		fmt.Fprintf(&panel, "• 范围：只在列出的 %d 个群里\n", len(cfg.Groups))
	}
	fmt.Fprintf(&panel, "• 默认表情：<code>%s</code>（大表情：%s）\n", command.Escape(cfg.Emoji), onOffWord(cfg.Big))
	fmt.Fprintf(&panel, "• 已成功点赞 %d 条，队列丢弃 %d 条", s.reacted.Load(), s.dropped.Load())
	if blocked := s.mutes.list(time.Now()); len(blocked) > 0 {
		fmt.Fprintf(&panel, "，暂时跳过 %d 个群（%s）", len(blocked), command.Escape(strings.Join(blocked, "、")))
	}

	fmt.Fprintf(&panel, "\n\n<b>关键词</b>（%d 个）\n", len(cfg.Keywords))
	if len(cfg.Keywords) == 0 {
		panel.WriteString("• 还没设，现在一条都不会点")
	}
	for index, keyword := range cfg.Keywords {
		line := fmt.Sprintf("%d. <code>%s</code>", index+1, command.Escape(keyword.Text))
		if emoji := strings.TrimSpace(keyword.Emoji); emoji != "" {
			line += "　<code>" + command.Escape(emoji) + "</code>"
		} else {
			line += "　用默认"
		}
		panel.WriteString(line + "\n")
	}

	fmt.Fprintf(&panel, "\n\n<b>群</b>（%d 个）\n", len(cfg.Groups))
	switch {
	case len(cfg.Groups) > 0:
		for index, chatID := range cfg.Groups {
			line := fmt.Sprintf("%d. %s", index+1, command.Escape(chatID))
			if title := channelTitle(inv.Client, chatID); title != "" {
				line += " " + command.Escape(title)
			}
			panel.WriteString(line + "\n")
		}
	case cfg.AllGroups:
		panel.WriteString("• 没列群也在点，因为范围是所有群")
	default:
		panel.WriteString("• 空的；在群里发一句 <code>.react group add</code> 就能把它加进来")
	}
	return panel.String()
}

// keywordDetail 说明某个词命中后点的是哪个表情。
func keywordDetail(cfg Config, keyword Keyword) string {
	emoji := strings.TrimSpace(keyword.Emoji)
	if emoji == "" {
		return "表情用默认的 " + cfg.Emoji
	}
	return "命中就点 " + emoji
}

// groupNote 解释这次 group add 到底改变了什么。
func groupNote(cfg Config, client *bot.Client, chatID, argument string) string {
	note := ""
	if title := channelTitle(client, chatID); title != "" {
		note = title
	}
	if !cfg.AllGroups {
		return note
	}
	if note != "" {
		note += "；"
	}
	return note + "范围本来就是所有群，加不加这条都一样"
}

func groupListHint(cfg Config) string {
	if cfg.AllGroups || len(cfg.Groups) == 0 {
		return ""
	}
	return "当前列出的是：" + strings.Join(cfg.Groups, "、")
}

// channelTitle 用 peer 缓存里现成的群名点缀面板。缓存里没有就只报 ID：
// 为了一个标题专门发一次 RPC 不值得，monitor 也是这么取舍的。
func channelTitle(client *bot.Client, chatID string) string {
	id, err := strconv.ParseInt(strings.TrimPrefix(chatID, "-100"), 10, 64)
	if err != nil || id == 0 {
		return ""
	}
	info, ok := client.Peers().Channel(id)
	if !ok || strings.TrimSpace(info.Title) == "" {
		return ""
	}
	return info.Title
}

// mutate 存一次配置。apply 返回错误时 store 原样不动，所以坏参数不会留下半截配置。
// 存成功就作废快照，让下一次读取立刻拿到新值。
func (s *service) mutate(apply func(cfg *Config) error) error {
	if err := s.store.Update(apply); err != nil {
		return err
	}
	s.forget()
	return nil
}

// fail 把配置错误原样报回去，不写日志：这些是用户自己打错了，
// 值得他当场看见，不值得进日志。
func (s *service) fail(ctx context.Context, inv *command.Invocation, err error) error {
	return inv.EditText(ctx, "❌ 没改成："+err.Error())
}

func switchValue(argument string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(argument)) {
	case "on", "true", "1", "yes":
		return true, nil
	case "off", "false", "0", "no":
		return false, nil
	}
	return false, errors.New("要写 on 或 off")
}

func onOffWord(value bool) string {
	if value {
		return "开着"
	}
	return "关着"
}

// describeKeyword 提醒一句 `re:` 是正则而不是字面量：把它当普通词写的人，
// 会在群里白等一个永远不会来的点赞。
func describeKeyword(keyword string) string {
	if strings.HasPrefix(strings.ToLower(keyword), "re:") {
		return "按正则匹配整条正文"
	}
	return "忽略大小写，正文或说明里含这个词就算命中"
}
