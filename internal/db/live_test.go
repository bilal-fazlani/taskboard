package db

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// rawReadAllowed names the functions that may read projects, tickets, epics,
// documents, subtasks or entries straight from the table, deleted projects'
// rows included, and why. Everything else goes through the live sources in
// live.go.
var rawReadAllowed = map[string]string{
	"checkPrefixFree":    "a deleted project keeps its prefix, so a new one must not take it",
	"fillDocumentSearch": "keeps every document's search text current, deleted projects' too",
}

// rawRead matches a read of one of the tables live.go covers: FROM or JOIN
// with the table's name. A DELETE FROM is a write and is not matched; writes
// are not checked here (see live.go).
var rawRead = regexp.MustCompile(`(?i)(DELETE\s+)?\b(FROM|JOIN)\s+(projects|tickets|epics|documents|subtasks|entries)\b`)

// TestReadsGoThroughLiveSources keeps a new query from forgetting that a
// deleted project is gone: every SQL string in the package that reads
// projects, tickets, epics, documents, subtasks or entries must take them from
// live.go's sources, unless its function is in rawReadAllowed.
func TestReadsGoThroughLiveSources(t *testing.T) {
	fset := token.NewFileSet()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") || name == "live.go" {
			continue
		}
		file, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, decl := range file.Decls {
			owner := declName(decl)
			ast.Inspect(decl, func(n ast.Node) bool {
				lit, ok := n.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					return true
				}
				text, err := strconv.Unquote(lit.Value)
				if err != nil {
					t.Fatalf("%s: %v", fset.Position(lit.Pos()), err)
				}
				for _, m := range rawRead.FindAllStringSubmatch(text, -1) {
					if m[1] != "" {
						continue
					}
					checked++
					if _, ok := rawReadAllowed[owner]; !ok {
						t.Errorf("%s: %s reads %q straight from the table; use its live source from live.go, "+
							"or add %s to rawReadAllowed with the reason it must see deleted projects",
							fset.Position(lit.Pos()), owner, m[0], owner)
					}
				}
				return true
			})
		}
	}
	if checked == 0 {
		t.Fatal("found no raw reads at all, not even the allowed ones: is the pattern still right?")
	}
}

// declName is the name of a function, method, or the first name a const or
// var declaration declares.
func declName(decl ast.Decl) string {
	switch d := decl.(type) {
	case *ast.FuncDecl:
		return d.Name.Name
	case *ast.GenDecl:
		for _, spec := range d.Specs {
			if v, ok := spec.(*ast.ValueSpec); ok && len(v.Names) > 0 {
				return v.Names[0].Name
			}
		}
	}
	return ""
}
