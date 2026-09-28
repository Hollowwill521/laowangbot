package yvlu

import (
	"context"
	"encoding/base64"
	"github.com/OrionG-hub/laowangbot/internal/bot"
	"github.com/OrionG-hub/laowangbot/internal/command"
	"github.com/OrionG-hub/laowangbot/internal/commands/kit"
	"github.com/OrionG-hub/laowangbot/internal/media"
	"github.com/gotd/td/tg"
	"os"
	"strings"
)

func (s *yvluService) embedSticker(ctx context.Context, inv *command.Invocation, message *bot.Message, doc *tg.Document, item *quoteMessage) error {
	if doc.Size > 8<<20 {
		return kit.Fail("贴纸超过 8 MiB，无法生成语录")
	}
	file, e := inv.Client.DownloadMedia(ctx, message.Raw, 8<<20)
	if e != nil {
		return kit.Failf("贴纸下载失败：%v", e)
	}
	mime := strings.ToLower(strings.TrimSpace(doc.MimeType))
	data := file.Data
	switch mime {
	case "video/webm", "image/webp", "image/png", "image/jpeg":
	case "application/x-tgsticker":
		dir, e := os.MkdirTemp("", "laowangbot-yvlu-tgs-")
		if e != nil {
			return e
		}
		defer os.RemoveAll(dir)
		data, e = media.TGSWebM(ctx, dir, data)
		if e != nil {
			return kit.Failf("TGS 动画转换失败：%v。需要 ffmpeg，以及安装 rlottie-python、Pillow 的 Python（LAOWANGBOT_PYTHON）。", e)
		}
		mime = "video/webm"
	default:
		return kit.Failf("暂不支持该贴纸格式：%s", mime)
	}
	item.Media = &quotePhot{URL: "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(data)}
	return nil
}
