package llm

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The service layer must never print. Callers own the degradation message, which
// is what lets one task serve the CLI, the TUI, and a test without three copies
// of the wording — and it is why the old implementation's bare fmt.Printf inside
// the service was a defect worth a rule.
//
// The rule is asserted by scanning source rather than trusted to review, because
// a stray Printf is exactly the kind of thing that survives review.
func TestPackageDoesNotWriteToStdio(t *testing.T) {
	// Two files are exempt by design, both because terminal I/O is their entire
	// job rather than an incidental print:
	//   - prompt_cli.go renders the gate to a caller-supplied io.Writer,
	//     defaulting to stdout.
	//   - editor.go hands the terminal to $EDITOR, which cannot work without
	//     inheriting stdio.
	// Everything else — assembly, the service, the state machine — must stay
	// silent so callers own the wording.
	exempt := map[string]bool{
		"prompt_cli.go": true,
		"editor.go":     true,
	}

	banned := []string{
		"fmt.Print", "fmt.Printf", "fmt.Println",
		"os.Stdout", "os.Stderr",
		"print", "println",
	}

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("reading package dir: %v", err)
	}

	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") {
			continue
		}
		if strings.HasSuffix(name, "_test.go") || exempt[name] {
			continue
		}

		src, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("reading %s: %v", name, err)
		}

		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, name, src, 0)
		if err != nil {
			t.Fatalf("parsing %s: %v", name, err)
		}

		ast.Inspect(file, func(n ast.Node) bool {
			expr, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			ident, ok := expr.X.(*ast.Ident)
			if !ok {
				return true
			}
			qualified := ident.Name + "." + expr.Sel.Name
			for _, b := range banned {
				if qualified == b {
					pos := fset.Position(expr.Pos())
					t.Errorf("%s:%d: %s writes to stdio — the service layer must return errors and let callers decide how to degrade",
						name, pos.Line, qualified)
				}
			}
			return true
		})
	}
}

// EditInEditor is the one place the package shells out; it must not be reachable
// from the service path, only from the gate. This keeps the "no side effects in
// assembly" property honest.
func TestPromptAssemblyFilesHaveNoExec(t *testing.T) {
	for _, name := range []string{"task.go", "service.go"} {
		src, err := os.ReadFile(filepath.Clean(name))
		if err != nil {
			t.Fatalf("reading %s: %v", name, err)
		}
		if strings.Contains(string(src), "os/exec") {
			t.Errorf("%s imports os/exec — prompt assembly and the service must stay free of subprocesses", name)
		}
	}
}
