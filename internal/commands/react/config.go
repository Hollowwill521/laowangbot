// Package react 实现 .react：命中关键词的消息，用本账号给它点一个表情。
//
// 它刻意不并进 monitor。monitor 的动作是"命中就转发并通知"，关键词表、群表、
// 去重都为那条链路服务；这里要的是另一套词、另一个范围，而且只做一个动作，
// 混在一起以后谁都说不清 `.monitor keyword add` 到底影响什么。
//
// 它也没做成 plugins/ 下的源码插件：internal/hostbridge 的方法白名单里
// 根本没有点赞要用的 messages.sendReaction，插件拿不到裸 API，硬要做就得先给
// 宿主协议加一个 react capability（server.go、dispatch.go、pkg/pluginapi 三处）。
// 内置命令握着 *bot.Client，一次裸调用就够，而且全是新增文件，
// 每次 rebase 上游时不会多一处冲突。
package react

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"
)

// defaultEmoji 是没配置过时的表情：Telegram 里最接近"赞"的那个。
const defaultEmoji = "👍"

// 两张表都有上限：设置面板要把它们整个显示在一条消息里，
// 每条经过的消息也要逐条扫一遍，列太长两头都吃亏。
const (
	maxKeywords = 200
	maxGroups   = 200
)

// Keyword 是一个关键词，以及命中它时要用的表情。
// Emoji 为空表示用配置里那个默认表情——"不同的词点不同的表情"就靠这个字段。
type Keyword struct {
	Text  string `json:"text"`
	Emoji string `json:"emoji,omitempty"`
}

// Config 是 .react 的全部设置，落在 data/react.json。
type Config struct {
	// Enabled 是总开关；关掉就一条消息都不看。
	Enabled bool `json:"enabled"`
	// AllGroups 为真时账号所在的每个超级群和频道都算范围；
	// 为假时只在 Groups 列出的群里点赞。
	AllGroups bool `json:"all_groups"`
	// Big 是 Telegram 的"大表情"动画。
	Big bool `json:"big"`
	// Emoji 是默认表情：命中的那个词没单独写表情时用它。
	Emoji    string    `json:"emoji"`
	Keywords []Keyword `json:"keywords"`
	// Groups 是群 ID 名单（带标记的十进制，形如 -100xxxx）。表情不挂在群上。
	Groups []string `json:"groups"`
}

// defaults 是新文件的初值：默认关着，免得装上就在全账号的群里到处点赞。
func defaults() Config {
	return Config{Emoji: defaultEmoji, Keywords: []Keyword{}, Groups: []string{}}
}

// rawConfig 让 keywords 和 groups 两个字段各自接受两种 JSON 形状：关键词可以是
// {text,emoji} 对象数组，也可以是纯字符串数组；群可以是 ID 字符串数组，也可以是
// 旧的对象数组。配置文件允许手改，而 store 的 clone 同样要过一遍 JSON 往返——
// 读不进来就等于把已经配好的词表静默清空，那种丢法在面板上看不出来，所以两种写法都得能读。
type rawConfig struct {
	Enabled   bool            `json:"enabled"`
	AllGroups bool            `json:"all_groups"`
	Big       bool            `json:"big"`
	Emoji     string          `json:"emoji"`
	Keywords  json.RawMessage `json:"keywords"`
	Groups    json.RawMessage `json:"groups"`
}

// UnmarshalJSON 接受新旧两种形状。
func (c *Config) UnmarshalJSON(raw []byte) error {
	var document rawConfig
	if err := json.Unmarshal(raw, &document); err != nil {
		return err
	}
	keywords, err := decodeKeywords(document.Keywords)
	if err != nil {
		return err
	}
	groups, err := decodeGroups(document.Groups)
	if err != nil {
		return err
	}
	c.Enabled, c.AllGroups, c.Big, c.Emoji = document.Enabled, document.AllGroups, document.Big, document.Emoji
	c.Keywords, c.Groups = keywords, groups
	return nil
}

