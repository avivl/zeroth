package logging

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Policy is I/O-free (ADR-Z-0001). A Zap global, or any import that reads disk,
// network, or the environment, would violate that isolation.
func TestPolicyPackageHasNoIOImports(t *testing.T) {
	t.Parallel()
	dir := filepath.Join("..", "policy")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read policy: %v", err)
	}

	denied := []string{
		"os",
		"net",
		"net/http",
		"os/exec",
		"database/sql",
		"log",
		"go.uber.org/zap",
		"github.com/spf13/viper",
		"github.com/spf13/cobra",
		"github.com/failsafe-go/failsafe-go",
		"github.com/avivl/zeroth/internal/logging",
		"github.com/avivl/zeroth/internal/resilience",
		"github.com/avivl/zeroth/internal/store",
	}

	fset := token.NewFileSet()
	found := 0
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		found++
		path := filepath.Join(dir, name)
		file, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		for _, imp := range file.Imports {
			pkg := strings.Trim(imp.Path.Value, `"`)
			for _, d := range denied {
				if pkg == d || strings.HasPrefix(pkg, d+"/") {
					t.Errorf("%s imports %q; internal/policy must stay I/O-free (inject a logger from the caller if one is needed)", path, pkg)
				}
			}
		}
	}
	if found == 0 {
		t.Fatal("no policy Go files found")
	}
}
