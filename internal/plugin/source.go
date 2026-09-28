package plugin

import (
	"errors"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
)

// SourceManifest validates a portable source package before a build is attempted.
func SourceManifest(dir string) (Manifest, error) {
	m, err := readManifest(dir)
	if err != nil {
		return m, err
	}
	if m.ProtocolVersion != 2 {
		return m, errors.New("旧独立进程插件包不再支持；请迁移为 protocol_version=2 的 Go 源码包")
	}
	return m, nil
}

func validateSourceTree(dir string) error {
	found := false
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		switch d.Name() {
		case "go.mod", "go.sum", "go.work", "go.work.sum":
			return errors.New("source plugins use the host Go module; nested module/workspace files are not supported")
		}
		if filepath.Dir(p) == dir && strings.HasSuffix(p, ".go") && !strings.HasSuffix(p, "_test.go") {
			f, e := parser.ParseFile(token.NewFileSet(), p, nil, parser.PackageClauseOnly)
			if e != nil {
				return e
			}
			if f.Name.Name == "main" {
				return errors.New("source plugin must be an importable Go package, not main")
			}
			found = true
		}
		return nil
	})
	if err != nil {
		return err
	}
	if !found {
		return errors.New("source package has no root Go files")
	}
	return nil
}
