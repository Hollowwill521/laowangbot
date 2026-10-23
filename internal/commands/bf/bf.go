// Package bf 实现 .bf：把配置打包发到收藏夹，重装后用它恢复。打包和恢复本身在 internal/backup。
package bf

import (
	"bytes"
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/gotd/td/tg"

	"github.com/OrionG-hub/laowangbot/internal/app"
	"github.com/OrionG-hub/laowangbot/internal/backup"
	"github.com/OrionG-hub/laowangbot/internal/brand"
	"github.com/OrionG-hub/laowangbot/internal/command"
	"github.com/OrionG-hub/laowangbot/internal/commands/kit"
)

const maxBackupSize = 32 << 20

// captionFileLimit 让文件列表不超出 Telegram 说明文字的 1024 字符上限；
// 再多的话，给出总数就够了。
const captionFileLimit = 12

func backupCaption(version string, names []string, size int) string {
	listed := names
	more := ""
	if len(listed) > captionFileLimit {
		listed, more = listed[:captionFileLimit], fmt.Sprintf(" 等 %d 个", len(names))
	}
	quoted := make([]string, len(listed))
	for index, name := range listed {
		quoted[index] = command.Escape(name)
	}
	return "📦 <b>" + command.Escape(brand.Get()) + " 配置备份</b> · " + command.Escape(version) + "\n" +
		strconv.Itoa(len(names)) + " 个文件，" + command.Escape(kit.FormatBytes(size)) + "：" +
		strings.Join(quoted, "、") + more + "\n\n" +
		"这个文件只包含插件配置，不包含登录会话。\n\n" +
		"<b>恢复</b>\n回复这条文件消息发送 <code>.hf</code>，只会恢复插件配置，登录状态保持不变。"
}

func backupHelp(prefix string) string {
	p := command.Escape(prefix)
	return "📦 <b>配置备份</b>\n\n把各命令和插件的配置打包，发到本账号的收藏夹。\n\n" +
		"• <code>" + p + "bf</code> 打包并发到收藏夹\n\n" +
		"<b>包含</b>\ndata/ 下每个命令的配置和 state/ 下各插件的配置、白名单及记录（含待合并的旧配置）。\n\n" +
		"<b>不包含</b>\n登录会话、.env、缓存和插件源码。\n\n" +
		"<b>恢复</b>\n在收藏夹回复备份文件发送 <code>" + p + "hf</code>，恢复后服务会自动重启。\n\n" +
		"⚠️ 无论在哪个对话里执行，备份都只发到收藏夹，不会出现在当前对话。" +
		"只允许在收藏夹恢复。"
}

// Register 注册 .bf。
func Register(a *app.App) {
	handle := func(ctx context.Context, inv *command.Invocation) error {
		switch strings.ToLower(inv.Arg(0)) {
		case "help", "h":
			return inv.Edit(ctx, backupHelp(inv.Prefix))
		}
		if err := inv.Edit(ctx, "📦 正在打包配置…"); err != nil {
			return err
		}
		archive, names, err := backup.Create(a.Root, a.Version, time.Now())
		if err != nil {
			return inv.EditText(ctx, "❌ 备份失败："+err.Error())
		}
		// 不管命令是在哪个对话里发的，一律发到收藏夹。这个压缩包就等于
		// 账号本身；只因为有人碰巧在群里敲了 .bf 就把它发进群，等于把
		// 账号交给了群里所有人。
		name := "laowangbot-backup-" + time.Now().Format("20060102-1504") + ".tar.gz"
		if err := inv.Client.SendDocument(ctx, &tg.InputPeerSelf{}, name, "application/gzip", archive,
			backupCaption(a.Version, names, len(archive)), 0); err != nil {
			return err
		}
		inv.Log.Info("backup.sent", "files", len(names), "bytes", len(archive))
		// 在收藏夹以外的对话里，确认消息只说文件去了哪，不提里面有什么。
		return inv.Edit(ctx, "✅ 备份已发到收藏夹（"+strconv.Itoa(len(names))+" 个文件）")
	}
	a.Registry.Register(&command.Command{
		Name: "bf", Description: "备份配置到收藏夹，重装后可恢复",
		Help: backupHelp, Timeout: 2 * time.Minute, Handle: handle,
	})
	a.Registry.Register(&command.Command{
		Name: "hf", Description: "回复备份文件恢复插件配置", Help: func(prefix string) string {
			return "♻️ <b>恢复插件配置</b>\n\n在收藏夹回复 <code>" + command.Escape(prefix) + "hf</code> 到 <code>" + command.Escape(prefix) + "bf</code> 发送的备份文件。只恢复插件配置，不覆盖登录会话。"
		}, Timeout: 2 * time.Minute, Handle: func(ctx context.Context, inv *command.Invocation) error {
			if !inv.Message.Saved || inv.Trigger != nil {
				return inv.EditText(ctx, "❌ 只能在收藏夹中回复备份文件使用 .hf")
			}
			reply, err := inv.Client.GetReply(ctx, inv.Message)
			if err != nil {
				return err
			}
			if reply == nil || reply.Raw == nil {
				return inv.EditText(ctx, "请回复一条 .bf 发送的备份文件使用 .hf")
			}
			file, err := inv.Client.DownloadMedia(ctx, reply.Raw, maxBackupSize)
			if err != nil {
				return inv.EditText(ctx, "❌ 无法下载备份文件："+command.Brief(err))
			}
			if file == nil || file.FileName == "" {
				return inv.EditText(ctx, "请回复一条 .bf 发送的备份文件使用 .hf")
			}
			if err := inv.EditText(ctx, "♻️ 正在恢复插件配置…"); err != nil {
				return err
			}
			names, err := backup.RestorePlugins(bytes.NewReader(file.Data), a.Root)
			if err != nil {
				return inv.EditText(ctx, "❌ 恢复失败："+command.Brief(err))
			}
			inv.Log.Info("backup.restored", "files", len(names))
			if err := inv.Edit(ctx, "✅ 已恢复 "+strconv.Itoa(len(names))+" 个插件配置，服务即将重启"); err != nil {
				return err
			}
			go func() {
				time.Sleep(500 * time.Millisecond)
				a.RequestRestart()
			}()
			return nil
		},
	})
}
