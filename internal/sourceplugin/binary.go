package sourceplugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/OrionG-hub/laowangbot/internal/compiled"
)

// NeedsSourceBuild fails closed on unreadable state and also checks the linked
// registry: removing source files must never silently discard compiled plugins.
func NeedsSourceBuild(root string) (bool, error) {
	if len(compiled.Entries()) != 0 {
		return true, nil
	}
	if _, err := os.Lstat(filepath.Join(root, ".compiled", "pending")); err == nil {
		return true, nil
	} else if !os.IsNotExist(err) {
		return false, err
	}
	// An empty source deployment may return to official binaries after uninstall.
	// Invalid metadata is still an error, never permission to discard state.
	if data, err := os.ReadFile(filepath.Join(root, ".compiled", "current.json")); err == nil {
		var current metadata
		if err = json.Unmarshal(data, &current); err != nil {
			return false, err
		}
		if current.Version == "" {
			return false, errors.New("source metadata has no version")
		}
	} else if !os.IsNotExist(err) {
		return false, err
	}
	entries, err := os.ReadDir(filepath.Join(root, "plugins"))
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	// Preserve even malformed/legacy/hidden packages rather than guessing their purpose.
	return len(entries) > 0, nil
}

// InstallBinary uses the same lock, checks, journal and rollback snapshots as
// source builds. fetch streams a verified release executable into candidate.
func (b Builder) InstallBinary(ctx context.Context, tag string, fetch func(candidate string) error) error {
	if !releaseTag.MatchString(tag) {
		return errors.New("binary update requires an exact release tag vX.Y.Z")
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
	required, err := NeedsSourceBuild(b.Root)
	if err != nil {
		return err
	}
	if required {
		return errors.New("deployment contains compiled plugins or source state; use source update")
	}
	stage, err := os.MkdirTemp(filepath.Join(b.Root, ".compiled"), "stage-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	// Preserve the empty plugin directory, but discard stale host source only in
	// staging: future plugin builds must use the newly installed release sources.
	if err = b.snapshot(stage); err != nil {
		return err
	}
	for _, name := range []string{"source", "current.json"} {
		if err = os.RemoveAll(filepath.Join(stage, name)); err != nil {
			return err
		}
	}
	candidate := filepath.Join(stage, "binary")
	if err = fetch(candidate); err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if err = os.Chmod(candidate, 0700); err != nil {
		return err
	}
	var output bounded
	version := exec.CommandContext(ctx, candidate, "--version")
	version.Stdout, version.Stderr = &output, &output
	if err = version.Run(); err != nil {
		return fmt.Errorf("candidate version check: %w", err)
	}
	if strings.TrimPrefix(strings.TrimSpace(string(output.data)), "v") != strings.TrimPrefix(tag, "v") {
		return errors.New("candidate version does not match release tag")
	}
	check := exec.CommandContext(ctx, candidate, "--check-plugins")
	check.Dir = stage
	if err = run(check); err != nil {
		return fmt.Errorf("candidate plugin validation: %w", err)
	}
	if _, err = os.Stat(filepath.Join(b.Root, "config.json")); err == nil {
		check = exec.CommandContext(ctx, candidate, "--check", "--root", b.Root)
		check.Dir = stage
		if err = run(check); err != nil {
			return fmt.Errorf("candidate configuration validation: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	return b.commit(stage)
}
