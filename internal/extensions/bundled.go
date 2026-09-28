package extensions

import (
	"encoding/json"

	"github.com/OrionG-hub/laowangbot/internal/compiled"
	"github.com/OrionG-hub/laowangbot/internal/plugin"
	"github.com/OrionG-hub/laowangbot/pkg/pluginapi"
	"github.com/OrionG-hub/laowangbot/plugins/monitor"
	"github.com/OrionG-hub/laowangbot/plugins/qdsg"
)

const bundledSource = "builtin"

// Bundled implementations are separate from the external compiled registry:
// their presence must never force an official binary update to build from source.
// Existing linked source plugins take precedence to preserve custom versions.
func bundledEntries(external []compiled.Entry) []compiled.Entry {
	entries := append([]compiled.Entry(nil), external...)
	present := map[string]bool{}
	for _, entry := range external {
		present[entry.Manifest.Name] = true
	}
	for _, built := range []struct {
		manifest string
		factory  pluginapi.Factory
	}{
		{monitor.ManifestJSON(), monitor.Open},
		{qdsg.ManifestJSON(), qdsg.Open},
	} {
		var manifest plugin.Manifest
		if err := json.Unmarshal([]byte(built.manifest), &manifest); err != nil {
			panic("invalid bundled plugin manifest: " + err.Error())
		}
		if !present[manifest.Name] {
			entries = append(entries, compiled.Entry{Manifest: manifest, New: built.factory})
		}
	}
	return entries
}
