package plugin

import (
	"os"
	"path/filepath"
	"strings"
)

// Snapshot pins the installed code and manifests for one host lifetime. Package
// replacements continue to affect the deployment, while existing workers and
// future calls in this host see exactly the code that was registered at startup.
// The caller must invoke cleanup after all its workers have stopped.
func (m Manager) Snapshot() (Manager, func(), error) {
	installMu.Lock()
	defer installMu.Unlock()
	lock, err := m.transactionLock()
	if err != nil {
		return Manager{}, nil, err
	}
	defer lock.Close()
	base := filepath.Join(m.Root, "plugins")
	entries, err := os.ReadDir(base)
	if err != nil && !os.IsNotExist(err) {
		return Manager{}, nil, err
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".previous-") {
			if err = m.recover(strings.TrimPrefix(entry.Name(), ".previous-")); err != nil {
				return Manager{}, nil, err
			}
		}
	}
	entries, err = os.ReadDir(base)
	if err != nil && !os.IsNotExist(err) {
		return Manager{}, nil, err
	}
	root, err := os.MkdirTemp("", "laowangbot-plugins-")
	if err != nil {
		return Manager{}, nil, err
	}
	cleanup := func() { _ = os.RemoveAll(root) }
	stateRoot := m.StateRoot
	if stateRoot == "" {
		stateRoot = m.Root
	}
	snapshot := Manager{Root: root, StateRoot: stateRoot}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		manifest, err := m.load(entry.Name())
		if err != nil {
			cleanup()
			return Manager{}, nil, err
		}
		target := filepath.Join(root, "plugins", entry.Name())
		if err = copyPlugin(filepath.Join(base, entry.Name()), target, manifest); err != nil {
			cleanup()
			return Manager{}, nil, err
		}
		if _, err = readManifest(target); err != nil {
			cleanup()
			return Manager{}, nil, err
		}
	}
	return snapshot, cleanup, nil
}
