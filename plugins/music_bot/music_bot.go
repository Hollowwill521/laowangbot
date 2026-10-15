// Package musicbot ports TeleBox's music_bot plugin to laowangbot.
//
// It searches music through third-party Telegram music bots and forwards the
// resulting audio file back to the chat where the command was issued:
//
//	mbs 关键词            -> @music_v1bot 搜索（关键词含来源自动识别）
//	mbkw 关键词           -> 酷我源
//	mbkg 关键词           -> 酷狗源
//	mbqq 关键词           -> QQ 音乐源
//	mbne 关键词           -> 网易云音乐源
//	mbvk 关键词           -> @vkmusic_bot
//	mbym 关键词           -> @ttaudiobot (YouTube Music)
//	music_bot <来源> 关键词 -> 同上，来源为 search/kugou/kuwo/qq/netease/vk/ym
package musicbot

import (
	"context"
	"encoding/json"
	"html"
	"strings"
	"sync"
	"time"

	api "github.com/OrionG-hub/laowangbot/pkg/pluginapi"
)

const (
	defaultBot = "@music_v1bot"
	vkBot      = "@vkmusic_bot"
	ymBot      = "@ttaudiobot"
)

var validActions = map[string]bool{
	"search": true, "kugou": true, "kuwo": true,
	"qq": true, "netease": true, "vk": true, "ym": true,
}

// Plugin implements api.Plugin.
type Plugin struct {
	host  api.Host
	dir   string
	mu    sync.Mutex
	ready map[string]bool
}

// New constructs the plugin; ctx is the plugin lifetime context.
func New(_ context.Context, host api.Host, dir string) (*Plugin, error) {
	return &Plugin{host: host, dir: dir, ready: map[string]bool{}}, nil
}

// Close releases plugin resources.
func (p *Plugin) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.ready = map[string]bool{}
}

// Handle dispatches command requests; other request types are ignored.
func (p *Plugin) Handle(ctx context.Context, req api.Request) api.Response {
	if req.Type != "command" {
		return api.Response{Version: 1}
	}
	var ev api.Event
	if len(req.Event) > 0 {
		_ = json.Unmarshal(req.Event, &ev)
	}
	action, keyword, display := parseCommand(req.Command, req.Args)
	if !validActions[action] || strings.TrimSpace(keyword) == "" {
		return api.Response{Version: 1, HTML: true, Text: helpHTML()}
	}
	botName := defaultBot
	switch action {
	case "vk":
		botName = vkBot
	case "ym":
		botName = ymBot
	}
	p.searchAndSend(ctx, ev, action, keyword, display, botName)
	return api.Response{Version: 1}
}

// parseCommand maps the invoked command and its args to a search action,
// the keyword sent to the music bot, and the keyword shown to the user.
func parseCommand(cmd string, args []string) (action, keyword, display string) {
	switch cmd {
	case "music_bot":
		if len(args) > 0 {
			action = strings.ToLower(args[0])
			keyword = strings.Join(args[1:], " ")
		}
		display = keyword
		return action, keyword, display
	case "mbym":
		action = "ym"
		base := strings.Join(args, " ")
		display = base
		if base != "" {
			// Faithful port of the original TeleBox plugin behavior.
			keyword = base + " lyric】"
		}
		return action, keyword, display
	}
	switch cmd {
	case "mbs":
		action = "search"
	case "mbkw":
		action = "kuwo"
	case "mbkg":
		action = "kugou"
	case "mbqq":
		action = "qq"
	case "mbne":
		action = "netease"
	case "mbvk":
		action = "vk"
	}
	keyword = strings.Join(args, " ")
	return action, keyword, keyword
}

