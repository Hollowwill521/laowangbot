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
	return fmt.Sprintf("总开关: %t · 监控全部: %t\n本群监听: %t · 本群屏蔽: %t\n\n", s.IsGlobalEnabled, s.MonitorAllGroups, slices.Contains(s.EnabledGroups, chat), slices.Contains(s.ExcludedGroups, chat)) + Help
}

// Help is shared by the plugin command and host help registry.
const Help = `<b>Monitor 命令帮助</b>

以下命令均以 <code>.monitor</code> 开头，仅机主可修改设置。
<code>help</code> 帮助；<code>list</code> / <code>list_groups</code> 配置列表
<code>on</code> / <code>off</code> 添加/移除当前群监听
<code>global on|off</code> 总开关
<code>clean</code> 清空去重记录
<code>send &lt;目标ID或用户名&gt; &lt;文本&gt;</code> 立即代发
<code>sync &lt;目标&gt; &lt;备用群&gt; &lt;文本&gt;</code> 延迟同步；仅机主和可信 Leader 可触发

<b>设置（前缀 .monitor set）</b>
<code>leader &lt;用户ID&gt;|del</code> 可信 Leader
<code>bot_token &lt;Token&gt;|del</code> 通知机器人
<code>bot_id &lt;用户ID&gt;|del</code> 排除该机器人消息
以下设置接 <code>on|off</code>：
<code>monitor_all_groups</code> 监控所有群
<code>dedup</code> 24 小时去重
<code>monitor_admins_messages</code> 管理员消息
<code>monitor_users_messages</code> 普通用户消息
<code>ignore_bot_messages</code>：沿用原版，on 监控机器人，off 忽略

<code>monitor_group add &lt;群ID/用户名/链接&gt;</code>
<code>monitor_group del &lt;序号或ID&gt;</code>
<code>exclude_group add &lt;群ID/用户名/链接&gt;</code>
<code>exclude_group del &lt;序号或ID&gt;</code>
<code>target add &lt;ID或用户名&gt; [备注]</code> 通知目标
<code>target del &lt;序号&gt;</code>
<code>keyword add &lt;关键词&gt;</code> 全局关键词
<code>keyword del &lt;序号或完整关键词&gt;</code>
<code>group_keyword add &lt;群&gt; &lt;关键词&gt;</code>
<code>group_keyword del &lt;群&gt; &lt;序号或完整关键词&gt;</code>
<code>group_keyword clear &lt;群&gt;</code>
<code>group_user add &lt;群&gt; &lt;用户ID&gt;</code>
<code>group_user add &lt;消息链接&gt;</code> 提取发送者
<code>group_user del &lt;群&gt; &lt;序号或用户ID&gt;</code>
<code>group_user clear &lt;群&gt;</code>
群支持 ID、用户名或链接；序号从 1 起，按 list 数组顺序。
普通关键词忽略大小写；正则支持 re:表达式、re:/表达式/flags。
指定人绕过身份过滤；排除群优先。无 Bot Token 时通知没有交互按钮。

<b>示例</b>
<code>.monitor on</code>
<code>.monitor set keyword add 抽奖</code>
<code>.monitor set keyword add re:/抽奖|福利/i</code>
<code>.monitor set target add -100123 通知群</code>
<code>.monitor sync @ExampleBot -100123 /start abc</code>`

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
