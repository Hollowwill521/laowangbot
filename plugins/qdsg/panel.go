package qdsg

import (
	"fmt"
	"strings"
	"time"
)

func shown(s string) string {
	if s == "" {
		return "未设置"
	}
	return s
}
func enabled(v bool) string {
	if v {
		return "开启"
	}
	return "关闭"
}
func (p *Plugin) taskPanel() string {
	lines := []string{fmt.Sprintf("【QDSG 任务列表 · %d 项】", len(p.db.Tasks)), "时间按服务器本地时区显示；立即执行不会改变定时计划。"}
	if len(p.db.Tasks) == 0 {
		lines = append(lines, "暂无签到任务。先查看创建示例：", ".qdsg help")
	}
	for _, t := range p.db.Tasks {
		state := "已启用"
		action := "disable"
		label := "暂停此任务"
		if t.Disabled {
			state = "已暂停"
			action = "enable"
			label = "启用此任务"
		}
		random := "关闭"
		if t.Random {
			random = fmt.Sprintf("%g–%g 分钟", float64(t.RandomMin)/60000, float64(t.RandomMax)/60000)
		}
		next := "暂无计划"
		if t.Disabled {
			next = "已暂停"
		} else if n := p.next[t.ID]; !n.IsZero() {
			next = n.Format(time.RFC3339)
		}
		lines = append(lines, "", fmt.Sprintf("【任务 %s · %s】", t.ID, state), "机器人：@"+t.Bot, "备注："+shown(t.Remark), "模式："+t.Mode, "计划说明："+shown(t.Display), "Cron（秒 分 时 日 月 星期）："+t.Cron, "发送内容："+shown(t.Command), "后续步骤："+shown(t.Secondary), fmt.Sprintf("操作等待：%d 毫秒；随机延迟：%s", t.Wait, random), fmt.Sprintf("AI：%s；服务：%s；输入类型：%s", enabled(t.AI), shown(t.Provider), shown(t.AITarget)), fmt.Sprintf("识别步数：%d；失败重试：%d 次；间隔：%g 分钟", t.Steps, t.Retry, t.Interval), "提示词："+shown(t.Prompt), "下次执行："+next, "上次执行："+shown(t.LastRun), "上次结果："+shown(t.LastResult), "上次错误："+shown(t.LastError), "立即执行：", ".qdsg now "+t.ID, label+"：", ".qdsg "+action+" "+t.ID, "修改备注（替换最后一个参数）：", ".qdsg edit "+t.ID+" remark 新备注", "删除任务（不可撤销）：", ".qdsg rm "+t.ID)
	}
	return strings.Join(lines, "\n")
}
func (p *Plugin) aiPanel() string {
	v := p.db.AI
	key := func(s string) string {
		if s == "" {
			return "未设置"
		}
		return "已设置（隐藏）"
	}
	lines := []string{"【QDSG AI 配置】", "默认服务：" + shown(v.Default), "默认提示词：" + shown(v.Prompt), "", "【OpenAI】", "地址：" + shown(v.OpenAIBase), "模型：" + shown(v.OpenAIModel), "密钥：" + key(v.OpenAIKey), "", "【Gemini】", "地址：" + shown(v.GeminiBase), "模型：" + shown(v.GeminiModel), "密钥：" + key(v.GeminiKey)}
	for _, c := range v.Custom {
		lines = append(lines, "", "【自定义服务 · "+c.ID+"】", "地址："+c.Base, "模型："+c.Model, "密钥："+key(c.Key))
	}
	lines = append(lines, "", "【通知与外援】", "结果通知："+enabled(p.db.Notify.Enabled), "通知接收人："+shown(p.db.Notify.Target), "CF Bucket："+shown(p.db.Bucket), "", "【修改与测试】", "切换默认服务（替换服务名）：", ".qdsg aiconfig set provider openai", "设置模型（替换模型名）：", ".qdsg aiconfig set openai_model 模型名", "设置密钥（仅在收藏夹中执行）：", ".qdsg aiconfig set openai_key 你的密钥", "回复验证码测试本地 OCR：", ".qdsg test local", "查看全部配置命令：", ".qdsg help")
	return strings.Join(lines, "\n")
}
