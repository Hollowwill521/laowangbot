package qdsg

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	api "github.com/OrionG-hub/laowangbot/pkg/pluginapi"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

//go:embed ocr_worker.py
var ocrScript string

func (p *Plugin) request(ctx context.Context, method, endpoint, key string, body any, out any) error {
	var b []byte
	if body != nil {
		var e error
		b, e = json.Marshal(body)
		if e != nil {
			return e
		}
	}
	req, e := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(b))
	if e != nil {
		return e
	}
	req.Header.Set("Content-Type", "application/json")
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	resp, e := p.HTTP.Do(req)
	if e != nil {
		return fmt.Errorf("HTTP 请求失败: %s", safeHTTPError(e))
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	if out == nil {
		_, e = io.Copy(io.Discard, io.LimitReader(resp.Body, 2<<20))
		return e
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(out)
}
func safeHTTPError(e error) string {
	if u, ok := e.(*url.Error); ok {
		return u.Err.Error()
	}
	return e.Error()
}
func (p *Plugin) localOCR(ctx context.Context, img []byte) (string, error) {
	py := os.Getenv("QDSG_PYTHON")
	if py == "" {
		py = filepath.Join(p.dir, ".venv", "bin", "python")
		if _, e := os.Stat(py); e != nil {
			py = "python3"
		}
	}
	script := filepath.Join(p.dir, "ocr_worker.py")
	if e := os.WriteFile(script, []byte(ocrScript), 0600); e != nil {
		return "", e
	}
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, py, script)
	cmd.Stdin = strings.NewReader(base64.StdEncoding.EncodeToString(img))
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = io.Discard
	e := cmd.Run()
	var r struct {
		Success bool
		Result  string
		Error   string
	}
	if json.Unmarshal(out.Bytes(), &r) != nil {
		return "", fmt.Errorf("OCR 无可用输出 (%v)，请设置 QDSG_PYTHON 或在插件 .venv 安装 ddddocr opencv-python-headless numpy", e)
	}
	if !r.Success {
		return "", fmt.Errorf("OCR: %s；请在插件专用 Python 环境安装 ddddocr opencv-python-headless numpy", r.Error)
	}
	return r.Result, nil
}
func (p *Plugin) recognize(ctx context.Context, t Task, m api.Message, buttons []api.Button) (string, error) {
	c := p.snapshot().AI
	provider := t.Provider
	if provider == "" {
		provider = c.Default
	}
	if provider == "" {
		provider = "local"
	}
	prompt := t.Prompt
	if prompt == "" {
		prompt = c.Prompt
	}
	text := m.Text
	var img []byte
	mime := "image/jpeg"
	if m.HasMedia && t.AITarget != "text" {
		r, e := p.host.Call(ctx, api.Call{Method: "download", Target: t.Bot, MessageID: m.ID})
		if e != nil {
			return "", e
		}
		img = r.Bytes
		if r.MimeType != "" {
			mime = r.MimeType
		}
	}
	if t.AITarget == "image" {
		text = ""
	} else if t.AITarget != "text" && provider != "local" && (strings.Contains(text, "░") || strings.Contains(text, "补全诗句") || strings.Contains(text, "验证")) {
		img = nil
	}
	if len(img) == 0 && text == "" {
		return "", errors.New("无可识别图片或文本")
	}
	if provider == "local" {
		if len(img) == 0 {
			return "", errors.New("本地 OCR 只支持图片")
		}
		return p.localOCR(ctx, img)
	}
	opts := []string{}
	for _, b := range buttons {
		opts = append(opts, b.Text)
	}
	prompt += "\n文本内容：" + text + "\n可选按钮：" + strings.Join(opts, ", ")
	key, base, model := c.OpenAIKey, c.OpenAIBase, c.OpenAIModel
	gemini := provider == "gemini"
	if gemini {
		key, base, model = c.GeminiKey, c.GeminiBase, c.GeminiModel
	} else if provider != "openai" {
		found := false
		for _, v := range c.Custom {
			if v.ID == provider {
				key, base, model = v.Key, v.Base, v.Model
				found = true
				break
			}
		}
		if !found {
			return "", errors.New("未知 AI 提供商，请先 aiconfig addcustom")
		}
	}
	if key == "" {
		return "", errors.New("AI API Key 未配置")
	}
	base = strings.TrimRight(base, "/")
	var response map[string]json.RawMessage
	if gemini {
		parts := []any{map[string]any{"text": prompt}}
		if len(img) > 0 {
			parts = append(parts, map[string]any{"inlineData": map[string]string{"mimeType": mime, "data": base64.StdEncoding.EncodeToString(img)}})
		}
		body := map[string]any{"contents": []any{map[string]any{"role": "user", "parts": parts}}}
		if e := p.request(ctx, "POST", base+"/v1beta/models/"+url.PathEscape(model)+":generateContent?key="+url.QueryEscape(key), "", body, &response); e != nil {
			return "", e
		}
		var cs []struct {
			Content struct{ Parts []struct{ Text string } }
		}
		_ = json.Unmarshal(response["candidates"], &cs)
		if len(cs) > 0 && len(cs[0].Content.Parts) > 0 && cs[0].Content.Parts[0].Text != "" {
			return strings.TrimSpace(cs[0].Content.Parts[0].Text), nil
		}
	} else {
		var content any = prompt
		if len(img) > 0 {
			content = []any{map[string]any{"type": "text", "text": prompt}, map[string]any{"type": "image_url", "image_url": map[string]string{"url": "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(img)}}}
		}
		if !strings.HasSuffix(base, "/v1") && !strings.Contains(base, "/api/v3") {
			base += "/v1"
		}
		body := map[string]any{"model": model, "messages": []any{map[string]any{"role": "user", "content": content}}, "max_tokens": 300}
		if e := p.request(ctx, "POST", base+"/chat/completions", key, body, &response); e != nil {
			return "", e
		}
		var cs []struct{ Message struct{ Content string } }
		_ = json.Unmarshal(response["choices"], &cs)
		if len(cs) > 0 && cs[0].Message.Content != "" {
			return strings.TrimSpace(cs[0].Message.Content), nil
		}
	}
	return "", errors.New("AI 返回内容为空")
}
