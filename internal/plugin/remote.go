package plugin

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const CatalogURL = "https://raw.githubusercontent.com/OrionG-hub/laowangbot/master/plugins/catalog.json"
const remoteMarker = ".catalog-source"

type Catalog struct {
	Plugins []CatalogEntry `json:"plugins"`
}
type CatalogEntry struct {
	Manifest Manifest     `json:"manifest"`
	Files    []RemoteFile `json:"files"`
}
type RemoteFile struct {
	Path   string `json:"path"`
	URL    string `json:"url"`
	SHA256 string `json:"sha256"`
}

func trustedURL(raw string) bool {
	u, e := url.Parse(raw)
	return e == nil && u.Scheme == "https" && u.Host == "raw.githubusercontent.com" && u.User == nil && strings.HasPrefix(u.Path, "/OrionG-hub/laowangbot/")
}
func download(ctx context.Context, c *http.Client, raw string, max int64, restricted bool) ([]byte, error) {
	if restricted && !trustedURL(raw) {
		return nil, errors.New("download outside current project")
	}
	client := *c
	previous := client.CheckRedirect
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return errors.New("too many redirects")
		}
		if restricted && !trustedURL(req.URL.String()) {
			return errors.New("redirect outside current project")
		}
		if previous != nil {
			return previous(req, via)
		}
		return nil
	}
	req, e := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if e != nil {
		return nil, e
	}
	res, e := client.Do(req)
	if e != nil {
		return nil, e
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return nil, fmt.Errorf("download status %d", res.StatusCode)
	}
	b, e := io.ReadAll(io.LimitReader(res.Body, max+1))
	if int64(len(b)) > max {
		return nil, errors.New("download too large")
	}
	return b, e
}
func (m Manager) InstallRemote(ctx context.Context, name string) error {
	return m.installRemote(ctx, &http.Client{Timeout: 30 * time.Second}, CatalogURL, name, false, true)
}
func (m Manager) UpdateRemote(ctx context.Context, name string) error {
	return m.installRemote(ctx, &http.Client{Timeout: 30 * time.Second}, CatalogURL, name, true, true)
}
func (m Manager) installRemote(ctx context.Context, client *http.Client, catalogURL, name string, update, restricted bool) error {
	dir, e := m.directory(name)
	if e != nil {
		return e
	}
	if update {
		marker, e := os.ReadFile(filepath.Join(dir, remoteMarker))
		if e != nil || string(marker) != catalogURL {
			return errors.New("manual plugins cannot be remotely updated")
		}
	}
	b, e := download(ctx, client, catalogURL, 1<<20, restricted)
	if e != nil {
		return e
	}
	var catalog Catalog
	if e = json.Unmarshal(b, &catalog); e != nil {
		return e
	}
	var entry *CatalogEntry
	for i := range catalog.Plugins {
		if catalog.Plugins[i].Manifest.Name == name {
			if entry != nil {
				return errors.New("duplicate catalog entry")
			}
			entry = &catalog.Plugins[i]
		}
	}
	if entry == nil {
		return errors.New("plugin absent from current project catalog")
	}
	if e = validate(entry.Manifest); e != nil {
		return e
	}
	if len(entry.Files) == 0 || len(entry.Files) > 128 {
		return errors.New("invalid file count")
	}
	stage, e := os.MkdirTemp("", "laowangbot-plugin-")
	if e != nil {
		return e
	}
	defer os.RemoveAll(stage)
	seen := map[string]bool{}
	var total int
	for _, file := range entry.Files {
		if !filepath.IsLocal(file.Path) || strings.Contains(file.Path, "\\") || filepath.Clean(file.Path) == "manifest.json" || filepath.Clean(file.Path) == remoteMarker || seen[filepath.Clean(file.Path)] {
			return errors.New("invalid or duplicate remote path")
		}
		seen[filepath.Clean(file.Path)] = true
		expected, e := hex.DecodeString(file.SHA256)
		if e != nil || len(expected) != 32 {
			return errors.New("invalid sha256")
		}
		data, e := download(ctx, client, file.URL, 16<<20, restricted)
		if e != nil {
			return e
		}
		total += len(data)
		if total > 32<<20 {
			return errors.New("plugin too large")
		}
		sum := sha256.Sum256(data)
		if !strings.EqualFold(hex.EncodeToString(sum[:]), file.SHA256) {
			return errors.New("sha256 mismatch")
		}
		dest := filepath.Join(stage, file.Path)
		if e = os.MkdirAll(filepath.Dir(dest), 0700); e != nil {
			return e
		}
		if e = os.WriteFile(dest, data, 0700); e != nil {
			return e
		}
	}
	b, e = json.Marshal(entry.Manifest)
	if e != nil {
		return e
	}
	if e = os.WriteFile(filepath.Join(stage, "manifest.json"), b, 0600); e != nil {
		return e
	}
	if e = os.WriteFile(filepath.Join(stage, remoteMarker), []byte(catalogURL), 0600); e != nil {
		return e
	}
	return m.installOwned(stage, update, catalogURL)
}
