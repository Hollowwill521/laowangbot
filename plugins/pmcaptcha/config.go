package pmcaptcha

import (
	"encoding/json"
	"errors"
	"fmt"
	api "github.com/OrionG-hub/laowangbot/pkg/pluginapi"
	"os"
	"path/filepath"
	"slices"
)

type Config struct {
	Enabled          bool     `json:"plugin_enabled"`
	Captcha          bool     `json:"captcha_enabled"`
	Mode             string   `json:"captcha_mode"`
	Timeout          int      `json:"captcha_timeout"`
	Tries            int      `json:"captcha_max_tries"`
	Fail             []string `json:"captcha_fail_actions"`
	Keyword          string   `json:"captcha_text_keyword"`
	Prompt           string   `json:"captcha_prompt"`
	Pass             []string `json:"captcha_pass_actions"`
	Folder           int      `json:"captcha_pass_folder"`
	InitiativeFailed bool     `json:"auto_initiative_failed"`
	Initiative       bool     `json:"auto_initiative"`
	History          int      `json:"auto_history_count"`
	Groups           int      `json:"auto_groups_in_common"`
	WLWords          []string `json:"auto_whitelist_words"`
	BLWords          []string `json:"auto_blacklist_words"`
	Premium          string   `json:"auto_premium"`
}
type Record struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	Username string `json:"username,omitempty"`
	Time     string `json:"time"`
	Reason   string `json:"reason,omitempty"`
}
type Data struct {
	Whitelist []int64  `json:"whitelist_user_ids"`
	Verified  []Record `json:"verified_users"`
	Failed    []Record `json:"failed_users"`
	Merged    bool     `json:"_merged_legacy_v1"`
}

func defaults() Config {
	return Config{Mode: "math", Timeout: 30, Tries: 3, Keyword: "我同意", Initiative: true, History: -1, Groups: -1, Premium: "none", Fail: []string{}, Pass: []string{}, WLWords: []string{}, BLWords: []string{}}
}
func validate(c Config) error {
	if !slices.Contains([]string{"math", "text", "img_digit", "img_mixed"}, c.Mode) {
		return errors.New("无效验证模式")
	}
	if c.Timeout < 0 || int64(c.Timeout) > int64(^uint64(0)>>1)/1000000000 || c.Tries < 0 || c.Folder < 0 || c.History < -1 || c.Groups < -1 {
		return errors.New("无效数字参数")
	}
	if c.Keyword == "" || !slices.Contains([]string{"none", "allow", "ban", "only"}, c.Premium) {
		return errors.New("无效关键词或 Premium 策略")
	}
	for _, a := range c.Fail {
		if !slices.Contains([]string{"block", "delete", "report", "mute", "archive", "kick", "ban"}, a) {
			return fmt.Errorf("无效失败动作 %s", a)
		}
	}
	for _, a := range c.Pass {
		if !slices.Contains([]string{"unmute", "unarchive", "wl", "add_folder"}, a) {
			return fmt.Errorf("无效通过动作 %s", a)
		}
	}
	return nil
}
func readMap(path string) (map[string]json.RawMessage, error) {
	b, e := os.ReadFile(path)
	if errors.Is(e, os.ErrNotExist) {
		return map[string]json.RawMessage{}, nil
	}
	if e != nil {
		return nil, e
	}
	var m map[string]json.RawMessage
	e = json.Unmarshal(b, &m)
	return m, e
}
func (p *Plugin) load() error {
	p.config = defaults()
	p.data = Data{Whitelist: []int64{}, Verified: []Record{}, Failed: []Record{}, Merged: true}
	// Migration copies legacy files into this directory. The marker prevents deleted
	// records from being imported again from an old config on future starts.
	old, e := readMap(filepath.Join(p.dir, "legacy", "pmcaptcha_config.json"))
	if e != nil {
		return e
	}
	oldData, e := readMap(filepath.Join(p.dir, "legacy", "pmcaptcha_data.json"))
	if e != nil {
		return e
	}
	panel, e := readMap(filepath.Join(p.dir, "config.json"))
	if e != nil {
		return e
	}
	current, e := readMap(filepath.Join(p.dir, "pmcaptcha_config.json"))
	if e != nil {
		return e
	}
	merged := map[string]json.RawMessage{}
	for _, m := range []map[string]json.RawMessage{old, panel, current} {
		for k, v := range m {
			merged[k] = v
		}
	}
	b, _ := json.Marshal(merged)
	if e = json.Unmarshal(b, &p.config); e != nil {
		return e
	}
	if e = validate(p.config); e != nil {
		return e
	}
	dm, e := readMap(filepath.Join(p.dir, "pmcaptcha_data.json"))
	if e != nil {
		return e
	}
	marker := false
	if v := dm["_merged_legacy_v1"]; v != nil {
		if e = json.Unmarshal(v, &marker); e != nil {
			return e
		}
	}
	if marker {
		b, _ = json.Marshal(dm)
		if e = json.Unmarshal(b, &p.data); e != nil {
			return e
		}
	} else {
		for _, m := range []map[string]json.RawMessage{old, panel, current, oldData, dm} {
			var d Data
			b, _ = json.Marshal(m)
			if e = json.Unmarshal(b, &d); e != nil {
				return e
			}
			for _, id := range d.Whitelist {
				if id > 0 && !slices.Contains(p.data.Whitelist, id) {
					p.data.Whitelist = append(p.data.Whitelist, id)
				}
			}
			for _, r := range d.Verified {
				if r.ID > 0 {
					p.data.Verified = upsert(p.data.Verified, r)
				}
			}
			for _, r := range d.Failed {
				if r.ID > 0 {
					p.data.Failed = upsert(p.data.Failed, r)
				}
			}
		}
	}
	if e = api.SaveJSON(filepath.Join(p.dir, "pmcaptcha_data.json"), p.data); e != nil {
		return e
	}
	return api.SaveJSON(filepath.Join(p.dir, "pmcaptcha_config.json"), p.config)
}
func upsert(rs []Record, r Record) []Record {
	rs = slices.Clone(rs)
	for i := range rs {
		if rs[i].ID == r.ID {
			rs[i] = r
			return rs
		}
	}
	return append(rs, r)
}
func remove(rs []Record, id int64) []Record {
	return slices.DeleteFunc(slices.Clone(rs), func(r Record) bool { return r.ID == id })
}
func has(rs []Record, id int64) bool {
	return slices.ContainsFunc(rs, func(r Record) bool { return r.ID == id })
}
func cloneData(d Data) Data {
	d.Whitelist = slices.Clone(d.Whitelist)
	d.Verified = slices.Clone(d.Verified)
	d.Failed = slices.Clone(d.Failed)
	return d
}
func (p *Plugin) saveData(d Data) error {
	if e := api.SaveJSON(filepath.Join(p.dir, "pmcaptcha_data.json"), d); e != nil {
		return fmt.Errorf("数据保存失败: %w", e)
	}
	p.data = d
	return nil
}
func (p *Plugin) saveConfig(c Config) error {
	if e := validate(c); e != nil {
		return e
	}
	if e := api.SaveJSON(filepath.Join(p.dir, "pmcaptcha_config.json"), c); e != nil {
		return fmt.Errorf("配置保存失败: %w", e)
	}
	p.config = c
	return nil
}
