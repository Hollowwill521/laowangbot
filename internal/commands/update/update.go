// Package update installs verified releases while preserving compiled plugins.
package update

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/OrionG-hub/laowangbot/internal/sourceplugin"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/OrionG-hub/laowangbot/internal/app"
	"github.com/OrionG-hub/laowangbot/internal/command"
	"github.com/OrionG-hub/laowangbot/internal/commands/kit"
	"github.com/OrionG-hub/laowangbot/internal/commands/restart"
	"github.com/OrionG-hub/laowangbot/internal/httpx"
)

type release struct {
	TagName string `json:"tag_name"`
	Body    string `json:"body"`
	Assets  []struct {
		Name string `json:"name"`
		URL  string `json:"browser_download_url"`
		Size int64  `json:"size"`
	} `json:"assets"`
}

// Register 注册 .update。
func Register(a *app.App) {
	repo := a.Env.Get("MIBOT_UPDATE_REPO", "OrionG-hub/laowangbot")
	a.Registry.Register(&command.Command{Name: "update", Description: "检查版本，无插件下载更新，有插件保留源码编译", Usage: "[check|run|rollback]", Timeout: 30 * time.Minute,
		Help: func(prefix string) string {
			return "<b>程序更新</b>\n" + command.Code(prefix+"update") + " 当前版本与回滚状态\n" + command.Code(prefix+"update check") + " 读取 GitHub Releases 检查新版本\n" +
				command.Code(prefix+"update run") + " 无源码插件时下载校验官方二进制；有源码插件时限流编译、检查、替换并重启\n" + command.Code(prefix+"update rollback") + " 恢复上次构建的程序和插件源码并重启（状态数据不回退）\n发布仓库由 " + command.Code("MIBOT_UPDATE_REPO") + " 指定。\n无源码插件时只需下载权限；源码插件更新需要 Go（满足新版 go.mod）和 Git，以及源码/依赖下载网络；不依赖 Node.js。手动插件源码与版本原样保留，远程插件也不会随主程序更新自动升级。编译或检查失败保留当前程序。插件更新请用 tpm update；手动插件用 tpm replace。首次可用 LAOWANGBOT_SOURCE 指定本地源码进行插件编译。Windows 暂不支持源码自编译替换，请外部构建后停止服务手动替换。"
		},
		Handle: func(ctx context.Context, inv *command.Invocation) error {
			binary, err := os.Executable()
			if err == nil {
				binary, _ = filepath.EvalSymlinks(binary)
			}
			// 下载、替换程序文件、回滚都只能一个一个来：两个同时跑会互相覆盖下载的文件，
			// 或者把留作回滚的上一版本弄丢。
			if sub := strings.ToLower(inv.Arg(0)); sub == "run" || sub == "apply" || sub == "rollback" {
				if !busy.TryLock() {
					return inv.EditText(ctx, "已有一个更新或回滚正在进行，请稍候")
				}
				defer busy.Unlock()
			}
			switch strings.ToLower(inv.Arg(0)) {
			case "", "ver", "status":
				rows := []string{"<b>更新状态</b>", "当前版本：" + command.Code(kit.Version(a)), "程序文件：" + command.Code(binary), "发布仓库：" + command.Code(repo)}
				if info, err := os.Stat(filepath.Join(a.Root, ".compiled", "previous", "binary")); err == nil && info.Mode().IsRegular() && info.Size() > 0 {
					rows = append(rows, "可回滚到上一版本："+command.Code(inv.Prefix+"update rollback"))
				} else {
					rows = append(rows, "暂无可回滚的上一版本")
				}
				return inv.Edit(ctx, strings.Join(rows, "\n"))
			case "check":
				if err := inv.Edit(ctx, "<b>laowangbot 更新</b>\n正在读取发布信息…"); err != nil {
					return err
				}
				latest, err := fetchRelease(ctx, repo)
				if err != nil {
					return inv.Edit(ctx, "<b>更新检查失败</b>\n"+command.Escape(httpx.Reason(err)))
				}
				if !newer(a.Version, latest.TagName) {
					return inv.Edit(ctx, "<b>已是最新版本</b>\n当前："+command.Code(kit.Version(a))+"\n最新："+command.Code(latest.TagName))
				}
				notes := strings.TrimSpace(latest.Body)
				if len(notes) > 800 {
					notes = notes[:800] + "…"
				}
				text := "<b>发现新版本</b>\n当前：" + command.Code(kit.Version(a)) + "\n最新：" + command.Code(latest.TagName) + "\n执行 " + command.Code(inv.Prefix+"update run") + " 安装。"
				if notes != "" {
					text += "\n\n<blockquote expandable>" + command.Escape(notes) + "</blockquote>"
				}
				return inv.Edit(ctx, text)
			case "run", "apply":
				if runtime.GOOS == "windows" {
					return inv.EditText(ctx, "Windows 暂不支持运行中的源码编译替换；请在构建机带上插件源码编译，停止服务后手动替换，勿用官方二进制覆盖本地插件。")
				}
				return runUpdate(ctx, a, inv, repo, binary)
			case "rollback":
				if runtime.GOOS == "windows" {
					return inv.EditText(ctx, "Windows 请停止服务后手动恢复程序与对应插件源码备份。")
				}
				if !restart.Available() {
					return inv.EditText(ctx, "重启组件不可用")
				}
				builder := sourceplugin.Builder{Root: a.Root, Binary: binary, Version: a.Version, Repo: repo}
				if err := builder.Rollback(ctx); err != nil {
					return err
				}
				return restart.Now(ctx, inv, "rollback", "<b>laowangbot 回滚</b>\n已换回上一版本，正在重启…", "回滚后重启失败。")
			}
			return inv.EditText(ctx, "用法："+inv.Prefix+"update [check|run|rollback]")
		}})
}

