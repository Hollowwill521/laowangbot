package hostbridge

import (
	"context"
	"errors"
	"github.com/OrionG-hub/laowangbot/internal/bot"
	"github.com/OrionG-hub/laowangbot/pkg/pluginapi"
	"github.com/gotd/td/telegram/message/unpack"
	"github.com/gotd/td/telegram/uploader"
	"github.com/gotd/td/tg"
	"math/rand/v2"
)

func sendPhoto(ctx context.Context, b *bot.Client, p tg.InputPeerClass, c pluginapi.Call) (r pluginapi.Result, err error) {
	if len(c.Bytes) == 0 || len(c.Bytes) > 512<<10 {
		return r, errors.New("send_photo requires 1..524288 bytes")
	}
	text := c.Text
	var entities []tg.MessageEntityClass
	if c.HTML {
		text, entities, err = bot.ParseHTML(text)
		if err != nil {
			return r, err
		}
	}
	file, err := uploader.NewUploader(b.API()).FromBytes(ctx, "captcha.png", c.Bytes)
	if err != nil {
		return r, err
	}
	q := &tg.MessagesSendMediaRequest{Peer: p, Media: &tg.InputMediaUploadedPhoto{File: file}, Message: text, RandomID: rand.Int64()}
	if len(entities) > 0 {
		q.SetEntities(entities)
	}
	if c.ReplyTo > 0 {
		q.SetReplyTo(&tg.InputReplyToMessage{ReplyToMsgID: c.ReplyTo})
	}
	updates, err := b.API().MessagesSendMedia(ctx, q)
	if err != nil {
		return r, err
	}
	r.MessageID, err = unpack.MessageID(updates, nil)
	if err == nil && r.MessageID <= 0 {
		err = errors.New("send_photo returned no message ID")
	}
	return r, err
}
