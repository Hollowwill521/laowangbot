package monitor

import (
	"fmt"
	"html"
	"slices"
	"strconv"
	"strings"
)

func (m *Monitor) help(chat string) string {
	s := m.state.Settings
	return fmt.Sprintf("<b>当前状态</b>\n总开关：%s · 全群监控：%s\n本群监听：%s · 本群排除：%s\n\n", stateLabel(s.IsGlobalEnabled), stateLabel(s.MonitorAllGroups), stateLabel(slices.Contains(s.EnabledGroups, chat)), stateLabel(slices.Contains(s.ExcludedGroups, chat))) + Help
}

// Help is shared by the plugin command and host help registry.
const Help = `<b>Monitor · 消息监控帮助</b>
点击等宽命令复制整行（包含前缀）。尖括号是需替换的参数；设置仅机主可改。

<b>① 快速开始</b>
<code>.monitor on</code>
在当前群开启监听；还需总开关开启、配置关键词与通知目标。
<code>.monitor set keyword add 抽奖</code>
添加一个全局关键词示例。
<code>.monitor set target add -100123 通知群</code>
添加通知目标；将 -100123 换成真实群 ID。
<code>.monitor list</code>
查看配置、编号及每项对应的操作命令。
<code>.monitor help</code>
查看本帮助。

<b>② 开关与监听范围</b>
<code>.monitor global on</code>
开启总开关。
<code>.monitor global off</code>
关闭总开关，保留配置。
<code>.monitor off</code>
停止监听当前群。
<code>.monitor set monitor_all_groups on</code>
监听所有群；排除群仍优先。
<code>.monitor set monitor_all_groups off</code>
仅监听已添加的群。
<code>.monitor set monitor_group add &lt;群ID/用户名/链接&gt;</code>
添加指定监听群。
<code>.monitor set monitor_group del &lt;序号或ID&gt;</code>
移除监听群。
<code>.monitor set exclude_group add &lt;群ID/用户名/链接&gt;</code>
排除指定群。
<code>.monitor set exclude_group del &lt;序号或ID&gt;</code>
解除排除。

<b>③ 关键词与指定用户</b>
<code>.monitor set keyword add re:/抽奖|福利/i</code>
正则示例；普通关键词忽略大小写，正则支持 re:表达式 或 re:/表达式/flags。
<code>.monitor set keyword del &lt;序号或完整关键词&gt;</code>
删除全局关键词。
<code>.monitor set group_keyword add &lt;群&gt; &lt;关键词&gt;</code>
为单个群添加关键词。
<code>.monitor set group_keyword del &lt;群&gt; &lt;序号或完整关键词&gt;</code>
删除该群的一个关键词。
<code>.monitor set group_keyword clear &lt;群&gt;</code>
清空该群关键词。
<code>.monitor set group_user add &lt;群&gt; &lt;用户ID&gt;</code>
关注该群指定人；指定人绕过身份过滤，排除群仍优先。
<code>.monitor set group_user add &lt;消息链接&gt;</code>
从消息链接提取发送者。
<code>.monitor set group_user del &lt;群&gt; &lt;序号或用户ID&gt;</code>
移除一个关注用户。
<code>.monitor set group_user clear &lt;群&gt;</code>
清空该群关注用户。

<b>④ 身份过滤与去重</b>
<code>.monitor set monitor_admins_messages on</code>
监控管理员消息。
<code>.monitor set monitor_admins_messages off</code>
不监控管理员消息。
<code>.monitor set monitor_users_messages on</code>
监控普通用户消息。
<code>.monitor set monitor_users_messages off</code>
不监控普通用户消息。
<code>.monitor set ignore_bot_messages on</code>
沿用旧版字段语义：on 表示监控机器人。
<code>.monitor set ignore_bot_messages off</code>
off 表示忽略机器人。
<code>.monitor set dedup on</code>
开启 24 小时去重。
<code>.monitor set dedup off</code>
关闭去重。
<code>.monitor clean</code>
清空现有去重记录。

<b>⑤ 通知与权限</b>
<code>.monitor set target del &lt;序号&gt;</code>
删除通知目标；序号从 1 起，以配置列表为准。
<code>.monitor set bot_token &lt;Token&gt;</code>
设置通知 Bot，仅在收藏夹执行；未设置时使用账号通知，无交互按钮。
<code>.monitor set bot_token del</code>
移除通知 Bot Token。
<code>.monitor set bot_id &lt;用户ID&gt;</code>
排除该通知机器人自身消息。
<code>.monitor set bot_id del</code>
清除通知机器人 ID。
<code>.monitor set leader &lt;用户ID&gt;</code>
设置可信 Leader，仅授权触发同步，不授权修改配置。
<code>.monitor set leader del</code>
撤销 Leader。

<b>⑥ 代发与同步</b>
<code>.monitor send &lt;目标ID或用户名&gt; &lt;文本&gt;</code>
立即向目标发送文本。
<code>.monitor sync &lt;目标&gt; &lt;备用群&gt; &lt;文本&gt;</code>
延迟同步；目标不可发送时尝试备用群，仅机主或可信 Leader 可触发。
<code>.monitor sync @ExampleBot -100123 /start abc</code>
同步示例：替换目标及备用群后使用。
群参数支持 ID、用户名或链接；删除前先查看配置列表确认序号。`

