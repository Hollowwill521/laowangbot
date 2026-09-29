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
	for name, help := range map[string]string{"monitor": monitor.Help, "qdsg": qdsg.Help, "bh": bh.Help} {
		if custom[name] {
			continue
		}
		if c, ok := a.Registry.Lookup(name); ok {
			c.Help = func(prefix string) string {
				return bundledHelp(prefix, name, help)
			}
		}
	}
	if !custom["pmcaptcha"] {
		for _, name := range []string{"pmcaptcha"} {
			if c, ok := a.Registry.Lookup(name); ok {
				c.Help = func(prefix string) string { return bundledHelp(prefix, "pmcaptcha", pmcaptcha.HelpText(".", "")) }
			}
		}
	}
}

func bundledHelp(prefix, name, text string) string {
	if name == "qdsg" || name == "bh" || name == "pmc" || name == "pmcaptcha" {
		return pluginapi.PanelHTML(text, name, prefix)
	}
	if name == "monitor" {
		return strings.ReplaceAll(text, ".monitor", command.Escape(prefix+"monitor"))
	}
	text = strings.ReplaceAll(text, "."+name, prefix+name)
	return command.PlainHelp(text, prefix, name)
}

// Only known bundled help text is formatted; arbitrary plugin output stays plain.
func pluginHelpHTML(text, prefix string) (string, bool) {
	for name, help := range map[string]string{"qdsg": qdsg.Help, "bh": bh.Help} {
		if strings.Contains(text, help) {
			before, after, _ := strings.Cut(text, help)
			return command.Escape(before) + bundledHelp(prefix, name, help) + command.Escape(after), true
		}
	}
	for _, section := range []string{"", "basic", "captcha", "set", "wl", "record"} {
		help := pmcaptcha.HelpText(".", section)
		if strings.Contains(text, help) {
			before, after, _ := strings.Cut(text, help)
			return command.Escape(before) + bundledHelp(prefix, "pmcaptcha", help) + command.Escape(after), true
		}
	}
	return "", false
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
