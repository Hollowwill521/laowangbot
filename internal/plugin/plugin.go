// Package plugin hosts explicitly installed, trusted executable plugins.
package plugin

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/OrionG-hub/laowangbot/internal/platform"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
)

const MaxOutput = 1 << 20

var commandIdentifier = regexp.MustCompile(`^[a-zA-Z0-9_]+$`)
var identifier = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,63}$`)

type Manifest struct {
	Name            string   `json:"name"`
	Version         string   `json:"version"`
	ProtocolVersion int      `json:"protocol_version"`
	Executable      string   `json:"executable"`
	Args            []string `json:"args,omitempty"`
	Commands        []string `json:"commands,omitempty"`
	Events          []string `json:"events,omitempty"`
	IntervalSeconds int      `json:"interval_seconds,omitempty"`
	Persistent      bool     `json:"persistent,omitempty"`
	TimeoutSeconds  int      `json:"timeout_seconds,omitempty"`
}
type Request struct {
	Version int             `json:"version"`
	Type    string          `json:"type"`
	Command string          `json:"command,omitempty"`
	Args    []string        `json:"args,omitempty"`
	Text    string          `json:"text,omitempty"`
	Event   json.RawMessage `json:"event,omitempty"`
}
type Message struct {
	ChatID string `json:"chat_id"`
	Text   string `json:"text"`
}
type Response struct {
	Messages []Message `json:"messages,omitempty"`
	Version  int       `json:"version"`
	Text     string    `json:"text,omitempty"`
	Error    string    `json:"error,omitempty"`
}
type Manager struct {
	Root string
	// StateRoot keeps mutable plugin state in the deployment when Root is a
	// private snapshot of the code. Empty means Root.
	StateRoot string
}

func (m Manager) directory(name string) (string, error) {
	if !identifier.MatchString(name) {
		return "", errors.New("invalid plugin name")
	}
	for _, p := range []string{m.Root, filepath.Join(m.Root, "plugins"), filepath.Join(m.Root, "plugins", name)} {
		if info, err := os.Lstat(p); err == nil && info.Mode()&os.ModeSymlink != 0 {
			return "", errors.New("plugin directory symlink rejected")
		}
	}
	return filepath.Join(m.Root, "plugins", name), nil
}
func validate(v Manifest) error {
	if !identifier.MatchString(v.Name) || v.Version == "" || v.ProtocolVersion != 1 {
		return errors.New("invalid name/version or unsupported protocol")
	}
	if v.Executable == "" || !filepath.IsLocal(v.Executable) || strings.Contains(v.Executable, "\\") {
		return errors.New("executable must be a local relative path")
	}
	if v.TimeoutSeconds < 0 || v.TimeoutSeconds > 300 || v.IntervalSeconds < 0 || v.IntervalSeconds > 31536000 {
		return errors.New("invalid timeout/interval")
	}
	if v.Persistent && len(v.Events) == 0 && v.IntervalSeconds == 0 {
		return errors.New("persistent plugin requires an event or interval subscription")
	}
	seen := map[string]bool{}
	for _, c := range v.Commands {
		if !commandIdentifier.MatchString(c) || seen[c] {
			return fmt.Errorf("invalid or duplicate command %q", c)
		}
		seen[c] = true
	}
	return nil
}
func readManifest(dir string) (Manifest, error) {
	var v Manifest
	f, e := os.Open(filepath.Join(dir, "manifest.json"))
	if e != nil {
		return v, e
	}
	defer f.Close()
	data, e := io.ReadAll(io.LimitReader(f, 65537))
	if e != nil {
		return v, e
	}
	if len(data) > 65536 {
		return v, errors.New("manifest exceeds 64 KiB")
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if e = d.Decode(&v); e != nil {
		return v, e
	}
	var extra any
	if e = d.Decode(&extra); e != io.EOF {
		return v, errors.New("manifest has trailing data")
	}
	if e = validate(v); e != nil {
		return v, e
	}
	e = filepath.WalkDir(dir, func(p string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlinks not allowed: %s", p)
		}
		if !entry.IsDir() && !entry.Type().IsRegular() {
			return fmt.Errorf("non-regular file: %s", p)
		}
		return nil
	})
	if e != nil {
		return v, e
	}
	fi, e := os.Stat(filepath.Join(dir, v.Executable))
	if e != nil {
		return v, e
	}
	if !fi.Mode().IsRegular() {
		return v, errors.New("executable is not a regular file")
	}
	return v, nil
}
func (m Manager) Load(name string) (Manifest, error) {
	installMu.Lock()
	defer installMu.Unlock()
	lock, e := m.transactionLock()
	if e != nil {
		return Manifest{}, e
	}
	defer lock.Close()
	return m.load(name)
}
func (m Manager) load(name string) (Manifest, error) {
	if e := m.recover(name); e != nil {
		return Manifest{}, e
	}
	d, e := m.directory(name)
	if e != nil {
		return Manifest{}, e
	}
	v, e := readManifest(d)
	if e == nil && v.Name != name {
		e = errors.New("manifest name does not match directory")
	}
	return v, e
}
func (m Manager) List() ([]Manifest, error) {
	installMu.Lock()
	defer installMu.Unlock()
	lock, e := m.transactionLock()
	if e != nil {
		return nil, e
	}
	defer lock.Close()
	entries0, _ := os.ReadDir(filepath.Join(m.Root, "plugins"))
	for _, entry := range entries0 {
		if strings.HasPrefix(entry.Name(), ".previous-") {
			if e := m.recover(strings.TrimPrefix(entry.Name(), ".previous-")); e != nil {
				return nil, e
			}
		}
	}
	entries, e := os.ReadDir(filepath.Join(m.Root, "plugins"))
	if os.IsNotExist(e) {
		return []Manifest{}, nil
	}
	if e != nil {
		return nil, e
	}
	var out []Manifest
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		v, e := m.load(entry.Name())
		if e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, nil
}

// InstallLocal copies a complete directory. Local installs are never remotely updated.
func (m Manager) InstallLocal(source string) error {
	if _, err := os.Lstat(filepath.Join(source, remoteMarker)); !os.IsNotExist(err) {
		return errors.New("reserved catalog marker in local plugin")
	}
	return m.install(source, false)
}

// ReplaceLocal explicitly replaces an installed plugin with a manually maintained copy.
func (m Manager) ReplaceLocal(source string) error {
	if _, err := os.Lstat(filepath.Join(source, remoteMarker)); !os.IsNotExist(err) {
		return errors.New("reserved catalog marker in local plugin")
	}
	return m.install(source, true)
}

var installMu sync.Mutex

func (m Manager) install(source string, replace bool) error {
	return m.installOwned(source, replace, "")
}
func (m Manager) transactionLock() (*os.File, error) {
	if _, e := m.directory("lockcheck"); e != nil {
		return nil, e
	}
	dir := filepath.Join(m.Root, ".plugin-lock")
	if info, e := os.Lstat(dir); e == nil && info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("plugin lock directory symlink rejected")
	}
	if e := os.MkdirAll(dir, 0700); e != nil {
		return nil, e
	}
	return platform.LockRoot(dir)
}
func (m Manager) installOwned(source string, replace bool, expectedCatalog string) error {
	installMu.Lock()
	defer installMu.Unlock()
	lock, e := m.transactionLock()
	if e != nil {
		return e
	}
	defer lock.Close()
	v, e := readManifest(source)
	if e != nil {
		return e
	}
	if e = m.recover(v.Name); e != nil {
		return e
	}
	target, e := m.directory(v.Name)
	if e != nil {
		return e
	}
	if expectedCatalog != "" && replace {
		marker, err := os.ReadFile(filepath.Join(target, remoteMarker))
		if err != nil || string(marker) != expectedCatalog {
			return errors.New("manual plugins cannot be remotely updated")
		}
	}
	base := filepath.Dir(target)
	if e = os.MkdirAll(base, 0700); e != nil {
		return e
	}
	if _, e = os.Lstat(target); e == nil && !replace {
		return errors.New("plugin already installed")
	}
	stage, e := os.MkdirTemp(base, ".stage-")
	if e != nil {
		return e
	}
	defer os.RemoveAll(stage)
	e = copyPlugin(source, stage, v)
	if e != nil {
		return e
	}
	if _, e = readManifest(stage); e != nil {
		return e
	}
	backup := filepath.Join(base, ".previous-"+v.Name)
	if replace {
		if _, e = os.Lstat(backup); !os.IsNotExist(e) {
			return errors.New("previous backup exists")
		}
		if e = os.Rename(target, backup); e != nil {
			return e
		}
	}
	if e = os.Rename(stage, target); e != nil {
		if replace {
			_ = os.Rename(backup, target)
		}
		return e
	}
	if replace {
		return os.RemoveAll(backup)
	}
	return nil
}

// recover completes an interrupted replacement while installMu is held.
func (m Manager) recover(name string) error {
	target, err := m.directory(name)
	if err != nil {
		return err
	}
	backup := filepath.Join(filepath.Dir(target), ".previous-"+name)
	if _, err = os.Lstat(backup); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return err
	}
	old, err := readManifest(backup)
	if err != nil || old.Name != name {
		return errors.New("invalid recovery backup")
	}
	if _, err = os.Lstat(target); os.IsNotExist(err) {
		return os.Rename(backup, target)
	} else if err != nil {
		return err
	}
	current, err := readManifest(target)
	if err == nil && current.Name == name {
		return os.RemoveAll(backup)
	}
	// Keep a malformed replacement for inspection and restore the last validated install.
	failed, err := os.MkdirTemp(filepath.Dir(target), ".failed-")
	if err != nil {
		return err
	}
	if err = os.Remove(failed); err != nil {
		return err
	}
	if err = os.Rename(target, failed); err != nil {
		return err
	}
	if err = os.Rename(backup, target); err != nil {
		_ = os.Rename(failed, target)
		return err
	}
	return nil
}

func validateRequest(v Manifest, r Request) error {
	switch r.Type {
	case "command":
		if !slices.Contains(v.Commands, r.Command) {
			return errors.New("unregistered command")
		}
	case "event":
		if len(v.Events) == 0 {
			return errors.New("plugin has no event subscription")
		}
	case "tick":
		if v.IntervalSeconds == 0 {
			return errors.New("plugin has no interval")
		}
	default:
		return errors.New("unknown request type")
	}
	return nil
}

// Worker serializes request/response exchanges with an explicitly persistent process.
type Worker struct {
	stdout   io.ReadCloser
	gate     chan struct{}
	cmd      *exec.Cmd
	stdin    io.WriteCloser
	scanner  *bufio.Scanner
	cancel   context.CancelFunc
	done     chan error
	manifest Manifest
}

func (m Manager) start(ctx context.Context, name string) (*Worker, error) {
	v, e := m.Load(name)
	if e != nil {
		return nil, e
	}
	dir, _ := m.directory(name)
	dir, e = filepath.Abs(dir)
	if e != nil {
		return nil, e
	}
	stateRoot := m.StateRoot
	if stateRoot == "" {
		stateRoot = m.Root
	}
	state, e := filepath.Abs(filepath.Join(stateRoot, "state", name))
	if e != nil {
		return nil, e
	}
	for _, p := range []string{filepath.Dir(state), state} {
		if info, err := os.Lstat(p); err == nil && info.Mode()&os.ModeSymlink != 0 {
			return nil, errors.New("state directory symlink rejected")
		}
	}
	if e = os.MkdirAll(state, 0700); e != nil {
		return nil, e
	}
	ctx, cancel := context.WithCancel(ctx)
	cmd := exec.CommandContext(ctx, filepath.Join(dir, v.Executable), v.Args...)
	configureProcess(cmd)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "LAOWANGBOT_STATE_DIR="+state, "LAOWANGBOT_PROTOCOL_VERSION=1")
	cmd.WaitDelay = time.Second
	stdin, e := cmd.StdinPipe()
	if e != nil {
		cancel()
		return nil, e
	}
	stdout, stdoutWriter := io.Pipe()
	cmd.Stdout = stdoutWriter
	cmd.Stderr = &limitedDiscard{limit: MaxOutput}
	if e = cmd.Start(); e != nil {
		cancel()
		return nil, e
	}
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 4096), MaxOutput)
	w := &Worker{gate: make(chan struct{}, 1), cmd: cmd, stdin: stdin, scanner: scanner, stdout: stdout, cancel: cancel, done: make(chan error, 1), manifest: v}
	go func() { err := cmd.Wait(); _ = stdoutWriter.CloseWithError(err); w.done <- err }()
	return w, nil
}

type limitedDiscard struct{ limit int }

func (w *limitedDiscard) Write(p []byte) (int, error) {
	if len(p) > w.limit {
		return 0, errors.New("plugin stderr limit exceeded")
	}
	w.limit -= len(p)
	return len(p), nil
}
func (m Manager) StartWorker(ctx context.Context, name string) (*Worker, error) {
	v, e := m.Load(name)
	if e != nil {
		return nil, e
	}
	if !v.Persistent || len(v.Events) == 0 && v.IntervalSeconds == 0 {
		return nil, errors.New("persistent worker not requested by manifest")
	}
	return m.start(ctx, name)
}
func (w *Worker) Close() error {
	w.cancel()
	_ = w.stdout.Close()
	_ = w.stdin.Close()
	select {
	case e := <-w.done:
		return e
	case <-time.After(2 * time.Second):
		return errors.New("plugin process did not stop")
	}
}
func (w *Worker) Call(ctx context.Context, r Request) (Response, error) {

	if e := validateRequest(w.manifest, r); e != nil {
		return Response{}, e
	}
	seconds := w.manifest.TimeoutSeconds
	if seconds == 0 {
		seconds = 15
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(seconds)*time.Second)
	defer cancel()
	select {
	case w.gate <- struct{}{}:
	case <-ctx.Done():
		return Response{}, ctx.Err()
	}
	defer func() { <-w.gate }()
	if e := ctx.Err(); e != nil {
		return Response{}, e
	}
	r.Version = 1
	type result struct {
		r Response
		e error
	}
	ch := make(chan result, 1)
	go func() {
		var out Response
		e := json.NewEncoder(w.stdin).Encode(r)
		if e == nil {
			if !w.scanner.Scan() {
				e = w.scanner.Err()
				if e == nil {
					e = io.ErrUnexpectedEOF
				}
			} else {
				e = json.Unmarshal(w.scanner.Bytes(), &out)
				if e == nil && len(out.Messages) > 20 {
					e = errors.New("too many response messages")
				}
				if e == nil && out.Version != 1 {
					e = errors.New("invalid response version")
				}
			}
		}
		ch <- result{out, e}
	}()
	select {
	case out := <-ch:
		if out.e != nil {
			w.cancel()
		}
		if out.e == nil && out.r.Error != "" {
			out.e = fmt.Errorf("plugin error: %s", out.r.Error)
		}
		return out.r, out.e
	case <-ctx.Done():
		w.cancel()
		_ = w.stdout.Close()
		_ = w.stdin.Close()
		<-ch
		return Response{}, ctx.Err()
	}
}
func (m Manager) Run(ctx context.Context, name string, r Request) (Response, error) {
	w, e := m.start(ctx, name)
	if e != nil {
		return Response{}, e
	}
	defer w.Close()
	return w.Call(ctx, r)
}

// copyPlugin copies validated code with private directories and executable modes.
func copyPlugin(source, stage string, v Manifest) error {
	return filepath.WalkDir(source, func(p string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(source, p)
		if err != nil {
			return err
		}
		dest := filepath.Join(stage, rel)
		if entry.Type()&os.ModeSymlink != 0 {
			return errors.New("symlink rejected")
		}
		if entry.IsDir() {
			return os.MkdirAll(dest, 0700)
		}
		if !entry.Type().IsRegular() {
			return errors.New("non-regular file")
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		mode := os.FileMode(0600)
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if rel == filepath.Clean(v.Executable) || info.Mode()&0111 != 0 {
			mode = 0700
		}
		return os.WriteFile(dest, b, mode)
	})
}
