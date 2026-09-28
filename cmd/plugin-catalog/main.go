// Generate a release-pinned catalog from actual Go source package bytes.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

type catalog struct {
	Plugins []entry `json:"plugins"`
}
type entry struct {
	Manifest json.RawMessage `json:"manifest"`
	Files    []remoteFile    `json:"files"`
}
type remoteFile struct {
	Path   string `json:"path"`
	URL    string `json:"url"`
	SHA256 string `json:"sha256"`
}

func main() {
	tag := flag.String("tag", "", "release tag matching the published source commit")
	flag.String("dist", "dist", "deprecated compatibility flag; source files are read from the repository")
	out := flag.String("output", "dist/plugins-catalog.json", "output catalog; use plugins/catalog.json to update the tracked catalog")
	flag.Parse()
	c, err := generate(".", *tag)
	if err == nil {
		err = writeCatalog(*out, c)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func writeCatalog(output string, c catalog) error {
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(output), 0755); err != nil {
		return err
	}
	return os.WriteFile(output, append(b, '\n'), 0644)
}
func generate(root, tag string) (catalog, error) {
	c := catalog{Plugins: []entry{}}
	if !regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+(?:[-.a-zA-Z0-9]*)?$`).MatchString(tag) {
		return c, fmt.Errorf("invalid release tag: %q", tag)
	}
	dirs := []string{"examples/plugins/echo"}
	children, err := os.ReadDir(filepath.Join(root, "plugins"))
	if err != nil {
		return c, err
	}
	for _, child := range children {
		if child.IsDir() && !strings.HasPrefix(child.Name(), ".") {
			dirs = append(dirs, filepath.ToSlash(filepath.Join("plugins", child.Name())))
		}
	}
	names := map[string]bool{}
	for _, dir := range dirs {
		e, name, err := sourceEntry(root, dir, tag)
		if err != nil {
			return c, err
		}
		if name == "" {
			continue
		}
		if names[name] {
			return c, fmt.Errorf("duplicate plugin name: %s", name)
		}
		names[name] = true
		c.Plugins = append(c.Plugins, e)
	}
	return c, nil
}
func sourceEntry(root, dir, tag string) (entry, string, error) {
	var e entry
	base := filepath.Join(root, filepath.FromSlash(dir))
	raw, err := os.ReadFile(filepath.Join(base, "manifest.json"))
	if err != nil {
		return e, "", err
	}
	var m struct {
		Name     string `json:"name"`
		Protocol int    `json:"protocol_version"`
		Package  string `json:"package"`
	}
	if err = json.Unmarshal(raw, &m); err != nil {
		return e, "", err
	}
	if m.Protocol != 2 {
		fmt.Fprintf(os.Stderr, "skip %s: protocol_version=%d; migrate to Go source protocol 2 before publishing\n", dir, m.Protocol)
		return e, "", nil
	}
	if !regexp.MustCompile(`^[a-z][a-z0-9_-]{0,63}$`).MatchString(m.Name) || m.Package != "." {
		return e, "", fmt.Errorf("invalid source manifest: %s", dir)
	}
	var fields map[string]json.RawMessage
	if err = json.Unmarshal(raw, &fields); err != nil {
		return e, "", err
	}
	for _, key := range []string{"executable", "persistent", "args"} {
		if _, ok := fields[key]; ok {
			return e, "", fmt.Errorf("%s: legacy field %s", dir, key)
		}
	}
	e.Manifest = raw
	hasGo := false
	err = filepath.WalkDir(base, func(file string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if file == base {
			return nil
		}
		if strings.HasPrefix(d.Name(), ".") {
			return fmt.Errorf("hidden package entry is not publishable: %s", file)
		}
		if d.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlink is not publishable: %s", file)
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("nonregular package file: %s", file)
		}
		if d.Name() == "go.mod" || d.Name() == "go.work" || d.Name() == "go.sum" || d.Name() == "go.work.sum" {
			return fmt.Errorf("plugin dependencies must be pinned by host: %s", file)
		}
		rel, err := filepath.Rel(base, file)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if rel == "manifest.json" {
			return nil
		}
		if filepath.Dir(rel) == "." && strings.HasSuffix(rel, ".go") && !strings.HasSuffix(rel, "_test.go") {
			hasGo = true
		}
		b, err := os.ReadFile(file)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(b)
		parts := strings.Split(dir+"/"+rel, "/")
		for i := range parts {
			parts[i] = url.PathEscape(parts[i])
		}
		e.Files = append(e.Files, remoteFile{Path: rel, URL: "https://raw.githubusercontent.com/OrionG-hub/laowangbot/" + tag + "/" + strings.Join(parts, "/"), SHA256: hex.EncodeToString(sum[:])})
		return nil
	})
	if err != nil {
		return e, "", err
	}
	if !hasGo {
		return e, "", fmt.Errorf("plugin has no root Go source: %s", dir)
	}
	return e, m.Name, nil
}
