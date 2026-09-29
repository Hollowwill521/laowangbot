package pmcaptcha

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	api "github.com/OrionG-hub/laowangbot/pkg/pluginapi"
	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"
	"html"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math/big"
	"strconv"
	"strings"
	"time"
)

func random(n int) (int, error) {
	v, e := rand.Int(rand.Reader, big.NewInt(int64(n)))
	if e != nil {
		return 0, e
	}
	return int(v.Int64()), nil
}

var questions = [][2]string{{"天空是什么颜色？（中文）", "蓝色"}, {"一周有几天？（数字）", "7"}, {"一年有几个月？（数字）", "12"}, {"猫叫声是？（中文拟声词）", "喵"}, {"水的化学式是？", "h2o"}, {"太阳从哪边升起？（东/西/南/北）", "东"}, {"地球上最大的洋是？（中文）", "太平洋"}, {"1 + 1 等于几？（数字）", "2"}, {"中国的首都是哪个城市？（中文）", "北京"}, {"一天有多少小时？（数字）", "24"}, {"人有几根手指？（数字）", "10"}, {"苹果是什么颜色的？（中文，常见色）", "红色"}, {"冰是什么状态的水？（固/液/气）", "固"}, {"地球围绕什么转？（中文）", "太阳"}, {"键盘上字母共有几个？（数字）", "26"}}

func mathQuestion() (string, string, error) {
	nums := make([]int, 4)
	for i := range nums {
		v, e := random(90)
		if e != nil {
			return "", "", e
		}
		nums[i] = v
	}
	a, b, c := nums[1]+10, nums[2]%9+2, nums[3]%20+1
	switch nums[0] % 6 {
	case 0:
		return fmt.Sprintf("%d + %d", a, b), strconv.Itoa(a + b), nil
	case 1:
		return fmt.Sprintf("%d - %d", a+b, b), strconv.Itoa(a), nil
	case 2:
		return fmt.Sprintf("%d × %d", a, b), strconv.Itoa(a * b), nil
	case 3:
		return fmt.Sprintf("%d ÷ %d", a*b, b), strconv.Itoa(a), nil
	case 4:
		return fmt.Sprintf("%d × %d + %d", a, b, c), strconv.Itoa(a*b + c), nil
	default:
		return fmt.Sprintf("%d²", b), strconv.Itoa(b * b), nil
	}
}
func imageCaptcha(digits bool) ([]byte, string, error) {
	chars := "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	if digits {
		chars = "0123456789"
	}
	answer := make([]byte, 5)
	for i := range answer {
		v, e := random(len(chars))
		if e != nil {
			return nil, "", e
		}
		answer[i] = chars[v]
	}
	img := image.NewRGBA(image.Rect(0, 0, 240, 90))
	draw.Draw(img, img.Bounds(), image.NewUniform(color.RGBA{242, 245, 250, 255}), image.Point{}, draw.Src)
	noise := make([]byte, 1000)
	if _, e := rand.Read(noise); e != nil {
		return nil, "", e
	}
	for i := 0; i < 500; i++ {
		img.Set(int(noise[i*2])%240, int(noise[i*2+1])%90, color.RGBA{130, 160, 190, 255})
	}
	for i, ch := range answer {
		glyph := image.NewRGBA(image.Rect(0, 0, 8, 14))
		d := font.Drawer{Dst: glyph, Src: image.NewUniform(color.RGBA{20, 35, 65, 255}), Face: basicfont.Face7x13, Dot: fixed.P(0, 12)}
		d.DrawString(string(ch))
		x0, y0 := 18+i*43, 20+int(noise[i])%12
		for y := 0; y < 14; y++ {
			for x := 0; x < 8; x++ {
				col := glyph.RGBAAt(x, y)
				if col.A == 0 {
					continue
				}
				for dy := 0; dy < 3; dy++ {
					for dx := 0; dx < 4; dx++ {
						img.Set(x0+x*4+dx+(y-7)/4, y0+y*3+dy, col)
					}
				}
			}
		}
	}
	var out bytes.Buffer
	if e := png.Encode(&out, img); e != nil {
		return nil, "", e
	}
	return out.Bytes(), string(answer), nil
}
func (p *Plugin) prompt(s *state) string {
	custom := html.EscapeString(p.config.Prompt)
	body := ""
	switch s.Mode {
	case "math":
		body = "请回复算式答案：<code>" + html.EscapeString(s.Question) + " = ?</code>"
		if custom != "" {
			body = strings.ReplaceAll(custom, "{question}", html.EscapeString(s.Question))
		}
	case "text":
		if s.QA {
			body = "请回答：" + html.EscapeString(s.Question)
		} else {
			body = "请回复关键词：<code>" + html.EscapeString(s.Question) + "</code>"
			if custom != "" {
				body = strings.ReplaceAll(custom, "{keyword}", html.EscapeString(s.Question))
			}
		}
	default:
		body = "请输入图片中的 5 位验证码（字母不区分大小写）。"
		if custom != "" {
			body = custom
		}
	}
	footer := ""
	if p.config.Timeout > 0 {
		footer += fmt.Sprintf("\n验证时间：%d 秒", p.config.Timeout)
	}
	if p.config.Tries > 0 {
		footer += fmt.Sprintf("\n剩余次数：%d", max(0, p.config.Tries-s.Tries))
	}
	footer += "\n验证失败：归档、静音"
	if len(p.config.Fail) > 0 {
		footer += "、" + html.EscapeString(strings.Join(p.config.Fail, "、"))
	}
	return "🔒 人机验证\n\n" + body + "\n" + footer
}
func (p *Plugin) challenge(ctx context.Context, id int64) error {
	s := &state{Mode: p.config.Mode, Started: time.Now()}
	var img []byte
	var e error
	switch s.Mode {
	case "math":
		s.Question, s.Answer, e = mathQuestion()
	case "text":
		s.Answer = p.config.Keyword
		s.Question = s.Answer
		if p.config.Keyword == "我同意" && p.config.Prompt == "" {
			var n int
			n, e = random(len(questions))
			if e == nil {
				s.Question, s.Answer, s.QA = questions[n][0], questions[n][1], true
			}
		}
	default:
		img, s.Answer, e = imageCaptcha(s.Mode == "img_digit")
	}
	if e != nil {
		return e
	}
	// Store before send: all incoming/outgoing events are serialized by mu.
	p.states[id] = s
	mid, e := p.send(ctx, id, p.prompt(s), img)
	if e != nil {
		delete(p.states, id)
		return e
	}
	s.Messages = []int{mid}
	return nil
}
func (p *Plugin) refresh(ctx context.Context) error {
	var errs []error
	for id, s := range p.states {
		if !p.config.Enabled || !p.config.Captcha {
			errs = append(errs, p.cleanup(ctx, id, s))
			delete(p.states, id)
			continue
		}
		if len(s.Messages) > 0 {
			_, e := p.call(ctx, api.Call{Method: "edit", Target: idText(id), MessageID: s.Messages[0], Text: p.prompt(s), HTML: true})
			errs = append(errs, e)
		}
	}
	return errors.Join(errs...)
}
