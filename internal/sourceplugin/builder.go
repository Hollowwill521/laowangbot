// Package sourceplugin builds portable plugins into a replacement host executable.
package sourceplugin

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"github.com/OrionG-hub/laowangbot/internal/platform"
	"github.com/OrionG-hub/laowangbot/internal/plugin"
)

type Builder struct{ Root, Binary, Source, Version, Repo string }
type metadata struct {
	Version string `json:"version"`
}

var releaseTag = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+(?:-[A-Za-z0-9.-]+)?$`)

// Subprocesses run from staging directories, so deployment paths must not depend
// on their working directory. Preserve empty paths for the existing validation.
func (b *Builder) absolutePaths() error {
	for _, path := range []*string{&b.Root, &b.Binary, &b.Source} {
		if *path == "" {
			continue
		}
		absolute, err := filepath.Abs(*path)
		if err != nil {
			return err
		}
		*path = absolute
	}
	return nil
}

func (b Builder) Apply(ctx context.Context, mutate func(plugin.Manager) error) error {
	return b.run(ctx, "", mutate)
}
func (b Builder) Update(ctx context.Context, tag string) error {
	if !releaseTag.MatchString(tag) {
		return errors.New("update requires an exact release tag vX.Y.Z")
	}
	return b.run(ctx, tag, nil)
}
func (b Builder) lock() (*os.File, error) {
	if runtime.GOOS == "windows" {
		return nil, errors.New("Windows cannot replace the running executable; source rebuild requires an external stopped-process installer")
	}
	if b.Root == "" || b.Binary == "" {
		return nil, errors.New("deployment root and binary are required")
	}
	for _, path := range []string{b.Root, filepath.Join(b.Root, ".compiled"), b.Binary} {
		if info, err := os.Lstat(path); err == nil && info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("deployment symlink rejected: %s", path)
		} else if err != nil && !os.IsNotExist(err) {
			return nil, err
		}
	}
	if err := os.MkdirAll(filepath.Join(b.Root, ".compiled"), 0700); err != nil {
		return nil, err
	}
	return platform.LockRoot(filepath.Join(b.Root, ".compiled"))
}
func (b Builder) run(ctx context.Context, tag string, mutate func(plugin.Manager) error) error {
	if err := b.absolutePaths(); err != nil {
		return err
	}
	goBinary, err := findGo()
	if err != nil {
		return err
	}
	lock, err := b.lock()
	if err != nil {
		return err
	}
	defer lock.Close()
	if err = b.recover(); err != nil {
		return err
	}
	base := filepath.Join(b.Root, ".compiled")
	stage, err := os.MkdirTemp(base, "stage-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	if err = copyTree(filepath.Join(b.Root, "plugins"), filepath.Join(stage, "plugins"), false); err != nil {
		return err
	}
	if mutate != nil {
		if err = mutate(plugin.Manager{Root: stage}); err != nil {
			return err
		}
	}
	entries, err := os.ReadDir(filepath.Join(stage, "plugins"))
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	manifests := []plugin.Manifest{}
	dirs := []string{}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".") || !entry.IsDir() {
			continue
		}
		dir := filepath.Join(stage, "plugins", entry.Name())
		m, e := plugin.SourceManifest(dir)
		if e != nil {
			// Existing legacy packages remain visible for migration, but are never linked.
			// Only byte-for-byte unchanged legacy code may survive a source transaction.
			if m.ProtocolVersion == 1 {
				before, beforeErr := treeDigest(filepath.Join(b.Root, "plugins", entry.Name()))
				after, afterErr := treeDigest(dir)
				if beforeErr == nil && afterErr == nil && before == after {
					continue
				}
			}
			return fmt.Errorf("plugin %s: %w", entry.Name(), e)
		}
		if m.Name != entry.Name() {
			return errors.New("plugin name does not match directory")
		}
		manifests = append(manifests, m)
		dirs = append(dirs, dir)
	}
	source := filepath.Join(stage, "source")
	version := b.Version
	if tag != "" {
		version = tag
		err = b.clone(ctx, source, tag)
	} else if data, e := os.ReadFile(filepath.Join(base, "current.json")); e == nil {
		var m metadata
		if err = json.Unmarshal(data, &m); err != nil {
			return err
		}
		version = m.Version
		err = copyTree(filepath.Join(base, "source"), source, true)
	} else if !os.IsNotExist(e) {
		return e
	} else {
		local := b.Source
		if local == "" {
			local = os.Getenv("LAOWANGBOT_SOURCE")
		}
		if local != "" {
			err = copyTree(local, source, true)
		} else {
			err = b.clone(ctx, source, version)
		}
	}
	if err != nil {
		return err
	}
	if _, err = os.Stat(filepath.Join(source, "go.mod")); err != nil {
		return fmt.Errorf("host source unavailable: %w", err)
	}
	if err = os.RemoveAll(filepath.Join(source, "internal/installed")); err != nil {
		return err
	}
	var imports, initializers strings.Builder
	for i, m := range manifests {
		alias := fmt.Sprintf("p%03d", i)
		if err = copyTree(dirs[i], filepath.Join(source, "internal/installed", alias), false); err != nil {
			return err
		}
		fmt.Fprintf(&imports, "%s %q\n", alias, "github.com/OrionG-hub/laowangbot/internal/installed/"+alias)
		data, _ := json.Marshal(m)
		fmt.Fprintf(&initializers, "{var m plugin.Manifest; if err:=json.Unmarshal([]byte(%q), &m);err!=nil{panic(err)};compiled.Register(m,%s.Open)}\n", string(data), alias)
	}
	generated := "package main\n"
	if len(manifests) > 0 {
		generated += "import (\"encoding/json\";\"github.com/OrionG-hub/laowangbot/internal/plugin\";\"github.com/OrionG-hub/laowangbot/internal/compiled\";\n" + imports.String() + ")\nfunc init(){\n" + initializers.String() + "}\n"
	}
	if err = os.WriteFile(filepath.Join(source, "cmd/laowangbot/zz_plugins_generated.go"), []byte(generated), 0600); err != nil {
		return err
	}
	candidate := filepath.Join(stage, "binary")
	cmd := exec.CommandContext(ctx, goBinary, "build", "-mod=readonly", "-trimpath", "-ldflags", "-s -w -X main.version="+version, "-o", candidate, "./cmd/laowangbot")
	cmd.Dir = source
	cmd.Env = buildEnv()
	if err = run(cmd); err != nil {
		return err
	}
	check := exec.CommandContext(ctx, candidate, "--check-plugins")
	check.Dir = stage
	if err = run(check); err != nil {
		return fmt.Errorf("compiled plugin validation: %w", err)
	}
	if _, err = os.Stat(filepath.Join(b.Root, "config.json")); err == nil {
		root, absErr := filepath.Abs(b.Root)
		if absErr != nil {
			return absErr
		}
		configCheck := exec.CommandContext(ctx, candidate, "--check", "--root", root)
		configCheck.Dir = stage
		if err = run(configCheck); err != nil {
			return fmt.Errorf("deployment configuration validation: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	data, _ := json.Marshal(metadata{Version: version})
	if err = os.WriteFile(filepath.Join(stage, "current.json"), data, 0600); err != nil {
		return err
	}
	return b.commit(stage)
}
func buildEnv() []string {
	out := []string{}
	for _, s := range os.Environ() {
		name, _, _ := strings.Cut(s, "=")
		switch name {
		case "CGO_ENABLED", "GOWORK", "GOFLAGS", "GOOS", "GOARCH":
			continue
		}
		out = append(out, s)
	}
	return append(out, "CGO_ENABLED=0", "GOWORK=off", "GOFLAGS=")
}
func (b Builder) clone(ctx context.Context, dest, tag string) error {
	if !strings.HasPrefix(tag, "v") {
		tag = "v" + tag
	}
	if !releaseTag.MatchString(tag) {
		return errors.New("host source unavailable for development version; set LAOWANGBOT_SOURCE to the host checkout")
	}
	repo := b.Repo
	if repo == "" {
		repo = "OrionG-hub/laowangbot"
	}
	if !regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`).MatchString(repo) {
		return errors.New("invalid GitHub repository")
	}
	cmd := exec.CommandContext(ctx, "git", "clone", "--depth", "1", "--branch", tag, "--single-branch", "https://github.com/"+repo+".git", dest)
	if err := run(cmd); err != nil {
		return err
	}
	verify := exec.CommandContext(ctx, "git", "rev-parse", "--verify", "refs/tags/"+tag+"^{commit}")
	verify.Dir = dest
	tagCommit, err := verify.Output()
	if err != nil {
		return fmt.Errorf("release tag missing: %w", err)
	}
	head := exec.CommandContext(ctx, "git", "rev-parse", "HEAD")
	head.Dir = dest
	headCommit, err := head.Output()
	if err != nil {
		return err
	}
	if string(tagCommit) != string(headCommit) {
		return errors.New("cloned branch does not match release tag")
	}
	return os.RemoveAll(filepath.Join(dest, ".git"))
}

