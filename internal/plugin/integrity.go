package plugin

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
)

const hashMarker = ".catalog-hashes"

var ErrModified = errors.New("plugin has local modifications or no recorded baseline; use force to replace")

func reservedPath(name string) bool {
	for _, marker := range []string{remoteMarker, hashMarker} {
		if name == marker || len(name) > len(marker) && name[:len(marker)+1] == marker+"/" {
			return true
		}
	}
	return false
}
func rejectMetadata(dir string) error {
	for _, name := range []string{remoteMarker, hashMarker} {
		if _, err := os.Lstat(filepath.Join(dir, name)); !os.IsNotExist(err) {
			return errors.New("reserved catalog metadata in local plugin")
		}
	}
	return nil
}
func packageHashes(dir string) (map[string]string, error) {
	hashes := map[string]string{}
	err := filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		if reservedPath(filepath.ToSlash(rel)) {
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
		h := sha256.New()
		if rel == "manifest.json" {
			f, err := os.Open(p)
			if err != nil {
				return err
			}
			b, err := io.ReadAll(io.LimitReader(f, 65537))
			_ = f.Close()
			if err != nil {
				return err
			}
			if len(b) > 65536 {
				return errors.New("manifest too large")
			}
			var v any
			if err = json.Unmarshal(b, &v); err != nil {
				return err
			}
			b, err = json.Marshal(v)
			if err != nil {
				return err
			}
			_, _ = h.Write(b)
		} else {
			f, err := os.Open(p)
			if err != nil {
				return err
			}
			_, err = io.Copy(h, f)
			_ = f.Close()
			if err != nil {
				return err
			}
		}
		hashes[filepath.ToSlash(rel)] = hex.EncodeToString(h.Sum(nil))
		return nil
	})
	return hashes, err
}
func isModified(dir string) bool {
	b, err := os.ReadFile(filepath.Join(dir, hashMarker))
	if err != nil {
		return true
	}
	var baseline map[string]string
	if json.Unmarshal(b, &baseline) != nil || len(baseline) == 0 {
		return true
	}
	actual, err := packageHashes(dir)
	return err != nil || !reflect.DeepEqual(baseline, actual)
}
func checkRemoteUpdate(dir, catalog string, force bool) error {
	marker, err := os.ReadFile(filepath.Join(dir, remoteMarker))
	if err != nil || string(marker) != catalog {
		return errors.New("manual plugins cannot be remotely updated")
	}
	if !force && isModified(dir) {
		return ErrModified
	}
	return nil
}
