package hostbridge

import (
	"context"
	"errors"
	"github.com/OrionG-hub/laowangbot/internal/bot"
	"github.com/OrionG-hub/laowangbot/pkg/pluginapi"
	"github.com/gotd/td/tg"
	"math/rand/v2"
	"net/url"
	"strconv"
	"strings"
)

func dispatch(ctx context.Context, b *bot.Client, c pluginapi.Call) (r pluginapi.Result, err error) {
	if c.Method == "send_file" && (c.Filename == "" || strings.ContainsAny(c.Filename, "/\\\x00") || len(c.Bytes) == 0 || len(c.Bytes) > 512<<10) {
		return r, errors.New("send_file requires a filename and 1..524288 bytes")
	}
	if c.Method == "self" {
		r.Entity = entity(b, &tg.InputPeerSelf{})
		return
	}
	if c.Method == "folders" {
		return folders(ctx, b)
	}
	if c.Target == "" {
		return r, errors.New("target required")
	}
	p, err := resolve(ctx, b, c.Target)
	if err != nil {
		return r, err
	}
	switch c.Method {
	case "chat_action":
		err = chatAction(ctx, b, p, c.Action, c.FolderID)
	case "user_info":
		return userInfo(ctx, b, p)
	case "send_photo":
		return sendPhoto(ctx, b, p, c)
	case "resolve":
		r.Entity = entity(b, p)
	case "identity":
		r.Identity, err = identity(ctx, b, p, c.User)
	case "history":
		if c.OffsetID < 0 {
			return r, errors.New("offset_id cannot be negative")
		}
		if c.Limit < 1 || c.Limit > 100 {
			return r, errors.New("limit must be 1..100")
		}
		var result tg.MessagesMessagesClass
		result, err = b.API().MessagesGetHistory(ctx, &tg.MessagesGetHistoryRequest{Peer: p, Limit: c.Limit, OffsetID: c.OffsetID})
		if err == nil {
			ms, _ := b.Unpack(result)
			for _, m := range ms {
				if v, ok := m.(*tg.Message); ok {
					r.Messages = append(r.Messages, serializeFor(b, v))
				}
			}
		}
	case "messages", "delete", "forward":
		if len(c.IDs) < 1 || len(c.IDs) > 100 {
			return r, errors.New("ids must contain 1..100 message IDs")
		}
		for _, id := range c.IDs {
			if id <= 0 {
				return r, errors.New("message IDs must be positive")
			}
		}
		switch c.Method {
		case "messages":
			var ms []*tg.Message
			ms, err = b.GetMessages(ctx, p, c.IDs)
			for _, m := range ms {
				r.Messages = append(r.Messages, serializeFor(b, m))
			}
		case "delete":
			err = b.Delete(ctx, p, c.IDs)
		case "forward":
			if c.User == "" {
				return r, errors.New("source required")
			}
			var source tg.InputPeerClass
			source, err = resolve(ctx, b, c.User)
			if err == nil {
				err = b.Forward(ctx, source, p, c.IDs)
			}
		}
	case "send":
		if c.HTML {
			r.MessageID, err = b.SendHTML(ctx, p, c.Text, bot.SendOptions{ReplyTo: c.ReplyTo})
		} else {
			r.MessageID, err = b.SendText(ctx, p, c.Text, bot.SendOptions{ReplyTo: c.ReplyTo})
		}
	case "send_file":
		if c.MimeType == "" {
			c.MimeType = "application/octet-stream"
		}
		err = b.SendDocument(ctx, p, c.Filename, c.MimeType, c.Bytes, bot.Escape(c.Text), c.ReplyTo)
	case "edit":
		if c.MessageID <= 0 {
			return r, errors.New("message_id required")
		}
		text := c.Text
		if !c.HTML {
			text = bot.Escape(text)
		}
		err = b.EditMessage(ctx, p, c.MessageID, text, false)
	case "download", "click":
		if c.MessageID <= 0 {
			return r, errors.New("message_id required")
		}
		var ms []*tg.Message
		ms, err = b.GetMessages(ctx, p, []int{c.MessageID})
		if err != nil {
			return
		}
		if len(ms) != 1 {
			return r, errors.New("message not found")
		}
		if c.Method == "download" {
			var f *bot.MediaFile
			f, err = b.DownloadMedia(ctx, ms[0], MaxMedia)
			if err == nil {
				r.Bytes = f.Data
				r.MimeType = f.MimeType
			}
		} else {
			return click(ctx, b, p, ms[0], c.Row, c.Column)
		}
	case "webview":
		return webview(ctx, b, p, c.URL, c.Simple)
	case "webview_data":
		u, ok := bot.InputUser(p)
		if !ok {
			return r, errors.New("bot target must be a user")
		}
		_, err = b.API().MessagesSendWebViewData(ctx, &tg.MessagesSendWebViewDataRequest{Bot: u, RandomID: rand.Int64(), ButtonText: c.ButtonText, Data: c.Data})
	default:
		err = errors.New("unknown method")
	}
	return
}
func entity(b *bot.Client, p tg.InputPeerClass) *pluginapi.Entity {
	e := &pluginapi.Entity{}
	switch v := p.(type) {
	case *tg.InputPeerSelf:
		u := b.Self()
		e.ID = strconv.FormatInt(u.ID, 10)
		e.Name = strings.TrimSpace(u.FirstName + " " + u.LastName)
		e.Username = u.Username
		e.Bot = u.Bot
		e.Self = true
	case *tg.InputPeerUser:
		e.ID = bot.PeerID(&tg.PeerUser{UserID: v.UserID})
		if u, ok := b.Peers().User(v.UserID); ok {
			e.Name = u.DisplayName()
			e.Username = u.Username
			e.Bot = u.Bot
		}
		e.Self = v.UserID == b.SelfID()
	case *tg.InputPeerChannel:
		e.ID = bot.PeerID(&tg.PeerChannel{ChannelID: v.ChannelID})
		if ch, ok := b.Peers().Channel(v.ChannelID); ok {
			e.Name = ch.Title
			e.Username = ch.Username
			e.Broadcast = ch.Broadcast
			e.Group = ch.Megagroup
		}
	case *tg.InputPeerChat:
		e.ID = bot.PeerID(&tg.PeerChat{ChatID: v.ChatID})
		e.Group = true
		if ch, ok := b.Peers().Chat(v.ChatID); ok {
			e.Name = ch.Title
		}
	}
	return e
}
func identity(ctx context.Context, b *bot.Client, p tg.InputPeerClass, user string) (string, error) {
	if user == "" {
		return "", errors.New("user required")
	}
	u, err := resolve(ctx, b, user)
	if err != nil {
		return "", err
	}
	e := entity(b, u)
	if e.Self {
		return "owner", nil
	}
	if e.Bot {
		return "bot", nil
	}
	if ch, ok := bot.InputChannel(p); ok {
		v, err := b.API().ChannelsGetParticipant(ctx, &tg.ChannelsGetParticipantRequest{Channel: ch, Participant: u})
		if err != nil {
			return "", err
		}
		switch v.Participant.(type) {
		case *tg.ChannelParticipantCreator:
			return "admin", nil
		case *tg.ChannelParticipantAdmin:
			return "admin", nil
		}
		return "user", nil
	}
	if ch, ok := p.(*tg.InputPeerChat); ok {
		v, err := b.API().MessagesGetFullChat(ctx, ch.ChatID)
		if err != nil {
			return "", err
		}
		if full, ok := v.FullChat.(*tg.ChatFull); ok {
			if ps, ok := full.Participants.(*tg.ChatParticipants); ok {
				for _, participant := range ps.Participants {
					switch x := participant.(type) {
					case *tg.ChatParticipantCreator:
						if strconv.FormatInt(x.UserID, 10) == e.ID {
							return "admin", nil
						}
					case *tg.ChatParticipantAdmin:
						if strconv.FormatInt(x.UserID, 10) == e.ID {
							return "admin", nil
						}
					}
				}
			}
		}
	}
	return "user", nil
}
func webview(ctx context.Context, b *bot.Client, p tg.InputPeerClass, url string, simple bool) (r pluginapi.Result, err error) {
	return webviewWithBot(ctx, b, p, p, url, simple)
}
func webviewWithBot(ctx context.Context, b *bot.Client, p, botPeer tg.InputPeerClass, url string, simple bool) (r pluginapi.Result, err error) {
	u, ok := bot.InputUser(botPeer)
	if !ok {
		return r, errors.New("bot target must be a user")
	}
	if simple {
		q := &tg.MessagesRequestSimpleWebViewRequest{Bot: u, Platform: "android"}
		q.SetURL(url)
		var v *tg.WebViewResultURL
		v, err = b.API().MessagesRequestSimpleWebView(ctx, q)
		if err == nil {
			r.URL = v.URL
		}
	} else {
		q := &tg.MessagesRequestWebViewRequest{Peer: p, Bot: u, Platform: "android"}
		q.SetURL(url)
		var v *tg.WebViewResultURL
		v, err = b.API().MessagesRequestWebView(ctx, q)
		if err == nil {
			r.URL = v.URL
		}
	}
	return
}
func buttons(m *tg.Message) []pluginapi.Button {
	var out []pluginapi.Button
	add := func(row, col int, text string, typ any) {
		p := pluginapi.Button{Row: row, Column: col, Text: text, Kind: "unsupported"}
		switch x := typ.(type) {
		case *tg.ButtonTypeDefault:
			p.Kind = "reply"
		case *tg.InlineButtonTypeCallback:
			p.Kind = "callback"
			p.Data = x.Data
		case *tg.InlineButtonTypeURL:
			p.Kind = "url"
			p.URL = x.URL
		case *tg.InlineButtonTypeWebView:
			p.Kind = "webview"
			p.URL = x.URL
		case *tg.ButtonTypeSimpleWebView:
			p.Kind = "simple_webview"
			p.URL = x.URL
		}
		out = append(out, p)
	}
	switch v := m.ReplyMarkup.(type) {
	case *tg.ReplyInlineMarkup:
		for i, row := range v.Rows {
			for j, x := range row.Buttons {
				add(i, j, x.Text, x.Type)
			}
		}
	case *tg.ReplyKeyboardMarkup:
		for i, row := range v.Rows {
			for j, x := range row.Buttons {
				add(i, j, x.Text, x.Type)
			}
		}
	}
	return out
}
func click(ctx context.Context, b *bot.Client, p tg.InputPeerClass, m *tg.Message, row, col int) (r pluginapi.Result, err error) {
	for _, v := range buttons(m) {
		if v.Row != row || v.Column != col {
			continue
		}
		switch v.Kind {
		case "callback":
			q := &tg.MessagesGetBotCallbackAnswerRequest{Peer: p, MsgID: m.ID}
			q.SetData(v.Data)
			var a *tg.MessagesBotCallbackAnswer
			a, err = b.API().MessagesGetBotCallbackAnswer(ctx, q)
			if err == nil {
				r.Text = a.Message
				r.URL = a.URL
			}
		case "reply":
			r.MessageID, err = b.SendText(ctx, p, v.Text, bot.SendOptions{})
		case "url":
			r.URL = v.URL
		case "webview", "simple_webview":
			botPeer := p
			if m.ViaBotID != 0 {
				botPeer, err = b.InputPeer(&tg.PeerUser{UserID: m.ViaBotID})
			} else if sender, ok := m.FromID.(*tg.PeerUser); ok {
				botPeer, err = b.InputPeer(sender)
			}
			if err != nil {
				return r, err
			}
			return webviewWithBot(ctx, b, p, botPeer, v.URL, v.Kind == "simple_webview")
		default:
			err = errors.New("unsupported button")
		}
		return
	}
	return r, errors.New("button not found")
}

// resolve accepts public Telegram links without sending a URL as an MTProto username.
func resolve(ctx context.Context, b *bot.Client, target string) (tg.InputPeerClass, error) {
	target = strings.TrimSpace(target)
	raw := target
	if strings.HasPrefix(raw, "t.me/") || strings.HasPrefix(raw, "telegram.me/") {
		raw = "https://" + raw
	}
	if strings.Contains(raw, "://") {
		u, err := url.Parse(raw)
		if err != nil || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") || (u.Host != "t.me" && u.Host != "telegram.me") {
			return nil, errors.New("unsupported Telegram link")
		}
		parts := strings.Split(strings.Trim(u.Path, "/"), "/")
		if len(parts) == 0 || parts[0] == "" || parts[0] == "joinchat" || parts[0] == "c" || strings.HasPrefix(parts[0], "+") {
			return nil, errors.New("public Telegram username link required")
		}
		target = "@" + parts[0]
	}
	return b.ResolveTarget(ctx, target)
}