type bounded struct{ data []byte }

func (b *bounded) Write(p []byte) (int, error) {
	n := len(p)
	remain := (64 << 10) - len(b.data)
	if remain > 0 {
		if len(p) > remain {
			p = p[:remain]
		}
		b.data = append(b.data, p...)
	}
	return n, nil
}
func run(cmd *exec.Cmd) error {
	var output bounded
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s failed: %w\n%s", filepath.Base(cmd.Path), err, output.data)
	}
	return nil
}
func copyTree(src, dst string, source bool) error {
	if _, err := os.Lstat(src); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return err
	}
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		if source && rel != "." {
			name := d.Name()
			if filepath.Dir(rel) == "." {
				switch name {
				case "cmd", "internal", "pkg", "plugins", "go.mod", "go.sum", "VERSION":
				default:
					if d.IsDir() {
						return filepath.SkipDir
					}
					return nil
				}
			}
			blocked := name == ".git" || name == ".compiled" || name == "node_modules" || name == "zz_plugins_generated.go"
			if blocked {
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
		}
		if d.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlink rejected: %s", p)
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0700)
		}
		if !d.Type().IsRegular() {
			return errors.New("non-regular source file")
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		return copyFile(p, target, info.Mode().Perm())
	})
}
func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err = os.MkdirAll(filepath.Dir(dst), 0700); err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, in)
	if err == nil {
		err = out.Sync()
	}
	closeErr := out.Close()
	if err != nil {
		return err
	}
	return closeErr
}
func (b Builder) snapshot(dest string) error {
	if err := copyFile(b.Binary, filepath.Join(dest, "binary"), 0700); err != nil {
		return err
	}
	for _, name := range []string{"plugins", "source", "current.json"} {
		src := filepath.Join(b.Root, name)
		if name != "plugins" {
			src = filepath.Join(b.Root, ".compiled", name)
		}
		if err := copyTree(src, filepath.Join(dest, name), false); err != nil {
			return err
		}
	}
	return nil
}
func (b Builder) restore(src string) error {
	// Atomic replacement happens on the binary's own filesystem.
	tmp, err := os.CreateTemp(filepath.Dir(b.Binary), ".laowangbot-replace-")
	if err != nil {
		return err
	}
	tmp.Close()
	defer os.Remove(tmp.Name())
	if err = copyFile(filepath.Join(src, "binary"), tmp.Name(), 0700); err != nil {
		return err
	}
	if err = os.Chmod(tmp.Name(), 0700); err != nil {
		return err
	}
	for _, name := range []string{"plugins", "source", "current.json"} {
		dest := filepath.Join(b.Root, name)
		if name != "plugins" {
			dest = filepath.Join(b.Root, ".compiled", name)
		}
		if err = os.RemoveAll(dest); err != nil {
			return err
		}
		if err = copyTree(filepath.Join(src, name), dest, false); err != nil {
			return err
		}
	}
	return os.Rename(tmp.Name(), b.Binary)
}
func (b Builder) recover() error {
	journal := filepath.Join(b.Root, ".compiled", "pending")
	if _, err := os.Stat(journal); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return err
	}
	if err := b.restore(journal); err != nil {
		return fmt.Errorf("recover pending rebuild: %w", err)
	}
	return os.RemoveAll(journal)
}
func (b Builder) commit(stage string) error {
	base := filepath.Join(b.Root, ".compiled")
	backup, err := os.MkdirTemp(base, "backup-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(backup)
	if err = b.snapshot(backup); err != nil {
		return err
	}
	journal := filepath.Join(base, "pending")
	if err = os.Rename(backup, journal); err != nil {
		return err
	}
	if err = b.restore(stage); err != nil {
		recoverErr := b.recover()
		return errors.Join(err, recoverErr)
	}
	previous := filepath.Join(base, "previous")
	if err = os.RemoveAll(previous); err != nil {
		return errors.Join(err, b.recover())
	}
	if err = atomicBackup(filepath.Join(journal, "binary"), b.Binary+".previous"); err != nil {
		return errors.Join(err, b.recover())
	}
	if err = os.Rename(journal, previous); err != nil {
		return errors.Join(err, b.recover())
	}
	return nil
}
func (b Builder) Rollback(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := b.absolutePaths(); err != nil {
		return err
	}
	lock, err := b.lock()
	if err != nil {
		return err
	}
	defer lock.Close()
	if err = b.recover(); err != nil {
		return err
	}
	base := filepath.Join(b.Root, ".compiled")
	stage, err := os.MkdirTemp(base, "rollback-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	previous := filepath.Join(base, "previous")
	if _, err = os.Stat(filepath.Join(previous, "binary")); err != nil {
		return errors.New("no source-aware previous build available")
	}
	if err = copyTree(previous, stage, false); err != nil {
		return err
	}
	return b.commit(stage)
}

// Recover restores the last complete build after an interrupted transaction.
func (b Builder) Recover() error {
	if err := b.absolutePaths(); err != nil {
		return err
	}
	if _, err := os.Lstat(filepath.Join(b.Root, ".compiled", "pending")); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return err
	}
	lock, err := b.lock()
	if err != nil {
		return err
	}
	defer lock.Close()
	return b.recover()
}

func treeDigest(root string) ([32]byte, error) {
	h := sha256.New()
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		fmt.Fprintf(h, "%s\x00%t\x00", rel, d.IsDir())
		if d.IsDir() {
			return nil
		}
		if !info.Mode().IsRegular() {
			return errors.New("legacy package contains non-regular file")
		}
		fmt.Fprintf(h, "%d\x00", info.Size())
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		_, err = io.Copy(h, f)
		return err
	})
	var result [32]byte
	copy(result[:], h.Sum(nil))
	return result, err
}

// atomicBackup replaces the backup directory entry without following symlinks.
func atomicBackup(src, dst string) error {
	f, err := os.CreateTemp(filepath.Dir(dst), ".laowangbot-backup-")
	if err != nil {
		return err
	}
	path := f.Name()
	defer os.Remove(path)
	if err = f.Close(); err != nil {
		return err
	}
	if err = copyFile(src, path, 0700); err != nil {
		return err
	}
	if err = os.Chmod(path, 0700); err != nil {
		return err
	}
	return os.Rename(path, dst)
}
