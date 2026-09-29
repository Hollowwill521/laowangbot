package extensions

import (
	"context"
	"errors"
	"github.com/OrionG-hub/laowangbot/internal/commands/restart"
	"github.com/OrionG-hub/laowangbot/internal/sourceplugin"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/OrionG-hub/laowangbot/internal/app"
	"github.com/OrionG-hub/laowangbot/internal/command"
	"github.com/OrionG-hub/laowangbot/internal/plugin"
)

func registerManagement(a *app.App, m plugin.Manager) {
	a.Registry.Register(&command.Command{Name: "tpm", Description: "查看内置和外部插件；源码修改暂时禁用", Usage: "search|ls|upload", Timeout: 30 * time.Minute, Help: func(p string) string {
		return command.PlainHelp(tpmHelp(p), p, "tpm", "monitor", "qdsg", "bh", "pmcaptcha", "update")
	}, Handle: func(ctx context.Context, inv *command.Invocation) error {
		if len(inv.Args) == 0 || strings.EqualFold(inv.Arg(0), "help") || inv.Arg(0) == "h" {
			return inv.Edit(ctx, command.PlainHelp(tpmHelp(inv.Prefix), inv.Prefix, "tpm", "monitor", "qdsg", "bh", "pmcaptcha", "update"))
		}
		if tpmMutationDisabled(inv.Args) {
			return inv.EditText(ctx, "TPM 源码安装、更新、卸载、导入和替换暂时禁用，避免服务器本地编译。monitor、qdsg、bh、pmcaptcha 已内置，请直接使用 .monitor / .qdsg / .bh / .pmcaptcha；升级主程序请用 .update run。")
		}
		binary, err := os.Executable()
		if err != nil {
			return err
		}
		binary, err = filepath.EvalSymlinks(binary)
		if err != nil {
			return err
		}
		progress := newTPMProgress(ctx, inv.EditText)
		defer progress.Stop()
		builder := sourceplugin.Builder{Root: a.Root, Binary: binary, Version: a.Version, Source: a.Env.Get("LAOWANGBOT_SOURCE", ""), Repo: a.Env.Get("MIBOT_UPDATE_REPO", "OrionG-hub/laowangbot"), Progress: progress.Stage}
		compiledManager := &sourceManager{Manager: m, ctx: ctx, apply: builder.Apply}
		r, e := executeTPM(ctx, managedTPM{compiledManager, a.Registry}, inv.Args, progress.Item)
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
			name, e := compiledManager.ImportPackage(file.Data, false)
			if e != nil {
				return e
			}
			r.Text = "手动 Go 源码插件 " + name + " 已编译进主程序。"
		}
		progress.Stop()
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
		if compiledManager.changed {
			if restart.Available() {
				return restart.Now(ctx, inv, "tpm", "Go 源码插件已编译并安装，正在重启…", "编译成功，但重启失败；请手动重启服务。")
			}
			return inv.Reply(ctx, "Go 源码插件已编译并安装；请手动重启服务后生效。")
		}
		return nil
	}})
}

func tpmMutationDisabled(args []string) bool {
	if len(args) == 0 {
		return false
	}
	switch strings.ToLower(args[0]) {
	case "i", "install", "update", "updateall", "ua", "rm", "remove", "uninstall", "un", "local", "replace":
		return true
	}
	return false
}
