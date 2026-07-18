package architecture_test

import (
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

const (
	adapterModule = "github.com/Capsule7446/healix-rod"
	coreDomain    = "github.com/Capsule7446/healix-core/domain/"
)

func TestProductionDependencyBoundary(t *testing.T) {
	root := repositoryRoot(t)
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			name := entry.Name()
			if name == ".git" || name == "architecture" || name == "contract" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			return nil
		}
		parsed, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		for _, spec := range parsed.Imports {
			imported, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				t.Fatalf("unquote %s: %v", spec.Path.Value, err)
			}
			if allowedProductionImport(imported) {
				continue
			}
			t.Errorf("%s imports forbidden dependency %q", filepath.ToSlash(rel), imported)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func allowedProductionImport(imported string) bool {
	first := strings.Split(imported, "/")[0]
	if !strings.Contains(first, ".") {
		return true
	}
	return imported == adapterModule || strings.HasPrefix(imported, adapterModule+"/") ||
		strings.HasPrefix(imported, coreDomain) ||
		imported == "github.com/go-rod/rod" || strings.HasPrefix(imported, "github.com/go-rod/rod/") ||
		imported == "github.com/ysmood/gson"
}

func TestNoHealixHostDependency(t *testing.T) {
	root := repositoryRoot(t)
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") {
			return nil
		}
		raw, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, spec := range raw.Imports {
			imported, _ := strconv.Unquote(spec.Path.Value)
			if imported == "github.com/tt-win/healix" || strings.HasPrefix(imported, "github.com/tt-win/healix/") {
				rel, _ := filepath.Rel(root, path)
				t.Errorf("%s depends on Healix host package %q", filepath.ToSlash(rel), imported)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve architecture test path")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), ".."))
}
