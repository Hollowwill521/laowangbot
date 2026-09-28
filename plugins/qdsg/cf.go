package qdsg

import (
	"context"
	"crypto/md5"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	api "github.com/OrionG-hub/laowangbot/pkg/pluginapi"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

func safeJob(s string) string {
	clean := regexp.MustCompile("[^A-Za-z0-9_-]").ReplaceAllString(s, "")
	if len(clean) > 32 {
		clean = clean[:32]
	}
	if len(clean) >= 6 {
		return clean
	}
	h := md5.Sum([]byte(s))
	return hex.EncodeToString(h[:])[:16]
}
func statePayload(s string) map[string]any {
	out := map[string]any{}
	parts := strings.Split(s, ".")
	for _, part := range parts[:min(2, len(parts))] {
		b, e := base64.RawURLEncoding.DecodeString(strings.TrimRight(part, "="))
		if e == nil {
			var v map[string]any
			if json.Unmarshal(b, &v) == nil {
				if _, ok := v["nonce"]; ok {
					return v
				}
				if _, ok := v["purpose"]; ok {
					return v
				}
				out = v
			}
		}
	}
	return out
}
func (p *Plugin) solveCF(ctx context.Context, t Task, raw, button string, proof bool, c *cursor, budget time.Duration) (string, error) {
	u, e := url.Parse(raw)
	if e != nil || u.Scheme != "https" && u.Scheme != "http" {
		return "", errors.New("无效验证 URL")
	}
	state := u.Query().Get("state")
	if proof && state == "" {
		return "", errors.New("验证链接缺少 state")
	}
	payload := statePayload(state)
	purpose, _ := payload["purpose"].(string)
	if purpose == "" {
		purpose = "checkin"
	}
	seed, _ := payload["nonce"].(string)
	if seed == "" {
		seed = state
	}
	if seed == "" {
		seed = raw
	}
	id := safeJob(seed)
	expires, _ := payload["expires_at"].(float64)
	if expires > 0 {
		remaining := time.Until(time.Unix(int64(expires), 0).Add(-15 * time.Second))
		if remaining <= 0 {
			return "", errors.New("验证链接已过期")
		}
		if remaining < budget {
			budget = remaining
		}
	}
	box := filepath.Join(p.dir, "cfbox")
	if e = os.MkdirAll(box, 0700); e != nil {
		return "", e
	}
	entries, _ := os.ReadDir(box)
	for _, entry := range entries {
		if regexp.MustCompile("^(pending|solved)-[A-Za-z0-9_-]+\\.json$").MatchString(entry.Name()) {
			if s, e := entry.Info(); e == nil && time.Since(s.ModTime()) > 15*time.Minute {
				_ = os.Remove(filepath.Join(box, entry.Name()))
			}
		}
	}
	pending := filepath.Join(box, "pending-"+id+".json")
	solved := filepath.Join(box, "solved-"+id+".json")
	_ = os.Remove(solved)
	doc := map[string]any{"id": id, "bot": t.Bot, "purpose": purpose, "buttonText": button, "url": raw, "state": state, "expectProof": proof, "createdAt": time.Now().UnixMilli(), "expiresAt": int64(expires * 1000)}
	if e = api.SaveJSON(pending, doc); e != nil {
		return "", e
	}
	bucket := p.snapshot().Bucket
	endpoint := "https://api.npoint.io/" + bucket
	cloud := false
	cloudDoc := map[string]any{"cf_url": raw, "job": id, "bot": t.Bot, "purpose": purpose, "button_text": button, "expect_proof": proof, "status": "pending", "solver": "", "proof": "", "error": "", "issued_at": time.Now().Unix(), "expires_at": expires}
	if bucket != "" {
		cc, cancel := context.WithTimeout(ctx, 15*time.Second)
		cloud = p.request(cc, "POST", endpoint, "", cloudDoc, nil) == nil
		cancel()
	}
	defer func() {
		_ = os.Remove(pending)
		_ = os.Remove(solved)
		if cloud {
			cc, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			var current map[string]any
			if p.request(cc, "GET", endpoint, "", nil, &current) == nil && current["job"] == id {
				current["cf_url"] = "none"
				current["status"] = "done"
				current["proof"] = ""
				_ = p.request(cc, "POST", endpoint, "", current, nil)
			}
		}
	}()
	deadline := time.Now().Add(budget)
	boxDone, cloudDone := false, !cloud
	lastError := ""
	mismatch := 0
	other := ""
	for time.Now().Before(deadline) {
		ms, e := p.history(ctx, t.Bot)
		if e != nil {
			return "", e
		}
		for _, m := range ms {
			if c.fresh(m) {
				if failureRE.MatchString(m.Text) {
					return "", errors.New(m.Text)
				}
				if successRE.MatchString(m.Text) {
					return m.Text, nil
				}
			}
		}
		var ack map[string]any
		winner := false
		var value string
		if !boxDone {
			if api.LoadJSON(solved, &ack) == nil {
				boxDone = true
				ok, _ := ack["ok"].(bool)
				value, _ = ack["proof"].(string)
				if ok && (!proof || value != "") {
					winner = true
				} else {
					lastError = "本机外援失败或缺少 proof"
				}
			}
		}
		if !winner && !cloudDone {
			cc, cancel := context.WithTimeout(ctx, 8*time.Second)
			var v map[string]any
			err := p.request(cc, "GET", endpoint, "", nil, &v)
			cancel()
			if err == nil {
				jid, _ := v["job"].(string)
				if jid == id {
					mismatch = 0
					status, _ := v["status"].(string)
					if status == "solved" {
						cloudDone = true
						value, _ = v["proof"].(string)
						if !proof || value != "" {
							winner = true
						} else {
							lastError = "云端缺少 proof"
						}
					} else if status == "failed" || status == "done" {
						cloudDone = true
						lastError = fmt.Sprint(v["error"])
					}
				} else if jid != "" {
					if jid == other {
						mismatch++
					} else {
						other = jid
						mismatch = 1
					}
					if mismatch >= 2 {
						cloudDone = true
						lastError = "云端信箱被另一作业占用"
					}
				}
			}
		}
		if winner {
			if proof {
				data, _ := json.Marshal(map[string]string{"type": purpose + "_turnstile", "state": state, "proof": value})
				if _, e = p.host.Call(ctx, api.Call{Method: "webview_data", Target: t.Bot, ButtonText: button, Data: string(data)}); e != nil {
					return "", e
				}
			}
			return p.resultWithoutCF(ctx, t, c)
		}
		if boxDone && cloudDone {
			return "", fmt.Errorf("两路外援均未完成: %s", lastError)
		}
		if e = sleep(ctx, max(p.Poll, 2*time.Second)); e != nil {
			return "", e
		}
	}
	return "", errors.New("CF 外援处理超时；需运行配置了相同 cfbox 或 npoint bucket 的外部执行端")
}

func (p *Plugin) resultWithoutCF(ctx context.Context, t Task, c *cursor) (string, error) {
	ms, e := p.waitFresh(ctx, t.Bot, c, 36*time.Second, func(m api.Message) bool { return successRE.MatchString(m.Text) || failureRE.MatchString(m.Text) })
	if e != nil {
		return "", e
	}
	s, e := terminal(ms)
	if s == "" && e == nil {
		e = errors.New("外援完成但机器人未确认")
	}
	return s, e
}
