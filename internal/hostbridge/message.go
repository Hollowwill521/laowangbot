package hostbridge

import (
	"github.com/OrionG-hub/laowangbot/internal/bot"
	"github.com/OrionG-hub/laowangbot/pkg/pluginapi"
	"github.com/gotd/td/tg"
	"strconv"
	"unicode/utf16"
)

func serialize(m *tg.Message) pluginapi.Message {
	r := pluginapi.Message{ID: m.ID, ChatID: bot.PeerID(m.PeerID), SenderID: bot.PeerID(m.FromID), Text: m.Message, Date: m.Date, Out: m.Out, Edited: m.EditDate != 0}
	if reply, ok := m.ReplyTo.(*tg.MessageReplyHeader); ok {
		r.ReplyToID = reply.ReplyToMsgID
	}
	if m.Media != nil {
		_, empty := m.Media.(*tg.MessageMediaEmpty)
		r.HasMedia = !empty
	}
	if doc, ok := m.Media.(*tg.MessageMediaDocument); ok {
		if d, ok := doc.Document.(*tg.Document); ok {
			r.MimeType = d.MimeType
		}
	}
	if f, ok := m.GetFwdFrom(); ok {
		r.ForwardMessageID = f.ChannelPost
		r.ForwardDate = f.Date
		switch p := f.FromID.(type) {
		case *tg.PeerChannel:
			r.ForwardID = "channel:" + strconv.FormatInt(p.ChannelID, 10)
		case *tg.PeerChat:
			r.ForwardID = "chat:" + strconv.FormatInt(p.ChatID, 10)
		case *tg.PeerUser:
			r.ForwardID = "user:" + strconv.FormatInt(p.UserID, 10)
		}
	}
	text := utf16.Encode([]rune(m.Message))
	for _, v := range m.Entities {
		switch e := v.(type) {
		case *tg.MessageEntityTextURL:
			r.URLs = append(r.URLs, e.URL)
		case *tg.MessageEntityURL:
			if e.Offset >= 0 && e.Length >= 0 && e.Offset <= len(text) && e.Length <= len(text)-e.Offset {
				r.URLs = append(r.URLs, string(utf16.Decode(text[e.Offset:e.Offset+e.Length])))
			}
		}
	}
	r.Buttons = buttons(m)
	return r
}

func serializeFor(b *bot.Client, m *tg.Message) pluginapi.Message {
	r := serialize(m)
	if env, ok := bot.Envelope(m, b.SelfID(), false, b.Peers()); ok {
		r.SenderID = bot.PeerID(env.Sender)
		r.Out = env.Out
	}
	return r
}
