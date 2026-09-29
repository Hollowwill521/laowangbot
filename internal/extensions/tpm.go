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
type tpmBatcher interface {
	Batch(context.Context, func(tpmManager) (bool, error)) error
}

type tpmResult struct {
	Text        string
	File        []byte
	FileName    string
	ImportReply bool
}

func tpmHelp(prefix string) string {
	return strings.ReplaceAll(`📦 laowangbot 插件管理器

【已内置，无需安装】
.monitor help
消息监控。
.qdsg help
自动签到。
.bh help
Emby 保号。
.pmc help
私聊验证。

【查看与导出】
.tpm search [关键词]
搜索远程插件目录。
.tpm ls
查看内置和外部插件。
.tpm ls -v
查看详细来源与版本。
.tpm upload 名称
导出已有外部插件源码包。

【程序升级】
.update check
检查新版本。
.update run
升级主程序。只有内置插件时下载官方二进制；已有外部源码插件时仍需源码构建。
TPM 源码安装、更新、卸载、导入和替换暂时禁用，避免服务器本地编译。`, "\n.", "\n"+prefix)
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
			if item.Source == bundledSource {
				source = "内置（随主程序更新）"
			} else if item.Source == plugin.CatalogURL {
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
		return tpmResult{Text: "手动插件已编译安装；重启后生效"}, nil
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
			if progress != nil {
				if err := progress("获取远程插件列表…"); err != nil {
					return tpmResult{}, err
				}
			}
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
				if action == "remove" && v.Source == bundledSource {
					skipped++
					rows = append(rows, "⏭ "+v.Manifest.Name+"：内置插件，无需卸载源码")
					continue
				}
				if action == "update" && v.Source != plugin.CatalogURL {
					skipped++
					label := "手动插件"
					if v.Source == bundledSource {
						label = "内置插件，随主程序更新"
					}
					rows = append(rows, "⏭ "+v.Manifest.Name+"："+label)
					continue
				}
				names = append(names, v.Manifest.Name)
			}
		}
	}
	success, failed := 0, 0
	successRows := []int{}
	batch, supportsBatch := m.(tpmBatcher)
	useBatch := action == "install" && supportsBatch && len(names) > 1
	actionLabel := map[string]string{"install": "下载插件", "update": "更新插件源码", "remove": "移除插件源码"}[action]
	apply := func(m tpmManager) (bool, error) {
		for i, name := range names {
			if e := ctx.Err(); e != nil {
				return false, e
			}
			if progress != nil {
				if e := progress(fmt.Sprintf("%s：%d/%d · %s", actionLabel, i+1, len(names), name)); e != nil {
					return false, e
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
				successRows = append(successRows, len(rows))
				rows = append(rows, "✅ "+name)
			} else if errors.Is(e, plugin.ErrModified) {
				skipped++
				rows = append(rows, "⏭ "+name+"：本地修改，使用 update -f 显式覆盖")
			} else {
				failed++
				rows = append(rows, "❌ "+name+"："+e.Error())
			}
		}
		if err := ctx.Err(); err != nil {
			return false, err
		}
		if useBatch && success > 0 && progress != nil {
			if err := progress(fmt.Sprintf("插件准备完成：%d 项，正在统一编译…", success)); err != nil {
				return false, err
			}
		}
		return success > 0, nil
	}
	if useBatch {
		if err := batch.Batch(ctx, apply); err != nil {
			for _, index := range successRows {
				rows[index] = strings.Replace(rows[index], "✅ ", "❌ ", 1) + "：本批次未生效"
			}
			failed += success
			// Preflight can fail before any item is attempted.
			if success == 0 && failed == 0 {
				failed = len(names)
			}
			success = 0
			rows = append(rows, "统一编译或安装失败："+err.Error())
		}
	} else if _, err := apply(m); err != nil {
		return tpmResult{}, err
	}

	title := fmt.Sprintf("TPM %s：成功 %d · 跳过 %d · 失败 %d", action, success, skipped, failed)
	footer := "已完成成功项的编译安装；重启后生效，手动插件源码会随主程序更新保留"
	if action == "remove" {
		footer = "插件数据保留；重启后停止加载卸载的插件"
	}
	if success == 0 {
		footer = "本次没有变更，无需重启；请处理上述失败或跳过原因后重试"
	}
	return tpmResult{Text: title + "\n" + strings.Join(rows, "\n") + "\n\n" + footer}, nil
}
