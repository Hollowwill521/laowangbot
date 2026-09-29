package pmcaptcha

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	api "github.com/OrionG-hub/laowangbot/pkg/pluginapi"
	"slices"
	"strconv"
	"strings"
	"time"
)

const Help = "PMCaptcha：pmc / pmcaptcha 同入口。默认关闭。\npmc on/off/status；pmc captcha on/off/math/text/img_digit/img_mixed\npmc h basic/captcha/set/wl/record 查看分节帮助。\npmc folders / fixfolder；pmc set folder <ID>\n启用插件后，陌生私聊默认归档并静音；captcha on 才发送验证码。"

func HelpText(prefix, section string) string {
	sections := map[string]string{
		"basic":   "pmc on/off：启停，保留验证码配置；关闭会取消待验证。\npmc status：完整配置和统计。\npmc folders：分组与自动规则；pmc fixfolder：清除通过分组联系人/非联系人自动规则。",
		"captcha": "pmc captcha on/off\npmc captcha [mode] math/text/img_digit/img_mixed\nmath：四则/幂；text：默认随机问答，自定义 keyword 后回复关键词。\n图片为 5 位，兼容原版允许一个字符编辑距离；不在文本暴露答案。",
		"set":     "pmc set time <秒> / tries <次数>：0 不限\npmc set keyword <文本> / prompt <文本>（空清除）\n提示占位符 {question}/{keyword}，按纯文本显示。\npmc set fail block/delete/report/mute/archive/none（支持屏蔽/删除/举报/静音/归档/无）\npmc set pass unmute/unarchive/wl/add_folder/none（支持取消静音/取消归档/白名单/加入分组/无）\n动作可空格多选，失败始终静音归档；delete 双方撤回。私聊不支持 kick/ban。\npmc set folder <ID>：0 不启用、1 归档、>=2 自定义\npmc set initiative on/off\npmc set initiative-failed on/off（默认 off）\npmc set history <N>：<=0 禁用\npmc set groups <N>：-1 禁用，0 全部满足\npmc set wl-words/bl-words <词…>/none\npmc set premium allow/ban/only/none\n规则顺序：主动会话；历史；共同群；白词优先于黑词；Premium。待验证永不因主动发消息通过。",
		"wl":      "pmc wl / whitelist：列表\npmc add/del/pass <ID/@user>（支持回复）\npmc wl add/del/pass <ID/@user>（支持回复）\npmc wl del all；pmc wl cleanbots\npass 手动通过并加入白名单。del 同时删除通过记录。",
		"record":  "pmc record [verified/failed]\npmc record del verified/failed <ID>/all\n列表不重复显示已在白名单/已通过用户；记录不包含验证码答案。",
	}
	if v, ok := sections[section]; ok {
		return strings.ReplaceAll(v, "pmc ", prefix+"pmc ")
	}
	parts := []string{Help}
	for _, key := range []string{"basic", "captcha", "set", "wl", "record"} {
		parts = append(parts, "【"+key+"】\n"+sections[key])
	}
	parts = append(parts, "示例：\npmc on\npmc captcha on\npmc captcha img_digit\npmc set pass unmute unarchive\npmc set fail block report\npmc set folder 2\npmc set pass unmute unarchive add_folder")
	return strings.ReplaceAll(strings.Join(parts, "\n\n"), "pmc ", prefix+"pmc ")
}
func (p *Plugin) command(ctx context.Context, a []string, ev api.Event) (string, error) {
	if len(a) == 0 {
		return HelpText(".", ""), nil
	}
	cmd := strings.ToLower(a[0])
	arg := func(i int) string {
		if i < len(a) {
			return a[i]
		}
		return ""
	}
	switch cmd {
	case "h", "help", "?":
		return HelpText(".", arg(1)), nil
	case "status":
		c := p.config
		return fmt.Sprintf("PMCaptcha 5.1.2 Merged (Go)\n\n基础：插件=%t，验证码=%t，模式=%s\n超时=%d 秒，最大次数=%d（0 不限）\n关键词=%s\n自定义提示=%s\n\n失败动作：归档、静音；附加=%s\n通过动作=%s，通过分组=%d\n\n自动规则（按顺序）：\n主动会话=%t，失败后主动放行=%t\n历史阈值=%d（<=0 禁用）\n共同群阈值=%d（-1 禁用、0 全部满足）\n白词=%s\n黑词=%s\nPremium=%s\n\n白名单=%d，通过记录=%d，失败记录=%d，待验证=%d", c.Enabled, c.Captcha, c.Mode, c.Timeout, c.Tries, c.Keyword, c.Prompt, strings.Join(c.Fail, "、"), strings.Join(c.Pass, "、"), c.Folder, c.Initiative, c.InitiativeFailed, c.History, c.Groups, strings.Join(c.WLWords, "、"), strings.Join(c.BLWords, "、"), c.Premium, len(p.data.Whitelist), len(p.data.Verified), len(p.data.Failed), len(p.states)), nil
	case "on", "off":
		c := p.config
		c.Enabled = cmd == "on"
		if e := p.saveConfig(c); e != nil {
			return "", e
		}
		if e := p.refresh(ctx); e != nil {
			return "", fmt.Errorf("配置已保存，待验证更新失败: %w", e)
		}
		return "插件已" + map[bool]string{true: "启用", false: "禁用"}[c.Enabled] + "，验证码配置保留。", nil
	case "captcha":
		sub := strings.ToLower(arg(1))
		if sub == "mode" {
			sub = strings.ToLower(arg(2))
		}
		if sub == "" {
			return fmt.Sprintf("验证码开启=%t，模式=%s\n%s", p.config.Captcha, p.config.Mode, HelpText(".", "captcha")), nil
		}
		c := p.config
		if sub == "on" || sub == "off" {
			c.Captcha = sub == "on"
		} else {
			c.Mode = sub
		}
		if e := p.saveConfig(c); e != nil {
			return "", e
		}
		if e := p.refresh(ctx); e != nil {
			return "", fmt.Errorf("配置已保存，待验证更新失败: %w", e)
		}
		return "验证码配置已保存（已发送题目保留原答案）。", nil
	case "set":
		return p.set(ctx, a[1:])
	case "folders":
		r, e := p.call(ctx, api.Call{Method: "folders"})
		if e != nil {
			return "", e
		}
		b, _ := json.MarshalIndent(r.Folders, "", "  ")
		return "Telegram 分组（ID 1 为系统归档）：\n" + string(b), nil
	case "fixfolder":
		if p.config.Folder <= 1 {
			return "", errors.New("请先 set folder 设置 >=2 的自定义分组")
		}
		if e := p.action(ctx, 0, "folder_normalize"); e != nil {
			return "", e
		}
		p.normalized = true
		return "已清除通过分组的联系人/非联系人自动规则，其余规则和成员保留。", nil
	case "wl", "whitelist":
		return p.whitelist(ctx, a[1:], ev)
	case "add", "del", "pass", "cleanbots":
		return p.whitelist(ctx, a, ev)
	case "record", "records":
		return p.records(ctx, a[1:])
	default:
		return "", fmt.Errorf("未知命令 %s；使用 pmc h", cmd)
	}
}
func (p *Plugin) set(ctx context.Context, a []string) (string, error) {
	if len(a) == 0 {
		return HelpText(".", "set"), nil
	}
	key := strings.ReplaceAll(strings.ToLower(a[0]), "_", "-")
	v := strings.Join(a[1:], " ")
	c := p.config
	switch key {
	case "time", "timeout", "tries", "folder", "history", "groups":
		n, e := strconv.Atoi(v)
		if e != nil {
			return "", errors.New("参数须为整数")
		}
		switch key {
		case "time", "timeout":
			c.Timeout = n
		case "tries":
			c.Tries = n
		case "folder":
			c.Folder = n
		case "history":
			c.History = n
		case "groups":
			c.Groups = n
		}
	case "keyword":
		c.Keyword = v
	case "prompt":
		c.Prompt = v
	case "initiative", "initiative-failed":
		if v != "on" && v != "off" {
			return "", errors.New("请使用 on/off")
		}
		if key == "initiative" {
			c.Initiative = v == "on"
		} else {
			c.InitiativeFailed = v == "on"
		}
	case "premium":
		c.Premium = strings.ToLower(v)
	case "wl-words", "bl-words":
		if v == "" {
			if key == "wl-words" {
				return strings.Join(c.WLWords, " "), nil
			}
			return strings.Join(c.BLWords, " "), nil
		}
		words := slices.Clone(a[1:])
		if contains(words, "none") {
			words = []string{}
		}
		if key == "wl-words" {
			c.WLWords = words
		} else {
			c.BLWords = words
		}
	case "fail", "pass":
		if v == "" {
			return HelpText(".", "set"), nil
		}
		aliases := map[string]string{"屏蔽": "block", "删除": "delete", "举报": "report", "静音": "mute", "归档": "archive", "取消静音": "unmute", "取消归档": "unarchive", "白名单": "wl", "加入分组": "add_folder", "分组": "add_folder", "无": "none"}
		var actions []string
		for _, s := range a[1:] {
			s = strings.ToLower(s)
			if x, ok := aliases[s]; ok {
				s = x
			}
			if s == "none" {
				actions = []string{}
				break
			}
			if s == "kick" || s == "ban" || s == "踢出" || s == "封禁" {
				return "", errors.New("kick/ban 在私聊无效，请使用 block")
			}
			if !contains(actions, s) {
				actions = append(actions, s)
			}
		}
		if key == "fail" {
			c.Fail = actions
		} else {
			c.Pass = actions
		}
	case "ext-timeout", "exttimeout":
		return "", errors.New("ext-timeout 已移除，请使用 set time")
	default:
		return "", fmt.Errorf("未知或已移除参数 %s；使用 pmc h set", key)
	}
	if e := p.saveConfig(c); e != nil {
		return "", e
	}
	p.normalized = false
	if e := p.refresh(ctx); e != nil {
		return "", fmt.Errorf("配置已保存，待验证更新失败: %w", e)
	}
	return "配置已保存；待验证提示已刷新。", nil
}
func (p *Plugin) target(ctx context.Context, arg string, ev api.Event) (int64, error) {
	if ev.ReplyToID > 0 {
		r, e := p.call(ctx, api.Call{Method: "messages", Target: ev.ChatID, IDs: []int{ev.ReplyToID}})
		if e != nil {
			return 0, e
		}
		if len(r.Messages) > 0 {
			arg = r.Messages[0].SenderID
		}
	}
	id, e := strconv.ParseInt(arg, 10, 64)
	if e != nil {
		r, err := p.call(ctx, api.Call{Method: "resolve", Target: arg})
		if err != nil {
			return 0, err
		}
		if r.Entity == nil {
			return 0, errors.New("无法解析用户")
		}
		id, e = strconv.ParseInt(r.Entity.ID, 10, 64)
	}
	if e != nil || id <= 0 {
		return 0, errors.New("请提供有效用户 ID/@用户名或回复消息")
	}
	return id, nil
}
func (p *Plugin) whitelist(ctx context.Context, a []string, ev api.Event) (string, error) {
	if len(a) == 0 {
		var rows []string
		for _, id := range p.data.Whitelist {
			u, e := p.info(ctx, id)
			if e != nil {
				rows = append(rows, idText(id)+"（信息读取失败）")
				continue
			}
			if !u.Entity.Bot {
				rows = append(rows, fmt.Sprintf("%s (%d)", u.Entity.Name, id))
			}
		}
		return "白名单：\n" + strings.Join(rows, "\n"), nil
	}
	sub := a[0]
	arg := ""
	if len(a) > 1 {
		arg = a[1]
	}
	d := cloneData(p.data)
	if sub == "cleanbots" {
		kept := []int64{}
		n := 0
		for _, id := range d.Whitelist {
			u, e := p.info(ctx, id)
			if e != nil {
				return "", fmt.Errorf("清理未保存，读取 %d 失败: %w", id, e)
			}
			if u.Entity.Bot {
				n++
			} else {
				kept = append(kept, id)
			}
		}
		d.Whitelist = kept
		if e := p.saveData(d); e != nil {
			return "", e
		}
		return fmt.Sprintf("已移除 %d 个机器人", n), nil
	}
	if sub == "del" && arg == "all" {
		d.Whitelist = []int64{}
		if e := p.saveData(d); e != nil {
			return "", e
		}
		return "白名单已清空", nil
	}
	if sub != "add" && sub != "del" && sub != "pass" {
		return "", errors.New("使用 wl add/del/pass/cleanbots")
	}
	id, e := p.target(ctx, arg, ev)
	if e != nil {
		return "", e
	}
	if sub == "del" {
		d.Whitelist = slices.DeleteFunc(d.Whitelist, func(v int64) bool { return v == id })
		d.Verified = remove(d.Verified, id)
	} else {
		u, e := p.info(ctx, id)
		if e != nil {
			return "", e
		}
		if u.Entity.Bot || u.Entity.Self || id == 777000 {
			return "", errors.New("不能添加机器人、自身或官方服务号")
		}
		if !slices.Contains(d.Whitelist, id) {
			d.Whitelist = append(d.Whitelist, id)
		}
		d.Verified = remove(d.Verified, id)
		if sub == "pass" {
			d.Failed = remove(d.Failed, id)
			d.Verified = upsert(d.Verified, Record{ID: id, Name: u.Entity.Name, Username: u.Entity.Username, Time: time.Now().UTC().Format(time.RFC3339)})
		}
	}
	if e = p.saveData(d); e != nil {
		return "", e
	}
	if sub == "pass" || sub == "add" {
		s := p.states[id]
		delete(p.states, id)
		if e = p.cleanup(ctx, id, s); e != nil {
			return "", fmt.Errorf("白名单已保存，但清理验证消息失败: %w", e)
		}
	}
	return "白名单已更新", nil
}
func (p *Plugin) records(ctx context.Context, a []string) (string, error) {
	if len(a) == 0 {
		return fmt.Sprintf("通过 %d 人，失败 %d 人", len(p.data.Verified), len(p.data.Failed)), nil
	}
	d := cloneData(p.data)
	kind := a[0]
	if kind == "del" {
		if len(a) != 3 || (a[1] != "verified" && a[1] != "failed") {
			return "", errors.New("用法 record del verified/failed <ID>/all")
		}
		var rs []Record
		if a[1] == "verified" {
			rs = d.Verified
		} else {
			rs = d.Failed
		}
		if a[2] == "all" {
			rs = []Record{}
		} else {
			id, e := strconv.ParseInt(a[2], 10, 64)
			if e != nil || id <= 0 {
				return "", errors.New("无效用户 ID")
			}
			rs = remove(rs, id)
		}
		if a[1] == "verified" {
			d.Verified = rs
		} else {
			d.Failed = rs
		}
		if e := p.saveData(d); e != nil {
			return "", e
		}
		return "记录已删除", nil
	}
	var rs []Record
	switch kind {
	case "verified":
		rs = d.Verified
	case "failed":
		rs = d.Failed
	default:
		return "", errors.New("使用 record verified/failed")
	}
	var rows []string
	for _, r := range rs {
		if slices.Contains(d.Whitelist, r.ID) || (kind == "failed" && has(d.Verified, r.ID)) {
			continue
		}
		if u, e := p.info(ctx, r.ID); e == nil {
			if u.Entity.Bot {
				continue
			}
			r.Name = u.Entity.Name
		}
		rows = append(rows, fmt.Sprintf("%s (%d) @%s %s %s", r.Name, r.ID, r.Username, r.Time, r.Reason))
	}
	return kind + "：\n" + strings.Join(rows, "\n"), nil
}
