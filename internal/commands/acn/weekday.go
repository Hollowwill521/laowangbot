package acn

import (
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/OrionG-hub/laowangbot/internal/command"
	"github.com/OrionG-hub/laowangbot/internal/commands/kit"
)

func weekdayFormat(value string) string {
	switch strings.ToLower(value) {
	case "zh", "中文":
		return "zh"
	case "en", "英文":
		return "en"
	case "both", "中英", "中文英文":
		return "both"
	default:
		return ""
	}
}

func formatWeekday(zone, format string, now time.Time) string {
	location, err := time.LoadLocation(zone)
	if err != nil {
		return ""
	}
	day := now.In(location).Weekday()
	chinese := [...]string{"周日", "周一", "周二", "周三", "周四", "周五", "周六"}[day]
	english := day.String()[:3] + "."
	switch format {
	case "en":
		return english
	case "both":
		return chinese + " " + english
	default:
		return chinese
	}
}

var chineseWeekdayPattern = regexp.MustCompile(`(?i)周[一二三四五六日天](?:\s+(?:Mon|Tue|Wed|Thu|Fri|Sat|Sun)\.)?`)
var englishWeekdayPattern = regexp.MustCompile(`(?i)(?:Mon|Tue|Wed|Thu|Fri|Sat|Sun)\.`)

// stripWeekday 与原插件一样，只清除英文单词外的星期缩写。
func stripWeekday(name string) string {
	name = chineseWeekdayPattern.ReplaceAllString(name, "")
	isLetter := func(b byte) bool { return b >= 'A' && b <= 'Z' || b >= 'a' && b <= 'z' }
	var out strings.Builder
	start := 0
	for _, match := range englishWeekdayPattern.FindAllStringIndex(name, -1) {
		if match[0] > 0 && isLetter(name[match[0]-1]) || match[1] < len(name) && isLetter(name[match[1]]) {
			continue
		}
		out.WriteString(name[start:match[0]])
		start = match[1]
	}
	out.WriteString(name[start:])
	return out.String()
}

func (c *acnCall) weekday() error {
	value := strings.ToLower(c.inv.Arg(1))
	if value == "" {
		return c.inv.Edit(c.ctx, "📅 <b>星期显示</b>\n当前开关: "+command.Code(kit.OnOffText(c.user.ShowWeekday))+"\n当前格式: "+command.Code(kit.OrDefault(c.user.WeekdayFormat, "zh"))+"\n使用 "+command.Code(c.inv.Prefix+"acn weekday on/off")+" 控制显示\n使用 "+command.Code(c.inv.Prefix+"acn weekday zh/en/both")+" 设置格式")
	}
	if value == "on" || value == "off" {
		on := value == "on"
		if _, err := c.change(func(user *acnUser) {
			user.ShowWeekday = on
			if user.DisplayComponents != nil {
				if on && !slices.Contains(user.DisplayComponents, "weekday") {
					user.DisplayComponents = append(user.DisplayComponents, "weekday")
				}
				if !on {
					user.DisplayComponents = slices.DeleteFunc(user.DisplayComponents, func(v string) bool { return v == "weekday" })
				}
			}
		}); err != nil {
			return err
		}
		return c.inv.EditText(c.ctx, "✅ 星期显示已"+map[bool]string{true: "开启", false: "关闭"}[on])
	}
	if value == "format" {
		value = c.inv.Arg(2)
	}
	format := weekdayFormat(value)
	if format == "" {
		return c.inv.EditText(c.ctx, "请使用 "+c.inv.Prefix+"acn weekday on/off 或 zh/en/both（也支持 weekday format zh/en/both）")
	}
	if _, err := c.change(func(user *acnUser) { user.WeekdayFormat = format }); err != nil {
		return err
	}
	return c.inv.Edit(c.ctx, "✅ 星期格式已更新为: "+command.Code(format))
}
