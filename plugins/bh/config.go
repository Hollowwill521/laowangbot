package bh

import (
	"errors"
	"fmt"
	"github.com/dlclark/regexp2"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
)

const Help = `Emby 保号管家

【➕ 添加与修改任务】
.bh add [5/6段Cron或now] [Bot] [embyboss/custom] [命令或-] [正则或-] [random 分钟范围] [expire 天数] [备注]
.bh badd [Cron或now] @Bot1 @Bot2 ...
别名 batchadd；默认随机0-15分钟
.bh edit [ID/范围/all] [cron/bot/mode/cmd/regex/random/expire/remark] [值]
别名 set

【💡 可复制示例】
.bh add 0 9 * * * @EmbyTestBot embyboss - - random 5-15 服务
.bh add "0 9 * * *" @EmbyTestBot custom "/start|我的数据" "上次观看.*?(\d{4}-\d{2}-\d{2})" expire 15 服务
.bh edit 1-5 expire 30

【📋 任务管理】
.bh info [ID/all]
详细配置及历史结果
.bh list
所有任务及剩余天数（别名 ls）
.bh rm [ID/范围/机器人名/备注/all]
删除任务
.bh disable [ID]
暂停任务。
.bh enable [ID]
启用任务。

【🚀 立即执行】
.bh now
检测所有启用任务
.bh now [ID]
单独检测，允许检查已暂停任务
原消息显示进度和最终报告；手动也按通知配置通过 Bot 推送。

【⚙️ 通知配置】
.bh config
查看脱敏配置
.bh bottoken [Token]
发信 Bot Token
.bh target [ChatID]
通知接收人
.bh threshold [预警天数] [危险天数]
如 7 3
定时仅异常/临期推送；通知未配置或失败会明确报告，绝不假称发送成功。

【🔄 导入/导出】
.bh tpm ul
导出 baohao_config.json，清空 Token/ChatID
回复 baohao_config.json 后执行：
.bh tpm i
完整校验后导入，永远保留本地 Token/ChatID

【参数说明】
Cron按系统本地时区；now作为添加时间表示每天此刻。
random on/off 或数字/范围（分钟）；expire 0 恢复自动识别。
custom 命令以 | 分隔首条命令和多级内联按钮，自定义正则必须含第一个日期捕获组。
含空格参数用引号包裹；正则反斜杠按原样保留。
`

// splitArgs handles quoted spaces without interpreting regex backslashes.
func splitArgs(s string) ([]string, error) {
	var out []string
	var b strings.Builder
	var quote rune
	in := false
	for _, r := range s {
		if quote != 0 {
			if r == quote {
				quote = 0
			} else {
				b.WriteRune(r)
			}
			in = true
			continue
		}
		if !in && (r == '\'' || r == '"') {
			quote = r
			in = true
			continue
		}
		if unicode.IsSpace(r) {
			if in {
				out = append(out, b.String())
				b.Reset()
				in = false
			}
		} else {
			b.WriteRune(r)
			in = true
		}
	}
	if quote != 0 {
		return nil, errors.New("引号未闭合")
	}
	if in {
		out = append(out, b.String())
	}
	return out, nil
}

