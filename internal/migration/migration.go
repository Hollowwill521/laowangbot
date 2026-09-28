// Package migration imports legacy deployments without modifying their source.
package migration

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/OrionG-hub/laowangbot/internal/config"
	"github.com/OrionG-hub/laowangbot/internal/platform"
	"github.com/OrionG-hub/laowangbot/internal/session"
)

type Options struct {
	Source, Destination, Kind string
	Builtins                  map[string]bool
	Convert                   func(source, dataDir string, out io.Writer) error
}
type Plugin struct {
	Name           string `json:"name"`
	Status         string `json:"status"`
	Source         string `json:"source,omitempty"`
	PreviousSource string `json:"previous_source,omitempty"`
}
type Report struct {
	Kind       string   `json:"kind"`
	Files      []string `json:"files"`
	Plugins    []Plugin `json:"plugins"`
	Warnings   []string `json:"warnings"`
	Conversion string   `json:"conversion,omitempty"`
}

func Run(o Options) (*Report, error) {
	src, e := filepath.Abs(o.Source)
	if e != nil {
		return nil, e
	}
	dst, e := filepath.Abs(o.Destination)
	if e != nil {
		return nil, e
	}
	src, e = filepath.EvalSymlinks(src)
	if e != nil {
		return nil, e
	}
	// Resolve the closest existing destination ancestor before containment checks.
	dst, e = canonicalDestination(dst)
	if e != nil {
		return nil, e
	}
	if src == dst || strings.HasPrefix(dst, src+string(os.PathSeparator)) {
		return nil, errors.New("destination must be outside source")
	}
	if info, e := os.Lstat(dst); e == nil {
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return nil, errors.New("destination must be an empty directory")
		}
		entries, e := os.ReadDir(dst)
		if e != nil {
			return nil, e
		}
		if len(entries) > 0 {
			return nil, errors.New("destination is not empty; select a new deployment directory")
		}
	} else if !os.IsNotExist(e) {
		return nil, e
	}
	// Existing Go deployments use the same lock; refuse a live source.
	// Do not create a lock file in the source (the source remains unchanged).
	if _, err := os.Stat(filepath.Join(src, "mibot-lite.lock")); err == nil {
		lock, err := platform.LockRoot(src)
		if err != nil {
			return nil, err
		}
		defer lock.Close()
	}
	cfg, e := config.Read(src)
	if e != nil {
		return nil, e
	}
	if _, e = session.ParseStringSession(cfg.Session); e != nil {
		return nil, fmt.Errorf("legacy session: %w", e)
	}
	kind := o.Kind
	if kind == "" || kind == "auto" {
		kind = "mibot-lite"
		if _, e := os.Stat(filepath.Join(src, "assets")); e == nil {
			kind = "mibox"
		}
		if b, e := os.ReadFile(filepath.Join(src, "package.json")); e == nil {
			var p struct {
				Name string `json:"name"`
			}
			if json.Unmarshal(b, &p) == nil && strings.Contains(strings.ToLower(p.Name), "telebox") {
				kind = "telebox"
			}
		}
	}
	if kind != "mibot-lite" && kind != "mibox" && kind != "telebox" {
		return nil, fmt.Errorf("unsupported source kind %q", kind)
	}
	if e = os.MkdirAll(filepath.Dir(dst), 0700); e != nil {
		return nil, e
	}
	stage, e := os.MkdirTemp(filepath.Dir(dst), ".laowangbot-migrate-")
	if e != nil {
		return nil, e
	}
	defer os.RemoveAll(stage)
	r := &Report{Kind: kind, Warnings: []string{"Stop the source bot before starting this deployment; never run both against the same session."}}
	for _, name := range []string{"config.json", "gotd-session.json", ".env", "data"} {
		if e = copyOptional(src, stage, name, name, r); e != nil {
			return nil, e
		}
	}
	// Archive original plugin data and source, including formats without converters.
	for _, name := range []string{"assets", "plugins"} {
		if e = copyOptional(src, stage, name, "legacy/"+name, r); e != nil {
			return nil, e
		}
	}
	for _, name := range []string{"monitor/monitor.json", "qdsg/signin_config.json"} {
		if e = copyOptional(filepath.Join(stage, "legacy/assets"), stage, name, "state/"+name, r); e != nil {
			return nil, e
		}
	}
	if o.Convert != nil && (kind == "mibox" || kind == "telebox") {
		var log bytes.Buffer
		if e = o.Convert(filepath.Join(stage, "legacy"), filepath.Join(stage, "data"), &log); e != nil {
			return nil, e
		}
		r.Conversion = log.String()
	}
	env := config.ReadEnv(stage, nil)
	if len(env.Prefixes()) > 0 && env.Get("MIBOT_PREFIX", "") == "" && env["TB_PREFIX"] != "" {
		if e = config.SetEnv(stage, "LAOWANGBOT_PREFIX", env["TB_PREFIX"]); e != nil {
			return nil, e
		}
	}
	for key, value := range map[string]string{"LAOWANGBOT_UPDATE_REPO": "OrionG-hub/laowangbot", "LAOWANGBOT_SERVICE": "laowangbot.service"} {
		if e = config.SetEnv(stage, key, value); e != nil {
			return nil, e
		}
	}
	r.Plugins, e = inventory(filepath.Join(stage, "legacy"), o.Builtins)
	if e != nil {
		return nil, e
	}
	if len(r.Plugins) > 0 {
		r.Warnings = append(r.Warnings, "Legacy TypeScript files are archived, not executed. Builtin replacements update with laowangbot; other plugins require adaptation and explicit installation.")
	}
	if kind != "mibot-lite" {
		r.Warnings = append(r.Warnings, "Unmapped assets and databases are retained under legacy/assets; they are not automatically consumed by Go commands. See conversion report.")
	}
	b, e := json.MarshalIndent(r, "", "  ")
	if e != nil {
		return nil, e
	}
	if e = os.WriteFile(filepath.Join(stage, "migration-report.json"), append(b, '\n'), 0600); e != nil {
		return nil, e
	}
	// Empty target only; recheck immediately before atomic directory promotion.
	if entries, e := os.ReadDir(dst); e == nil {
		if len(entries) != 0 {
			return nil, errors.New("destination changed during migration")
		}
		if e = os.Remove(dst); e != nil {
			return nil, e
		}
	} else if !os.IsNotExist(e) {
		return nil, e
	}
	if e = os.Rename(stage, dst); e != nil {
		return nil, e
	}
	return r, nil
}

