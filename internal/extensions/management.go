package extensions

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/OrionG-hub/laowangbot/internal/app"
	"github.com/OrionG-hub/laowangbot/internal/command"
	"github.com/OrionG-hub/laowangbot/internal/plugin"
)

func registerManagement(a *app.App, m plugin.Manager) {
	a.Registry.Register(&command.Command{Name: "tpm", Description: "搜索、安装、更新、卸载和导出插件", Usage: "search|ls|i|update|rm|upload|local|replace", Timeout: 15 * time.Minute, Help: func(p string) string { return command.Escape(tpmHelp(p)) }, Handle: func(ctx context.Context, inv *command.Invocation) error {
		if len(inv.Args) == 0 || strings.EqualFold(inv.Arg(0), "help") || inv.Arg(0) == "h" {
			return inv.EditText(ctx, tpmHelp(inv.Prefix))
		}
		var last time.Time
		r, e := executeTPM(ctx, m, inv.Args, func(text string) error {
			if time.Since(last) < time.Second {
				return nil
			}
			last = time.Now()
			return inv.EditText(ctx, text)
		})
		if e != nil {
			return e
		}
		if r.ImportReply {
			reply, e := inv.Client.GetReply(ctx, inv.Message)
			if e != nil {
				return e
			}
			if reply == nil || reply.Raw == nil {
				return errors.New("请回复适配 laowangbot 的 ZIP 插件包，再发送 tpm i")
			}
			file, e := inv.Client.DownloadMedia(ctx, reply.Raw, plugin.MaxPackageSize+(1<<20))
			if e != nil {
				return e
			}
			name, e := m.ImportPackage(file.Data, false)
			if e != nil {
				return e
			}
			r.Text = "已安装手动插件 " + name + "；重启后生效。旧 TypeScript 代码必须先适配 laowangbot 协议。"
		}
		if r.File != nil {
			peer, e := inv.Client.InputPeer(inv.Message.Peer)
			if e != nil {
				return e
			}
			return inv.Client.SendDocument(ctx, peer, r.FileName, "application/zip", r.File, r.Text, inv.Message.ID)
		}
		for i, page := range command.EscapedPages(r.Text, 3500) {
			var e error
			if i == 0 {
				e = inv.Edit(ctx, page)
			} else {
				e = inv.Reply(ctx, page)
			}
			if e != nil {
				return e
			}
		}
		return nil
	}})
}
