package extensions

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/OrionG-hub/laowangbot/internal/plugin"
)

type tpmManager interface {
	Installed() ([]plugin.InstalledInfo, error)
	Search(context.Context, string) ([]plugin.CatalogEntry, error)
	InstallRemote(context.Context, string) error
	UpdateRemoteForce(context.Context, string, bool) error
	Remove(string) error
	InstallLocal(string) error
	ReplaceLocal(string) error
	Export(string) ([]byte, error)
	ImportPackage([]byte, bool) (string, error)
}
type tpmResult struct {
	Text        string
	File        []byte
	FileName    string
	ImportReply bool
}

func tpmHelp(prefix string) string {
	return "📦 laowangbot 插件管理器\n\n" + prefix + "tpm search/s [关键词]：搜索远程插件\n" + prefix + "tpm ls/list [-v] 或 lv：已安装插件及来源\n" + prefix + "tpm i/install 名称…|all：单个、批量或全部安装\n" + prefix + "tpm i：回复 ZIP 插件包安装\n" + prefix + "tpm update/ua [名称…] [-f]：更新远程插件，省略名称更新全部\n" + prefix + "tpm rm/remove/uninstall/un 名称…|all：卸载代码，保留数据\n" + prefix + "tpm upload/ul 名称：导出 ZIP 插件包（不含状态）\n" + prefix + "tpm local 路径 / replace 路径：手动安装 / 替换\n\n远程源固定为当前项目；手动插件不参与远程更新。修改过的远程插件默认跳过，-f 才覆盖。安装、更新和卸载在重启后生效。"
}
func executeTPM(ctx context.Context, m tpmManager, args []string, progress func(string) error) (tpmResult, error) {
	if len(args) == 0 {
		return tpmResult{Text: tpmHelp(".")}, nil
	}
	action := strings.ToLower(args[0])
	rest := args[1:]
	switch action {
	case "help", "h":
		return tpmResult{Text: tpmHelp(".")}, nil
	case "search", "s":
		items, e := m.Search(ctx, strings.Join(rest, " "))
		if e != nil {
			return tpmResult{}, e
		}
		rows := []string{fmt.Sprintf("📦 远程插件（%d）", len(items))}
		for _, item := range items {
			rows = append(rows, fmt.Sprintf("%s %s · %s", item.Manifest.Name, item.Manifest.Version, item.Manifest.Description))
		}
		if len(items) == 0 {
			rows = append(rows, "没有匹配的插件")
		}
		rows = append(rows, "来源："+plugin.CatalogURL)
		return tpmResult{Text: strings.Join(rows, "\n")}, nil
	case "ls", "list", "lv":
		verbose := action == "lv"
		for _, v := range rest {
			if v != "-v" && v != "--verbose" {
				return tpmResult{}, errors.New("用法：tpm ls [-v]")
			}
			verbose = true
		}
		items, e := m.Installed()
		if e != nil {
			return tpmResult{}, e
		}
		rows := []string{fmt.Sprintf("📦 已安装插件（%d）", len(items))}
		for _, item := range items {
			source := "手动"
			if item.Source == plugin.CatalogURL {
				source = "远程"
			}
			changed := ""
			if item.Modified {
				changed = " · 已修改"
			}
			row := fmt.Sprintf("%s %s · %s%s", item.Manifest.Name, item.Manifest.Version, source, changed)
			if item.Error != "" {
				row += "\n  ⚠ " + item.Error
			}
			if verbose {
				row += "\n  命令：" + strings.Join(item.Manifest.Commands, ", ") + "\n  来源：" + item.Source
				if !item.UpdateTime.IsZero() {
					row += "\n  安装/更新时间：" + item.UpdateTime.Format(time.RFC3339)
				}
				if item.Manifest.Description != "" {
					row += "\n  " + item.Manifest.Description
				}
			}
			rows = append(rows, row)
		}
		if len(items) == 0 {
			rows = append(rows, "未安装外部插件；内置命令通过 update 更新")
		}
		return tpmResult{Text: strings.Join(rows, "\n")}, nil
	case "local", "replace":
		if len(rest) == 0 {
			return tpmResult{}, errors.New("请提供插件目录路径")
		}
		path := strings.Join(rest, " ")
		var e error
		if action == "local" {
			e = m.InstallLocal(path)
		} else {
			e = m.ReplaceLocal(path)
		}
		if e != nil {
			return tpmResult{}, e
		}
		return tpmResult{Text: "手动插件已安装；重启后生效"}, nil
	case "upload", "ul":
		if len(rest) != 1 {
			return tpmResult{}, errors.New("用法：tpm upload 名称")
		}
		data, e := m.Export(rest[0])
		if e != nil {
			return tpmResult{}, e
		}
		return tpmResult{File: data, FileName: rest[0] + ".zip", Text: "插件代码包（不含插件状态及远程更新凭据）；导入后按手动插件维护"}, nil
	case "i", "install":
		action = "install"
	case "update", "updateall", "ua":
		action = "update"
	case "rm", "remove", "uninstall", "un":
		action = "remove"
	default:
		return tpmResult{}, errors.New("未知 TPM 子命令；发送 tpm help 查看用法（不支持自定义源）")
	}
	force := false
	names := []string{}
	seen := map[string]bool{}
	for _, name := range rest {
		if action == "update" && (name == "-f" || name == "--force") {
			force = true
			continue
		}
		if strings.HasPrefix(name, "-") {
			return tpmResult{}, fmt.Errorf("未知参数 %s", name)
		}
		if !seen[name] {
			names = append(names, name)
			seen[name] = true
		}
	}
	if seen["all"] && len(names) > 1 {
		return tpmResult{}, errors.New("all 不能和插件名称混用")
	}
	if action == "install" && len(names) == 0 {
		return tpmResult{ImportReply: true}, nil
	}
	if action == "remove" && len(names) == 0 {
		return tpmResult{}, errors.New("用法：tpm rm 名称…|all")
	}
	skipped := 0
	rows := []string{}
	all := seen["all"] || (action == "update" && len(names) == 0)
	if all {
		names = nil
		if action == "install" {
			items, e := m.Search(ctx, "")
			if e != nil {
				return tpmResult{}, e
			}
			installed, e := m.Installed()
			if e != nil {
				return tpmResult{}, e
			}
			existing := map[string]bool{}
			for _, v := range installed {
				existing[v.Manifest.Name] = true
			}
			for _, v := range items {
				if existing[v.Manifest.Name] {
					skipped++
					rows = append(rows, "⏭ "+v.Manifest.Name+"：已安装")
					continue
				}
				names = append(names, v.Manifest.Name)
			}
		} else {
			items, e := m.Installed()
			if e != nil {
				return tpmResult{}, e
			}
			for _, v := range items {
				if action == "update" && v.Source != plugin.CatalogURL {
					skipped++
					rows = append(rows, "⏭ "+v.Manifest.Name+"：手动插件")
					continue
				}
				names = append(names, v.Manifest.Name)
			}
		}
	}
	success, failed := 0, 0
	for i, name := range names {
		if e := ctx.Err(); e != nil {
			return tpmResult{}, e
		}
		if progress != nil {
			if e := progress(fmt.Sprintf("%s：%d/%d · %s", action, i+1, len(names), name)); e != nil {
				return tpmResult{}, e
			}
		}
		var e error
		switch action {
		case "install":
			e = m.InstallRemote(ctx, name)
		case "update":
			e = m.UpdateRemoteForce(ctx, name, force)
		case "remove":
			e = m.Remove(name)
		}
		if e == nil {
			success++
			rows = append(rows, "✅ "+name)
		} else if errors.Is(e, plugin.ErrModified) {
			skipped++
			rows = append(rows, "⏭ "+name+"：本地修改，使用 update -f 显式覆盖")
		} else {
			failed++
			rows = append(rows, "❌ "+name+"："+e.Error())
		}
	}
	title := fmt.Sprintf("TPM %s：成功 %d · 跳过 %d · 失败 %d", action, success, skipped, failed)
	footer := "重启后生效；手动插件由用户自行适配"
	if action == "remove" {
		footer = "插件数据保留；重启后停止加载卸载的插件"
	}
	return tpmResult{Text: title + "\n" + strings.Join(rows, "\n") + "\n\n" + footer}, nil
}