// searchAndSend drives the conversation with the music bot: send the search
// query, click the first result button, then forward the returned audio file
// to the chat where the command was issued.
func (p *Plugin) searchAndSend(ctx context.Context, ev api.Event, action, keyword, display, botName string) {
	chatID, msgID := ev.ChatID, ev.MessageID
	fail := func(text string) {
		_, _ = p.host.Call(ctx, api.Call{Method: "edit", Target: chatID, MessageID: msgID, Text: text, HTML: true})
	}
	if chatID == "" || msgID <= 0 {
		return
	}
	_, _ = p.host.Call(ctx, api.Call{
		Method: "edit", Target: chatID, MessageID: msgID,
		Text: "🔎 搜索中：<code>" + html.EscapeString(display) + "</code>", HTML: true,
	})

	startTs := time.Now().Unix()
	var query string
	if action == "vk" || action == "ym" {
		query = keyword
	} else {
		query = "/" + action + " " + keyword
	}
	if _, err := p.host.Call(ctx, api.Call{Method: "send", Target: botName, Text: query}); err != nil {
		p.mu.Lock()
		initialized := p.ready[botName]
		p.mu.Unlock()
		if !initialized {
			// First contact with this bot: try /start once, then retry.
			_, _ = p.host.Call(ctx, api.Call{Method: "send", Target: botName, Text: "/start"})
			p.mu.Lock()
			p.ready[botName] = true
			p.mu.Unlock()
			time.Sleep(500 * time.Millisecond)
			if _, err := p.host.Call(ctx, api.Call{Method: "send", Target: botName, Text: query}); err != nil {
				_, _ = p.host.Call(ctx, api.Call{Method: "send", Target: botName, Text: keyword})
			}
		} else {
			_, _ = p.host.Call(ctx, api.Call{Method: "send", Target: botName, Text: keyword})
		}
	}

	// Wait for the bot's reply carrying result buttons.
	var reply *api.Message
	for i := 0; i < 15; i++ {
		time.Sleep(700 * time.Millisecond)
		for _, m := range p.botHistory(ctx, botName, 5) {
			if m.Out || int64(m.Date) < startTs || len(m.Buttons) == 0 {
				continue
			}
			c := m
			reply = &c
			break
		}
		if reply != nil {
			break
		}
	}
	if reply == nil {
		fail("⚠️ 机器人未启用或未响应，请先打开 " + html.EscapeString(botName) + " 并点击 Start，然后重试。")
		return
	}

	clicked := false
	if _, err := p.host.Call(ctx, api.Call{Method: "click", Target: botName, MessageID: reply.ID, Row: 0, Column: 0}); err == nil {
		clicked = true
	}
	if !clicked {
		// Fallback for bots whose buttons are plain-text replies.
		_, _ = p.host.Call(ctx, api.Call{Method: "send", Target: botName, Text: "1"})
	}

	// Wait for the audio file message.
	var media *api.Message
	for i := 0; i < 20; i++ {
		time.Sleep(700 * time.Millisecond)
		for _, m := range p.botHistory(ctx, botName, 6) {
			if m.Out || m.Date < reply.Date || !m.HasMedia {
				continue
			}
			c := m
			media = &c
			break
		}
		if media != nil {
			break
		}
	}
	if media == nil {
		fail("❌ 未获取到音乐文件。")
		return
	}

	if _, err := p.host.Call(ctx, api.Call{
		Method: "forward", Target: chatID, User: botName, IDs: []int{media.ID},
	}); err != nil {
		fail("❌ 转发失败：" + html.EscapeString(err.Error()))
		return
	}
	_, _ = p.host.Call(ctx, api.Call{Method: "delete", Target: chatID, IDs: []int{msgID}})
}

// botHistory returns the most recent messages of the music bot chat,
// oldest first.
func (p *Plugin) botHistory(ctx context.Context, botName string, limit int) []api.Message {
	r, err := p.host.Call(ctx, api.Call{Method: "history", Target: botName, Limit: limit})
	if err != nil || len(r.Messages) == 0 {
		return nil
	}
	msgs := r.Messages
	for i, j := 0, len(msgs)-1; i < j; i, j = i+1, j-1 {
		msgs[i], msgs[j] = msgs[j], msgs[i]
	}
	return msgs
}

func helpHTML() string {
	return `🎵 <b>多音源音乐搜索</b>（依赖 @music_v1bot / @vkmusic_bot / @ttaudiobot）

<code>.mbs 关键词</code> 综合搜索（关键词含来源自动识别，如 <code>.mbs 洛天依 网易云</code>）
<code>.mbkw 关键词</code> 酷我源
<code>.mbkg 关键词</code> 酷狗源
<code>.mbqq 关键词</code> QQ 音乐源
<code>.mbne 关键词</code> 网易云音乐源
<code>.mbvk 关键词</code> VK 音乐源
<code>.mbym 关键词</code> YouTube Music 源
<code>.music_bot &lt;来源&gt; 关键词</code> 指定来源：search / kugou / kuwo / qq / netease / vk / ym`
}
