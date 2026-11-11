package internal_test

import (
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const modulePrefix = "github.com/hoangnecon/golabv2/internal/"

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot resolve caller path")
	}
	return filepath.Dir(filepath.Dir(file))
}

func collectInternalImports(t *testing.T, dir string) map[string][]string {
	t.Helper()
	fset := token.NewFileSet()
	result := make(map[string][]string)

	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		f, parseErr := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if parseErr != nil {
			return parseErr
		}
		var imps []string
		for _, imp := range f.Imports {
			val := strings.Trim(imp.Path.Value, `"`)
			if strings.HasPrefix(val, modulePrefix) {
				imps = append(imps, strings.TrimPrefix(val, modulePrefix))
			}
		}
		result[path] = imps
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", dir, err)
	}
	return result
}

func TestDomainZeroInternalImports(t *testing.T) {
	root := repoRoot(t)
	importsByFile := collectInternalImports(t, filepath.Join(root, "internal", "domain"))
	if len(importsByFile) == 0 {
		t.Fatal("expected Go files in internal/domain")
	}
	for file, imps := range importsByFile {
		if len(imps) > 0 {
			t.Errorf("Clean Architecture violation: %s imports internal package(s): %v", file, imps)
		}
	}
}

// CheckPackageAllowedImports enforces Clean Architecture internal import boundaries for a directory.
func CheckPackageAllowedImports(t *testing.T, relDir string, allowed func(imp string) bool) {
	t.Helper()
	root := repoRoot(t)
	target := filepath.Join(root, relDir)
	if _, err := os.Stat(target); err != nil {
		return
	}
	importsByFile := collectInternalImports(t, target)
	for file, imps := range importsByFile {
		for _, imp := range imps {
			if !allowed(imp) {
				t.Errorf("Clean Architecture violation in %s: forbidden import %q", file, modulePrefix+imp)
			}
		}
	}
}

// TestCleanArchitectureImports is Gate 1 (Spec v2 §3.2 & §8):
// enforces strict Clean Architecture layer boundaries across the entire repository.
func TestCleanArchitectureImports(t *testing.T) {
	root := repoRoot(t)

	// 1. Deleted legacy monolith files must never exist.
	for _, bannedFile := range []string{
		"internal/bridge/executor.go",
		"internal/store/db.go",
		"internal/service/terminal/service.go",
		"internal/mcpserver/tools_terminal.go",
	} {
		if _, err := os.Stat(filepath.Join(root, bannedFile)); err == nil {
			t.Errorf("Gate 1 violation: legacy file %s still exists", bannedFile)
		}
	}

	// 2. internal/domain: zero internal imports.
	CheckPackageAllowedImports(t, "internal/domain", func(imp string) bool {
		return false
	})

	// 3. internal/store/sqlite: imports ONLY internal/domain.
	CheckPackageAllowedImports(t, "internal/store", func(imp string) bool {
		return imp == "domain"
	})

	// 4. internal/process: imports ONLY internal/domain.
	CheckPackageAllowedImports(t, "internal/process", func(imp string) bool {
		return imp == "domain"
	})

	// 5. internal/bridge: imports ONLY internal/domain (or zero internal packages).
	CheckPackageAllowedImports(t, "internal/bridge", func(imp string) bool {
		return imp == "domain"
	})

	// 6. internal/driver/golab: imports ONLY internal/domain, internal/bridge, internal/driver/golab/scripts.
	CheckPackageAllowedImports(t, "internal/driver/golab", func(imp string) bool {
		return imp == "domain" || imp == "bridge" || imp == "driver/golab/scripts"
	})

	// 7. internal/session: imports ONLY internal/domain.
	CheckPackageAllowedImports(t, "internal/session", func(imp string) bool {
		return imp == "domain"
	})

	// 8. internal/service/**: imports ONLY internal/domain.
	CheckPackageAllowedImports(t, "internal/service", func(imp string) bool {
		return imp == "domain"
	})

	// 9. internal/api/**: imports ONLY internal/domain, internal/service/**, internal/api/**.
	CheckPackageAllowedImports(t, "internal/api", func(imp string) bool {
		return imp == "domain" ||
			imp == "service" || strings.HasPrefix(imp, "service/") ||
			imp == "api" || strings.HasPrefix(imp, "api/")
	})

	// 10. internal/mcpserver/**: imports ONLY internal/domain and internal/service/**.
	CheckPackageAllowedImports(t, "internal/mcpserver", func(imp string) bool {
		return imp == "domain" ||
			imp == "service" || strings.HasPrefix(imp, "service/")
	})

	// 11. cmd/server: composition via internal/app, internal/config, internal/mcpserver ONLY.
	CheckPackageAllowedImports(t, "cmd/server", func(imp string) bool {
		return imp == "app" || imp == "config" || imp == "mcpserver"
	})

	// 12. All cmd/** packages: never import internal/bridge directly (only internal/driver/golab and internal/bridge may).
	CheckPackageAllowedImports(t, "cmd", func(imp string) bool {
		return imp != "bridge" && !strings.HasPrefix(imp, "bridge/")
	})
}
