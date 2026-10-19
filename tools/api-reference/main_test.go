package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"
)

func TestSignatureIgnoresUnrelatedFilePositions(t *testing.T) {
	source := "package external\ntype Record struct {\n Created time.Time\n Name string\n}\n"
	own := token.NewFileSet()
	file, err := parser.ParseFile(own, "external.go", source, 0)
	if err != nil {
		t.Fatal(err)
	}
	declaration := file.Decls[0].(*ast.GenDecl)
	var first string
	for _, dense := range []bool{false, true} {
		unrelated := token.NewFileSet()
		positions := unrelated.AddFile("importer.go", -1, len(source)+100)
		if dense {
			for offset := 1; offset < positions.Size(); offset++ {
				positions.AddLine(offset)
			}
		}
		catalog := &catalog{set: unrelated}
		got := catalog.signature(declaration)
		if catalog.err != nil {
			t.Fatal(catalog.err)
		}
		if !strings.Contains(got, "time.Time") {
			t.Errorf("unrelated positions split a selector: %q", got)
		}
		if _, err := parser.ParseFile(token.NewFileSet(), "signature.go", "package api\n"+got, 0); err != nil {
			t.Errorf("invalid signature: %v", err)
		}
		if first != "" && got != first {
			t.Errorf("signature depends on unrelated positions:\nfirst: %s\nsecond: %s", first, got)
		}
		first = got
	}
}
