package plugin

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

const MaxPackageSize = 32 << 20
const maxPackageFiles = 129 // The manifest and up to 128 payload files.

type InstalledInfo struct {
	Manifest   Manifest
	Source     string
	Modified   bool
	Error      string
	UpdateTime time.Time
}

// Search searches the fixed project catalog by name, description and commands.
func (m Manager) Search(ctx context.Context, query string) ([]CatalogEntry, error) {
	return m.search(ctx, &http.Client{Timeout: 30 * time.Second}, CatalogURL, query, true)
}
func (m Manager) search(ctx context.Context, client *http.Client, catalogURL, query string, restricted bool) ([]CatalogEntry, error) {
	catalog, err := fetchCatalog(ctx, client, catalogURL, restricted)
	if err != nil {
		return nil, err
	}
	query = strings.ToLower(strings.TrimSpace(query))
	out := []CatalogEntry{}
	for _, entry := range catalog.Plugins {
		haystack := entry.Manifest.Name + " " + entry.Manifest.Description + " " + strings.Join(entry.Manifest.Commands, " ")
		if strings.Contains(strings.ToLower(haystack), query) {
			out = append(out, entry)
		}
	}
	return out, nil
}
func fetchCatalog(ctx context.Context, client *http.Client, catalogURL string, restricted bool) (Catalog, error) {
	var catalog Catalog
	b, err := download(ctx, client, catalogURL, 1<<20, restricted)
	if err != nil {
		return catalog, err
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err = dec.Decode(&catalog); err != nil {
		return catalog, err
	}
	var extra any
	if dec.Decode(&extra) != io.EOF {
		return catalog, errors.New("catalog has trailing data")
	}
	names := map[string]bool{}
	for _, entry := range catalog.Plugins {
		if err = validate(entry.Manifest); err != nil {
			return catalog, err
		}
		if names[entry.Manifest.Name] {
			return catalog, errors.New("duplicate catalog entry")
		}
		names[entry.Manifest.Name] = true
		if len(entry.Platforms) == 0 {
			if err = validateCatalogFiles(entry.Manifest, entry.Files, restricted); err != nil {
				return catalog, err
			}
		} else {
			for target, p := range entry.Platforms {
				if !validTarget(target) {
					return catalog, errors.New("unknown plugin platform")
				}
				v := entry.Manifest
				v.Executable = p.Executable
				if err = validate(v); err != nil {
					return catalog, err
				}
				if err = validateCatalogFiles(v, p.Files, restricted); err != nil {
					return catalog, err
				}
			}
		}
	}
	return catalog, nil
}

func (m Manager) Installed() ([]InstalledInfo, error) {
	installMu.Lock()
	defer installMu.Unlock()
	lock, err := m.transactionLock()
	if err != nil {
		return nil, err
	}
	defer lock.Close()
	entries, err := os.ReadDir(filepath.Join(m.Root, "plugins"))
	if os.IsNotExist(err) {
		return []InstalledInfo{}, nil
	}
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".previous-") {
			if err = m.recover(strings.TrimPrefix(entry.Name(), ".previous-")); err != nil {
				return nil, err
			}
		}
	}
	entries, err = os.ReadDir(filepath.Join(m.Root, "plugins"))
	if err != nil {
		return nil, err
	}
	out := make([]InstalledInfo, 0, len(entries))
	for _, installed := range entries {
		if strings.HasPrefix(installed.Name(), ".") {
			continue
		}
		dir, err := m.directory(installed.Name())
		if err != nil {
			return nil, err
		}
		manifest, manifestErr := readManifestDefinition(dir)
		if manifestErr == nil && manifest.Name != installed.Name() {
			manifestErr = errors.New("manifest name does not match directory")
		}
		entry := InstalledInfo{Manifest: manifest, Source: "manual"}
		if manifestErr != nil {
			entry.Manifest = Manifest{Name: installed.Name()}
			entry.Error = manifestErr.Error()
			entry.Modified = true
		}
		if marker, err := os.ReadFile(filepath.Join(dir, remoteMarker)); err == nil {
			entry.Source = string(marker)
			entry.Modified = entry.Modified || isModified(dir)
		}
		if info, err := os.Stat(dir); err == nil {
			entry.UpdateTime = info.ModTime()
		}
		out = append(out, entry)
	}
	return out, nil
}

// Remove removes installed code only; state and existing snapshots are retained.
func (m Manager) Remove(name string) error {
	installMu.Lock()
	defer installMu.Unlock()
	lock, err := m.transactionLock()
	if err != nil {
		return err
	}
	defer lock.Close()
	dir, err := m.directory(name)
	if err != nil {
		return err
	}
	if err = m.recover(name); err != nil {
		return err
	}
	if _, err = os.Lstat(dir); err != nil {
		return err
	}
	trash, err := os.MkdirTemp(filepath.Dir(dir), ".removed-")
	if err != nil {
		return err
	}
	if err = os.Remove(trash); err != nil {
		return err
	}
	if err = os.Rename(dir, trash); err != nil {
		return err
	}
	return os.RemoveAll(trash)
}

