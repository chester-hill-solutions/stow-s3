package main

import (
	"go/ast"
	"go/parser"
	"go/token"
)

func generatedGo(path string) bool {
	file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.PackageClauseOnly|parser.ParseComments)
	return err == nil && ast.IsGenerated(file)
}