func decodeKeywords(raw json.RawMessage) ([]Keyword, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var keywords []Keyword
	if err := json.Unmarshal(raw, &keywords); err == nil {
		return keywords, nil
	}
	var legacy []string
	if err := json.Unmarshal(raw, &legacy); err != nil {
		return nil, fmt.Errorf("keywords 既不是关键词表也不是旧的字符串数组：%w", err)
	}
	converted := make([]Keyword, 0, len(legacy))
	for _, text := range legacy {
		converted = append(converted, Keyword{Text: text})
	}
	return converted, nil
}

// legacyGroup 是旧形状里的群条目。它那个挂在群上的 emoji 在新形状里没有对应物
// （表情跟着词走，不跟着群走），只能丢掉；群名单本来就只决定"在哪些群里点"。
type legacyGroup struct {
	ChatID string `json:"chat_id"`
}

func decodeGroups(raw json.RawMessage) ([]string, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var ids []string
	if err := json.Unmarshal(raw, &ids); err == nil {
		return ids, nil
	}
	var legacy []legacyGroup
	if err := json.Unmarshal(raw, &legacy); err != nil {
		return nil, fmt.Errorf("groups 既不是群 ID 列表也不是旧的群表：%w", err)
	}
	ids = make([]string, 0, len(legacy))
	for _, group := range legacy {
		if group.ChatID != "" {
			ids = append(ids, group.ChatID)
		}
	}
	return ids, nil
}

// normalize 补齐缺省值、去掉空白和重复项。
//
// 手改过的或者更早版本写出的文件在启动时过一次这里，之后各处不用再防空表情、
// 空关键词和重复条目。
func (c *Config) normalize() {
	c.Emoji = strings.TrimSpace(c.Emoji)
	if c.Emoji == "" {
		c.Emoji = defaultEmoji
	}
	keywords := make([]Keyword, 0, len(c.Keywords))
	seenKeyword := map[string]bool{}
	for _, keyword := range c.Keywords {
		keyword.Text = strings.TrimSpace(keyword.Text)
		keyword.Emoji = strings.TrimSpace(keyword.Emoji)
		if keyword.Text == "" || seenKeyword[keyword.Text] {
			continue
		}
		seenKeyword[keyword.Text] = true
		keywords = append(keywords, keyword)
	}
	c.Keywords = keywords
	groups := make([]string, 0, len(c.Groups))
	seenGroup := map[string]bool{}
	for _, chatID := range c.Groups {
		chatID = strings.TrimSpace(chatID)
		if chatID == "" || seenGroup[chatID] {
			continue
		}
		seenGroup[chatID] = true
		groups = append(groups, chatID)
	}
	c.Groups = groups
}

// inScope 判断这个群在不在当前设置的范围内。
func (c Config) inScope(chatID string) bool {
	if c.AllGroups {
		return true
	}
	return c.hasGroup(chatID)
}

func (c Config) hasGroup(chatID string) bool {
	for _, existing := range c.Groups {
		if existing == chatID {
			return true
		}
	}
	return false
}

// keyword 按原文找一条关键词。
func (c Config) keyword(text string) (Keyword, bool) {
	for _, existing := range c.Keywords {
		if existing.Text == text {
			return existing, true
		}
	}
	return Keyword{}, false
}

// keywordIndex 找出关键词在表里的位置（从 1 开始，和面板显示一致）。
func (c Config) keywordIndex(text string) (int, bool) {
	for i, existing := range c.Keywords {
		if existing.Text == text {
			return i + 1, true
		}
	}
	return 0, false
}

// addKeyword 加一个关键词；表情为空表示用默认表情。
func (c *Config) addKeyword(keyword Keyword) (added bool, err error) {
	keyword.Text = strings.TrimSpace(keyword.Text)
	keyword.Emoji = strings.TrimSpace(keyword.Emoji)
	if keyword.Text == "" {
		return false, errors.New("关键词不能为空")
	}
	if keyword.Emoji != "" {
		if keyword.Emoji, err = normalizeEmoji(keyword.Emoji); err != nil {
			return false, err
		}
	}
	if _, exists := c.keywordIndex(keyword.Text); exists {
		return false, nil
	}
	if len(c.Keywords) >= maxKeywords {
		return false, fmt.Errorf("关键词最多 %d 个", maxKeywords)
	}
	c.Keywords = append(c.Keywords, keyword)
	return true, nil
}