// Export emits a portable manual package without catalog ownership metadata.
func (m Manager) Export(name string) ([]byte, error) {
	installMu.Lock()
	defer installMu.Unlock()
	lock, err := m.transactionLock()
	if err != nil {
		return nil, err
	}
	defer lock.Close()
	if _, err = m.load(name); err != nil {
		return nil, err
	}
	dir, err := m.directory(name)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	var total int64
	count := 0
	err = filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if reservedPath(rel) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			return errors.New("package symlink rejected")
		}
		if d.IsDir() {
			return nil
		}
		if !d.Type().IsRegular() {
			return errors.New("package non-regular file rejected")
		}
		count++
		if count > maxPackageFiles {
			return errors.New("too many package files")
		}
		f, err := os.Open(p)
		if err != nil {
			return err
		}
		defer f.Close()
		info, err := f.Stat()
		if err != nil {
			return err
		}
		if info.Size() > int64(MaxPackageSize)-total {
			return errors.New("package exceeds 32 MiB")
		}
		h, err := zip.FileInfoHeader(info)
		if err != nil {
			return err
		}
		h.Name = rel
		h.Method = zip.Deflate
		dest, err := zw.CreateHeader(h)
		if err != nil {
			return err
		}
		n, err := io.Copy(dest, io.LimitReader(f, int64(MaxPackageSize)-total+1))
		total += n
		if total > MaxPackageSize {
			return errors.New("package exceeds 32 MiB")
		}
		return err
	})
	closeErr := zw.Close()
	if err != nil {
		return nil, err
	}
	if closeErr != nil {
		return nil, closeErr
	}
	return buf.Bytes(), nil
}

func safePackagePath(name string) bool {
	if name == "" || strings.ContainsAny(name, "\\\x00:") || !filepath.IsLocal(name) || strings.HasPrefix(name, "/") {
		return false
	}
	for _, part := range strings.Split(name, "/") {
		if part == ".." || part == "." {
			return false
		}
	}
	return true
}

// ImportPackage validates and stages ZIP contents without executing any code.
func (m Manager) ImportPackage(data []byte, replace bool) (string, error) {
	if len(data) > MaxPackageSize+(1<<20) {
		return "", errors.New("compressed package too large")
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", err
	}
	if len(zr.File) == 0 || len(zr.File) > 256 {
		return "", errors.New("invalid ZIP entry count")
	}
	stage, err := os.MkdirTemp("", "laowangbot-import-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(stage)
	seen := map[string]bool{}
	var total uint64
	count := 0
	for _, f := range zr.File {
		name := strings.TrimSuffix(f.Name, "/")
		clean := path.Clean(name)
		if !safePackagePath(name) || reservedPath(clean) || seen[clean] {
			return "", fmt.Errorf("invalid or duplicate ZIP path %q", f.Name)
		}
		seen[clean] = true
		if f.Mode()&os.ModeSymlink != 0 || !f.Mode().IsRegular() && !f.FileInfo().IsDir() {
			return "", errors.New("non-regular ZIP entry")
		}
		dest := filepath.Join(stage, filepath.FromSlash(clean))
		if f.FileInfo().IsDir() {
			if err = os.MkdirAll(dest, 0700); err != nil {
				return "", err
			}
			continue
		}
		count++
		if count > maxPackageFiles || f.UncompressedSize64 > MaxPackageSize-total {
			return "", errors.New("package size or file count exceeded")
		}
		total += f.UncompressedSize64
		if err = os.MkdirAll(filepath.Dir(dest), 0700); err != nil {
			return "", err
		}
		src, err := f.Open()
		if err != nil {
			return "", err
		}
		b, readErr := io.ReadAll(io.LimitReader(src, int64(f.UncompressedSize64)+1))
		closeErr := src.Close()
		if readErr != nil {
			return "", readErr
		}
		if closeErr != nil {
			return "", closeErr
		}
		if uint64(len(b)) != f.UncompressedSize64 {
			return "", errors.New("ZIP entry size mismatch")
		}
		mode := os.FileMode(0600)
		if f.Mode()&0111 != 0 {
			mode = 0700
		}
		if err = os.WriteFile(dest, b, mode); err != nil {
			return "", err
		}
	}
	manifest, err := readManifest(stage)
	if err != nil {
		return "", err
	}
	if err = m.install(stage, replace); err != nil {
		return "", err
	}
	return manifest.Name, nil
}

func validTarget(s string) bool {
	switch s {
	case "linux/amd64", "linux/arm64", "darwin/amd64", "darwin/arm64", "windows/amd64", "windows/arm64":
		return true
	}
	return false
}
func validateCatalogFiles(v Manifest, files []RemoteFile, restricted bool) error {
	if len(files) == 0 || len(files) > 128 {
		return errors.New("invalid file count")
	}
	paths := map[string]bool{}
	for _, file := range files {
		clean := path.Clean(file.Path)
		if !safePackagePath(file.Path) || clean == "manifest.json" || reservedPath(clean) || paths[clean] {
			return errors.New("invalid or duplicate remote path")
		}
		paths[clean] = true
		expected, err := hex.DecodeString(file.SHA256)
		if err != nil || len(expected) != 32 {
			return errors.New("invalid sha256")
		}
		if restricted && !trustedURL(file.URL) {
			return errors.New("download outside current project")
		}
	}
	if v.ProtocolVersion == 2 {
		for p := range paths {
			if strings.HasSuffix(p, ".go") {
				return nil
			}
		}
		return errors.New("catalog Go source missing")
	}
	if !paths[filepath.ToSlash(filepath.Clean(v.Executable))] {
		return errors.New("catalog executable missing")
	}
	return nil
}
