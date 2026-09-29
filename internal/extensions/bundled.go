package extensions

import (
	"encoding/json"
	"strings"

	"github.com/OrionG-hub/laowangbot/internal/app"
	"github.com/OrionG-hub/laowangbot/internal/command"
	"github.com/OrionG-hub/laowangbot/internal/compiled"
	"github.com/OrionG-hub/laowangbot/internal/plugin"
	"github.com/OrionG-hub/laowangbot/pkg/pluginapi"
	"github.com/OrionG-hub/laowangbot/plugins/bh"
	"github.com/OrionG-hub/laowangbot/plugins/monitor"
	"github.com/OrionG-hub/laowangbot/plugins/pmcaptcha"
	"github.com/OrionG-hub/laowangbot/plugins/qdsg"
)

const bundledSource = "builtin"

// Existing external implementations retain their own help behavior.
func registerBundledHelp(a *app.App, external []compiled.Entry) {
	custom := map[string]bool{}
	for _, entry := range external {
		custom[entry.Manifest.Name] = true
	}
	for name, help := range map[string]string{"monitor": monitor.Help, "qdsg": command.Escape(qdsg.Help), "bh": command.Escape(bh.Help)} {
		if custom[name] {
			continue
		}
		if c, ok := a.Registry.Lookup(name); ok {
			c.Help = func(prefix string) string {
				return strings.ReplaceAll(help, "."+name, command.Escape(prefix+name))
			}
		}
	}
	if !custom["pmcaptcha"] {
		for _, name := range []string{"pmc", "pmcaptcha"} {
			if c, ok := a.Registry.Lookup(name); ok {
				c.Help = func(prefix string) string { return command.Escape(pmcaptcha.HelpText(prefix, "")) }
			}
		}
	}
}

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
		{bh.ManifestJSON(), bh.Open},
		{pmcaptcha.ManifestJSON(), pmcaptcha.Open},
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