var botPattern = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_]{0,63}$`)

func botName(s string) (string, error) {
	s = strings.TrimSpace(s)
	for _, p := range []string{"https://t.me/", "http://t.me/", "t.me/"} {
		s = strings.TrimPrefix(s, p)
	}
	s = strings.TrimPrefix(s, "@")
	s = strings.TrimSuffix(s, "/")
	if !botPattern.MatchString(s) {
		return "", errors.New("Bot 用户名无效")
	}
	return s, nil
}
func compile(s string) (*regexp2.Regexp, error) {
	r, e := regexp2.Compile(s, regexp2.ECMAScript)
	if e != nil {
		return nil, fmt.Errorf("正则无效: %w", e)
	}
	r.MatchTimeout = 100 * time.Millisecond
	return r, nil
}
func validateTask(t Task) error {
	if _, e := parser.Parse(t.Cron); e != nil {
		return fmt.Errorf("Cron 无效: %w", e)
	}
	if _, e := botName(t.Bot); e != nil {
		return e
	}
	if t.Mode != "custom" && t.Mode != "embyboss" {
		return errors.New("模式仅支持 embyboss/custom")
	}
	if t.Mode == "custom" {
		for _, s := range strings.Split(t.Command, "|") {
			if strings.TrimSpace(s) == "" {
				return errors.New("custom 命令及每级按钮不能为空")
			}
		}
		if t.Regex == "" {
			return errors.New("custom 必须提供含日期捕获组的正则")
		}
	}
	if t.Regex != "" {
		r, e := compile(t.Regex)
		if e != nil {
			return e
		}
		if len(r.GetGroupNumbers()) < 2 {
			return errors.New("正则必须包含第一个捕获组")
		}
	}
	if t.ExpireDays < 0 || t.ExpireDays > 365000 {
		return errors.New("expire 需为0至365000的整数")
	}
	lo, hi := randomBounds(t)
	if lo < 0 || hi < lo || hi > 31536000000 {
		return errors.New("random 范围无效或超过365天")
	}
	if t.LastDays < 0 || t.LastDays > 365000 {
		return errors.New("历史到期天数无效")
	}
	return nil
}
func validateDB(d DB) error {
	seq, e := strconv.ParseUint(d.Seq, 10, 63)
	if e != nil {
		return errors.New("seq 必须为非负整数字符串")
	}
	if d.Notify.Warning < 0 || d.Notify.Danger < 0 || d.Notify.Danger > d.Notify.Warning {
		return errors.New("阈值需满足 0 ≤ 危险 ≤ 预警")
	}
	seen := map[string]bool{}
	for _, t := range d.Tasks {
		id, e := strconv.ParseUint(t.ID, 10, 63)
		if e != nil || id == 0 || id > seq || seen[t.ID] {
			return errors.New("任务 ID 必须唯一、为正整数且不大于 seq")
		}
		seen[t.ID] = true
		if e = validateTask(t); e != nil {
			return fmt.Errorf("任务 %s: %w", t.ID, e)
		}
	}
	return nil
}
func randomBounds(t Task) (int64, int64) {
	lo, hi := int64(0), int64(300000)
	if t.RandomMin != nil {
		lo = *t.RandomMin
	}
	if t.RandomMax != nil {
		hi = *t.RandomMax
	}
	return lo, hi
}

var rangePattern = regexp.MustCompile(`^\d+(?:\.\d+)?(?:-\d+(?:\.\d+)?)?$`)

func setRandom(t *Task, v string) error {
	switch strings.ToLower(v) {
	case "off", "false":
		t.Random = false
		t.RandomMin = nil
		t.RandomMax = nil
		return nil
	case "on", "true":
		t.Random = true
		t.RandomMin = nil
		t.RandomMax = nil
		return nil
	}
	if !rangePattern.MatchString(v) {
		return errors.New("random 需为 on/off 或分钟数/范围")
	}
	a := strings.Split(v, "-")
	lo := float64(0)
	hi, e := strconv.ParseFloat(a[0], 64)
	if len(a) == 2 {
		lo = hi
		hi, e = strconv.ParseFloat(a[1], 64)
	}
	if e != nil || math.IsInf(hi, 0) || lo > hi || hi > 525600 {
		return errors.New("random 范围无效")
	}
	l, h := int64(lo*60000), int64(hi*60000)
	t.Random = true
	t.RandomMin = &l
	t.RandomMax = &h
	return nil
}
func cronArgs(a []string) (string, []string, error) {
	if len(a) == 0 {
		return "", nil, errors.New("缺少 Cron")
	}
	if strings.EqualFold(a[0], "now") {
		n := time.Now()
		return fmt.Sprintf("%d %d %d * * *", n.Second(), n.Minute(), n.Hour()), a[1:], nil
	}
	if strings.Contains(a[0], " ") {
		_, e := parser.Parse(a[0])
		return a[0], a[1:], e
	}
	for _, n := range []int{6, 5} {
		if len(a) > n {
			c := strings.Join(a[:n], " ")
			if _, e := parser.Parse(c); e == nil {
				return c, a[n:], nil
			}
		}
	}
	return "", nil, errors.New("需要有效的5或6段 Cron（可加引号）")
}
func integer(s string) (int, error) {
	n, e := strconv.Atoi(s)
	if e != nil || n < 0 {
		return 0, errors.New("需要非负整数")
	}
	return n, nil
}
func resolve(tasks []Task, args []string, names bool) (map[string]bool, []string) {
	ids := map[string]bool{}
	var missing []string
	for _, a := range args {
		if strings.EqualFold(a, "all") {
			for _, t := range tasks {
				ids[t.ID] = true
			}
			continue
		}
		found := false
		if regexp.MustCompile(`^\d+-\d+$`).MatchString(a) {
			v := strings.Split(a, "-")
			lo, e1 := strconv.ParseUint(v[0], 10, 63)
			hi, e2 := strconv.ParseUint(v[1], 10, 63)
			if lo > hi {
				lo, hi = hi, lo
			}
			if e1 == nil && e2 == nil {
				for _, t := range tasks {
					n, _ := strconv.ParseUint(t.ID, 10, 63)
					if n >= lo && n <= hi {
						ids[t.ID] = true
						found = true
					}
				}
			}
		} else {
			key := strings.ToLower(strings.TrimPrefix(a, "@"))
			for _, t := range tasks {
				if t.ID == a || (names && (strings.EqualFold(t.Bot, key) || strings.EqualFold(t.Remark, key))) {
					ids[t.ID] = true
					found = true
				}
			}
			if !found && names {
				for _, t := range tasks {
					if strings.Contains(strings.ToLower(t.Bot), key) || strings.Contains(strings.ToLower(t.Remark), key) {
						ids[t.ID] = true
						found = true
					}
				}
			}
		}
		if !found {
			missing = append(missing, a)
		}
	}
	return ids, missing
}
