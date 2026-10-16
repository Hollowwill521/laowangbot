// Package brand holds the customizable display name shown in user-visible
// output such as .status and .sysinfo. The name persists to brand.json under
// the app data dir and survives restarts.
package brand

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Default is the built-in display name.
const Default = "laowangbot"

// MaxLen caps the custom name length in runes.
const MaxLen = 32

var (
	mu   sync.RWMutex
	name = Default
	path string
)

// Init loads the persisted brand name from the app data dir.
// Call once at startup; a missing or invalid file keeps the default.
func Init(dataDir string) {
	p := filepath.Join(dataDir, "brand.json")
	mu.Lock()
	path = p
	mu.Unlock()
	data, err := os.ReadFile(p)
	if err != nil {
		return
	}
	var v struct {
		Name string `json:"name"`
	}
	if json.Unmarshal(data, &v) != nil {
		return
	}
	if n := normalize(v.Name); n != "" {
		mu.Lock()
		name = n
		mu.Unlock()
	}
}

// Get returns the current display name.
func Get() string {
	mu.RLock()
	defer mu.RUnlock()
	return name
}

// Set updates the display name and persists it.
// An empty name resets to Default. It returns the effective name.
func Set(n string) string {
	if nn := normalize(n); nn != "" {
		n = nn
	} else {
		n = Default
	}
	mu.Lock()
	name = n
	p := path
	mu.Unlock()
	if p != "" {
		data, err := json.Marshal(struct {
			Name string `json:"name"`
		}{Name: n})
		if err == nil {
			_ = os.WriteFile(p, append(data, '\n'), 0o600)
		}
	}
	return n
}

// normalize trims the name to a single line within MaxLen runes.
func normalize(s string) string {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", "")
	if r := []rune(s); len(r) > MaxLen {
		s = string(r[:MaxLen])
	}
	return strings.TrimSpace(s)
}