func fetchRelease(ctx context.Context, repo string) (*release, error) {
	response, err := httpx.Do(ctx, httpx.Request{URL: "https://api.github.com/repos/" + repo + "/releases/latest",
		Headers: map[string]string{"Accept": "application/vnd.github+json"}, Timeout: 20 * time.Second, MaxBytes: 1 << 20})
	if err != nil {
		return nil, err
	}
	if !response.OK() {
		return nil, &httpx.StatusError{Status: response.Status}
	}
	var parsed release
	if err := json.Unmarshal(response.Body, &parsed); err != nil {
		return nil, err
	}
	if parsed.TagName == "" {
		return nil, errors.New("release has no tag")
	}
	return &parsed, nil
}

func normalizeVersion(value string) string {
	return strings.TrimPrefix(strings.TrimSpace(value), "v")
}

func newer(current, latest string) bool {
	current, latest = normalizeVersion(current), normalizeVersion(latest)
	if current == "" || current == "dev" {
		return true
	}
	return current != latest
}

// busy 让更新和回滚同一时间只有一个在跑。
var busy sync.Mutex

func runUpdate(ctx context.Context, a *app.App, inv *command.Invocation, repo, binary string) error {
	if !restart.Available() {
		return inv.EditText(ctx, "重启组件不可用")
	}
	if err := inv.EditText(ctx, "正在读取发布信息…"); err != nil {
		return err
	}
	latest, err := fetchRelease(ctx, repo)
	if err != nil {
		return err
	}
	if !newer(a.Version, latest.TagName) {
		return inv.EditText(ctx, "已是最新版本："+kit.Version(a))
	}
	sourceRequired, err := sourceplugin.NeedsSourceBuild(a.Root)
	if err != nil {
		return err
	}
	if !sourceRequired {
		if err = inv.EditText(ctx, "正在下载并校验 "+latest.TagName+" 官方二进制…"); err != nil {
			return err
		}
		if err = replaceOfficialBinary(ctx, a.Root, binary, latest); err != nil {
			return inv.EditText(ctx, buildFailure(err))
		}
		return restart.Now(ctx, inv, "update", "<b>laowangbot 更新</b>\n已校验并安装 "+command.Escape(latest.TagName)+" 官方二进制，正在重启…", "更新后重启失败，请手动重启服务。")
	}
	if err = inv.EditText(ctx, "正在获取 "+latest.TagName+" 源码，保留已安装插件并限流编译主程序；首次编译可能较慢…"); err != nil {
		return err
	}
	builder := sourceplugin.Builder{Root: a.Root, Binary: binary, Version: a.Version, Repo: repo}
	if err = builder.Update(ctx, latest.TagName); err != nil {
		return inv.EditText(ctx, buildFailure(err))
	}
	return restart.Now(ctx, inv, "update", "<b>laowangbot 更新</b>\n已编译安装 "+command.Escape(latest.TagName)+"，本地插件源码已保留，正在重启…", "更新后重启失败，请手动重启服务。")
}

func buildFailure(err error) string {
	detail := []rune(err.Error())
	if len(detail) > 1800 {
		detail = append(detail[:1800], []rune("…（诊断已截断）")...)
	}
	return "程序更新失败：" + string(detail) + "\n请检查下载、校验或编译诊断；若提示恢复失败，请先保留 .compiled/pending 并修复磁盘问题。"
}
