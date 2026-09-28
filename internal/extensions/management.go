package extensions

import (
	"context"
	"fmt"
	"github.com/OrionG-hub/laowangbot/internal/app"
	"github.com/OrionG-hub/laowangbot/internal/command"
	"github.com/OrionG-hub/laowangbot/internal/plugin"
	"strings"
)

func registerManagement(a *app.App, m plugin.Manager) {
	a.Registry.Register(&command.Command{Name: "tpm", Description: "管理 laowangbot 独立进程插件", Usage: "list|install 名称|update 名称|local 路径|replace 路径", Handle: func(ctx context.Context, inv *command.Invocation) error {
		var err error
		switch inv.Arg(0) {
		case "", "list", "ls":
			items, e := m.List()
			if e != nil {
				return e
			}
			var rows []string
			for _, v := range items {
				rows = append(rows, fmt.Sprintf("%s %s (%s)", v.Name, v.Version, strings.Join(v.Commands, ", ")))
			}
			if len(rows) == 0 {
				rows = append(rows, "未安装外部插件；内置命令通过 update 更新")
			}
			return inv.EditText(ctx, strings.Join(rows, "\n"))
		case "install":
			err = m.InstallRemote(ctx, inv.Arg(1))
		case "update":
			err = m.UpdateRemote(ctx, inv.Arg(1))
		case "local":
			err = m.InstallLocal(inv.Rest(1))
		case "replace":
			err = m.ReplaceLocal(inv.Rest(1))
		default:
			return inv.EditText(ctx, "用法：tpm list|install 名称|update 名称|local 路径|replace 路径")
		}
		if err != nil {
			return err
		}
		return inv.EditText(ctx, "插件已安装。执行 restart 后生效；旧 TypeScript 插件需先按 laowangbot 协议适配。")
	}})
}