func copyOptional(src, dst, name, target string, r *Report) error {
	base := filepath.Join(src, name)
	if _, e := os.Lstat(base); os.IsNotExist(e) {
		return nil
	} else if e != nil {
		return e
	}
	return filepath.WalkDir(base, func(p string, d fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		rel, e := filepath.Rel(base, p)
		if e != nil {
			return e
		}
		out := filepath.Join(dst, target, rel)
		if d.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("refusing symlink %s", p)
		}
		if d.IsDir() {
			return os.MkdirAll(out, 0700)
		}
		info, e := d.Info()
		if e != nil {
			return e
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("not a regular file: %s", p)
		}
		if strings.HasSuffix(p, "-wal") && info.Size() > 0 {
			return fmt.Errorf("uncheckpointed SQLite WAL: %s; stop the source and checkpoint its databases first", p)
		}
		if e = os.MkdirAll(filepath.Dir(out), 0700); e != nil {
			return e
		}
		in, e := os.Open(p)
		if e != nil {
			return e
		}
		defer in.Close()
		f, e := os.OpenFile(out, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if e != nil {
			return e
		}
		_, e = io.Copy(f, in)
		closeErr := f.Close()
		if e != nil {
			return e
		}
		if closeErr != nil {
			return closeErr
		}
		r.Files = append(r.Files, filepath.ToSlash(filepath.Join(target, rel)))
		return nil
	})
}

func inventory(root string, builtins map[string]bool) ([]Plugin, error) {
	var records map[string]struct {
		URL string `json:"url"`
	}
	if b, e := os.ReadFile(filepath.Join(root, "assets/tpm/plugins.json")); e == nil {
		if e = json.Unmarshal(b, &records); e != nil {
			return nil, fmt.Errorf("invalid TPM inventory: %w", e)
		}
	} else if !os.IsNotExist(e) {
		return nil, e
	}
	names := map[string]bool{}
	for n := range records {
		names[n] = true
	}
	entries, e := os.ReadDir(filepath.Join(root, "plugins"))
	if e != nil && !os.IsNotExist(e) {
		return nil, e
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			names[strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name()))] = true
		}
	}
	sorted := make([]string, 0, len(names))
	for n := range names {
		sorted = append(sorted, n)
	}
	sort.Strings(sorted)
	var result []Plugin
	for _, n := range sorted {
		p := Plugin{Name: n, Status: "manual", PreviousSource: records[n].URL}
		if p.PreviousSource != "" {
			p.Status = "needs-adaptation"
			p.Source = "https://github.com/OrionG-hub/laowangbot"
			if builtins[n] {
				p.Status = "builtin"
			}
		}
		result = append(result, p)
	}
	return result, nil
}

func canonicalDestination(p string) (string, error) {
	cursor := p
	var tail []string
	for {
		resolved, err := filepath.EvalSymlinks(cursor)
		if err == nil {
			for i := len(tail) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, tail[i])
			}
			return resolved, nil
		}
		if !os.IsNotExist(err) {
			return "", err
		}
		parent := filepath.Dir(cursor)
		if parent == cursor {
			return "", err
		}
		tail = append(tail, filepath.Base(cursor))
		cursor = parent
	}
}