// setKeywordEmoji 改掉某个关键词的表情。
func (c *Config) setKeywordEmoji(text, emoji string) (Keyword, error) {
	emoji, err := normalizeEmoji(emoji)
	if err != nil {
		return Keyword{}, err
	}
	for i, keyword := range c.Keywords {
		if keyword.Text != strings.TrimSpace(text) {
			continue
		}
		c.Keywords[i].Emoji = emoji
		return c.Keywords[i], nil
	}
	return Keyword{}, fmt.Errorf("表里没有 %s", text)
}

// removeKeyword 按序号或原文删关键词。序号优先：原文里可能有空格，
// 也可能是 `re:` 表达式，逐字符比对更容易删错。
func (c *Config) removeKeyword(argument string) (Keyword, error) {
	argument = strings.TrimSpace(argument)
	if argument == "" {
		return Keyword{}, errors.New("要写删哪个关键词：序号或者原文都行")
	}
	if len(c.Keywords) == 0 {
		return Keyword{}, errors.New("关键词表是空的")
	}
	if index, err := parseIndex(argument, len(c.Keywords)); err == nil {
		removed := c.Keywords[index]
		c.Keywords = append(c.Keywords[:index], c.Keywords[index+1:]...)
		return removed, nil
	}
	for i, keyword := range c.Keywords {
		if keyword.Text == argument {
			c.Keywords = append(c.Keywords[:i], c.Keywords[i+1:]...)
			return keyword, nil
		}
	}
	return Keyword{}, fmt.Errorf("表里没有 %s", argument)
}

// addGroup 加入一个群，返回是不是新加的。表情不挂在群上，所以这里只有 ID。
func (c *Config) addGroup(chatID string) (bool, error) {
	chatID = strings.TrimSpace(chatID)
	if chatID == "" {
		return false, errors.New("群 ID 是空的")
	}
	if c.hasGroup(chatID) {
		return false, nil
	}
	if len(c.Groups) >= maxGroups {
		return false, fmt.Errorf("群列表最多 %d 个", maxGroups)
	}
	c.Groups = append(c.Groups, chatID)
	return true, nil
}

// removeGroup 按序号或群 ID 删掉一个群。
func (c *Config) removeGroup(argument string) (string, error) {
	argument = strings.TrimSpace(argument)
	if argument == "" {
		return "", errors.New("要写删哪个群：序号或者群 ID 都行")
	}
	if len(c.Groups) == 0 {
		return "", errors.New("群列表是空的")
	}
	if index, err := parseIndex(argument, len(c.Groups)); err == nil {
		removed := c.Groups[index]
		c.Groups = append(c.Groups[:index], c.Groups[index+1:]...)
		return removed, nil
	}
	for i, chatID := range c.Groups {
		if chatID == argument {
			c.Groups = append(c.Groups[:i], c.Groups[i+1:]...)
			return chatID, nil
		}
	}
	return "", fmt.Errorf("列表里没有 %s", argument)
}

// normalizeEmoji 校验要设的表情。
//
// Telegram 每个反应位只收一个表情，写成一串会让每次点赞都失败，所以在这里挡住；
// 但"是不是一个真实存在的表情"只有发出去才知道（服务器回 REACTION_INVALID），
// 这里只管长度和空白：皮肤修饰符（👍🏻）和组合表情本来就是好几个码位，
// 不能按单码位卡死。
func normalizeEmoji(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	switch {
	case value == "":
		return "", errors.New("表情不能为空")
	case strings.ContainsAny(value, " \t\n\r"):
		return "", errors.New("表情中间不能有空格")
	case utf8.RuneCountInString(value) > 8:
		return "", fmt.Errorf("只能设一个表情，%d 个字符太多了", utf8.RuneCountInString(value))
	}
	return value, nil
}

// parseIndex 把"序号"参数换成下标；序号从 1 开始，和面板里显示的一致。
func parseIndex(raw string, length int) (int, error) {
	if length == 0 {
		return 0, errors.New("列表是空的")
	}
	number, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || number < 1 || number > length {
		return 0, fmt.Errorf("序号得是 1 到 %d 之间的数字", length)
	}
	return number - 1, nil
}
