// Package compiled holds plugins linked into this binary by generated init code.
package compiled

import (
	"github.com/OrionG-hub/laowangbot/internal/plugin"
	"github.com/OrionG-hub/laowangbot/pkg/pluginapi"
	"slices"
	"sync"
)

type Entry struct {
	Manifest plugin.Manifest
	New      pluginapi.Factory
}

var mu sync.RWMutex
var entries []Entry

func clone(e Entry) Entry {
	e.Manifest.Commands = slices.Clone(e.Manifest.Commands)
	e.Manifest.Events = slices.Clone(e.Manifest.Events)
	e.Manifest.Capabilities = slices.Clone(e.Manifest.Capabilities)
	e.Manifest.Args = slices.Clone(e.Manifest.Args)
	return e
}

// Register records an entry; full validation occurs before commands are registered.
func Register(manifest plugin.Manifest, factory pluginapi.Factory) {
	mu.Lock()
	defer mu.Unlock()
	entries = append(entries, clone(Entry{manifest, factory}))
}
func Entries() []Entry {
	mu.RLock()
	defer mu.RUnlock()
	result := make([]Entry, len(entries))
	for i, e := range entries {
		result[i] = clone(e)
	}
	return result
}
