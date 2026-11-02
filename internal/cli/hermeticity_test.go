package cli

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRootCommandTestsRedirectDataDir is a hermeticity guard.
//
// Constructing a root command runs PersistentPreRunE, which probes
// <DataDir>/upgrade-in-progress.json. Before the stale-sentinel fix
// (#141) that probe was read-only, so a test that built a root command
// against the developer's real $HOME was merely reading their state.
//
// After the fix the same probe can DELETE a stale sentinel. That turned
// a pre-existing test-hygiene wart into a real hazard: running
// `go test ./...` on a machine with a stale upgrade marker silently
// removed that marker — state the user might have wanted to inspect.
//
// The fix is that any test constructing a root command must redirect
// the data dir first. This guard enforces that mechanically, so the
// next person to add such a test finds out immediately rather than
// discovering it by deleting someone's file.
//
// We parse the AST rather than grepping the source: a textual check
// would be satisfied by the word "redirectSentinelDataDir" appearing
// in a comment or an error message, which is exactly the kind of false
// negative that lets a real violation through.
func TestRootCommandTestsRedirectDataDir(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read cli package dir: %v", err)
	}

	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, "_test.go") {
			continue
		}
		// This file is the guard itself; it names NewRootCmd in the
		// strings it searches for.
		if name == "hermeticity_test.go" {
			continue
		}

		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, filepath.Join(".", name), nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}

		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}

			buildsRoot := false
			redirects := false

			ast.Inspect(fn.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				switch calleeName(call.Fun) {
				case "NewRootCmd":
					buildsRoot = true
				case "redirectSentinelDataDir", "redirectCleanupDataDir":
					redirects = true
				}
				return true
			})

			if buildsRoot && !redirects {
				t.Errorf("%s:%d: %s constructs a root command (NewRootCmd) but never redirects the data dir.\n"+
					"PersistentPreRunE reads <DataDir>/upgrade-in-progress.json and, since the\n"+
					"stale-sentinel fix, can DELETE it. Add redirectSentinelDataDir(t) at the\n"+
					"top of the test so it never touches the developer's real nodeup state.",
					name, fset.Position(fn.Pos()).Line, fn.Name.Name)
			}
		}
	}
}

// calleeName returns the bare name of a called function, or "" for
// anything that isn't a plain identifier call (a method call, a
// selector on a package, etc.).
func calleeName(fun ast.Expr) string {
	switch f := fun.(type) {
	case *ast.Ident:
		return f.Name
	case *ast.SelectorExpr:
		// pkg.Func() — the interesting case is a method on a value
		// (cmd.Func()), where the selector name alone would be a
		// false positive. We only accept a selector whose receiver is
		// a package-ish identifier we don't recognize as a value.
		if id, ok := f.X.(*ast.Ident); ok {
			return id.Name + "." + f.Sel.Name
		}
		return f.Sel.Name
	}
	return ""
}
