package logging_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// allowedStdlibLogCalls lists the few call names still permitted per file.
// Everything else must go through internal/logging so its records carry a
// level and a tag — a bare log.Printf lands in the file as INFO [app], which
// is exactly the undifferentiated stream this package exists to replace.
//
// log.Fatal in the relay's main is deliberate: the fail-closed startup checks
// (AGENTS.md red line #9) must stop the process, and by then the stdlib logger
// is already routed through logging.StdlibWriter, so the message is formatted
// like everything else on its way out.
var allowedStdlibLogCalls = map[string]map[string]bool{
	"cmd/atterm-relay/main.go": {"Fatal": true, "Fatalf": true},
}

// TestNoBareStdlibLogCalls walks the repo and fails on log.Print* outside the
// allow list.
func TestNoBareStdlibLogCalls(t *testing.T) {
	root := repoRoot(t)
	fset := token.NewFileSet()

	var offenders []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			switch info.Name() {
			case "node_modules", ".git", "dist", "build", "vendor", ".claude":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		rel = filepath.ToSlash(rel)
		allowed := allowedStdlibLogCalls[rel]

		file, parseErr := parser.ParseFile(fset, path, nil, 0)
		if parseErr != nil {
			// A file we cannot parse is not this test's problem; the build
			// will report it far more clearly.
			return nil
		}
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			pkg, ok := sel.X.(*ast.Ident)
			if !ok || pkg.Name != "log" {
				return true
			}
			switch sel.Sel.Name {
			case "Print", "Printf", "Println", "Fatal", "Fatalf", "Fatalln", "Panic", "Panicf", "Panicln":
				if allowed[sel.Sel.Name] {
					return true
				}
				pos := fset.Position(call.Pos())
				offenders = append(offenders,
					rel+":"+itoa(pos.Line)+": log."+sel.Sel.Name)
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}

	if len(offenders) > 0 {
		t.Fatalf("bare stdlib log calls found — use internal/logging (or the "+
			"desktop logDebug/logInfo/logWarn/logError aliases) so the record "+
			"carries a level and a tag:\n  %s", strings.Join(offenders, "\n  "))
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

// repoRoot walks up from the test's working directory until it finds go.mod.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found above the test directory")
		}
		dir = parent
	}
}