func validateCommand(a []string) error {
	bad := func() error { return fmt.Errorf("参数无效，请使用 .monitor help 查看命令用法") }
	toggle := func(v string) bool { return v == "on" || v == "off" }
	switch arg(a, 0) {
	case "", "help", "list", "list_groups", "clean", "on", "off":
		if len(a) > 1 {
			return bad()
		}
	case "global":
		if len(a) != 2 || !toggle(arg(a, 1)) {
			return bad()
		}
	case "send":
		if len(a) < 3 || rest(a, 2) == "" {
			return bad()
		}
	case "sync":
		if len(a) < 4 || rest(a, 3) == "" {
			return bad()
		}
	case "set":
		v := arg(a, 2)
		switch arg(a, 1) {
		case "leader", "bot_id", "bot_token":
			if len(a) != 3 || v == "" {
				return bad()
			}
			if arg(a, 1) != "bot_token" && !off(v) {
				id, err := strconv.ParseInt(v, 10, 64)
				if err != nil || id <= 0 {
					return bad()
				}
			}
		case "monitor_all_groups", "dedup", "ignore_bot_messages", "monitor_admins_messages", "monitor_users_messages":
			if len(a) != 3 || !toggle(v) {
				return bad()
			}
		case "keyword", "monitor_group", "exclude_group", "target":
			if (v != "add" && v != "del") || len(a) < 4 || rest(a, 3) == "" {
				return bad()
			}
			if arg(a, 1) != "keyword" && v == "del" && len(a) != 4 {
				return bad()
			}
		case "group_keyword", "group_user":
			if v == "clear" {
				if len(a) != 4 {
					return bad()
				}
			} else if v == "add" || v == "del" {
				linkAdd := arg(a, 1) == "group_user" && v == "add" && len(a) == 4 && messageLink.MatchString(a[3])
				if !linkAdd && (len(a) < 5 || rest(a, 4) == "") {
					return bad()
				}
			} else {
				return bad()
			}
		default:
			return bad()
		}
	default:
		return bad()
	}
	return nil
}

// Bound serialized HTML too, preserving complete entities and Unicode characters.
func escapedChunks(text string, limit int) []string {
	var result []string
	var part strings.Builder
	for _, r := range text {
		escaped := html.EscapeString(string(r))
		if part.Len()+len(escaped) > limit {
			result = append(result, part.String())
			part.Reset()
		}
		part.WriteString(escaped)
	}
	if part.Len() > 0 {
		result = append(result, part.String())
	}
	return result
}
func shortText(s string, limit int) string {
	r := []rune(s)
	if len(r) > limit {
		return string(r[:limit]) + "…"
	}
	return s
}

func canRemove(list []string, value string) bool {
	i, err := strconv.Atoi(value)
	return (err == nil && i > 0 && i <= len(list)) || slices.Contains(list, value)
}
