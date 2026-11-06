package main

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"sort"
	"strings"
)

type request struct {
	Files          []string `json:"files"`
	LifecycleNames []string `json:"lifecycleNames"`
}

type finding struct {
	Path  string   `json:"path"`
	Owner string   `json:"owner"`
	Rule  string   `json:"rule"`
	Flags []string `json:"flags"`
	Line  int      `json:"line"`
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	var input request
	if err := json.NewDecoder(os.Stdin).Decode(&input); err != nil {
		return err
	}
	names := make(map[string]bool)
	for _, name := range input.LifecycleNames {
		names[name] = true
	}
	findings := []finding{}
	for _, path := range input.Files {
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		found, err := scanSource(path, data, names)
		if err != nil {
			return err
		}
		findings = append(findings, found...)
	}
	return json.NewEncoder(os.Stdout).Encode(findings)
}

func scanSource(path string, source []byte, names map[string]bool) ([]finding, error) {
	positions := token.NewFileSet()
	file, err := parser.ParseFile(positions, path, source, parser.ParseComments|parser.AllErrors)
	if err != nil {
		return nil, err
	}
	if ast.IsGenerated(file) {
		return nil, nil
	}
	findings := []finding{}
	owners := []string{"module"}
	ast.Inspect(file, func(node ast.Node) bool {
		if node == nil {
			owners = owners[:len(owners)-1]
			return false
		}
		owner := ownerOf(node, owners[len(owners)-1])
		owners = append(owners, owner)
		flags := lifecycleBindings(node)
		selected := []string{}
		for _, name := range flags {
			if names[strings.ToLower(strings.Trim(name, "_"))] {
				selected = append(selected, name)
			}
		}
		if len(selected) >= 2 {
			sort.Strings(selected)
			findings = append(findings, finding{Path: path, Owner: owner, Rule: "lifecycle-flags", Flags: selected, Line: positions.Position(node.Pos()).Line})
		}
		return true
	})
	return findings, nil
}

func ownerOf(node ast.Node, parent string) string {
	switch node := node.(type) {
	case *ast.TypeSpec:
		return parent + "." + node.Name.Name
	case *ast.ValueSpec:
		return parent + "." + node.Names[0].Name
	case *ast.FuncDecl:
		if node.Recv != nil {
			return receiverName(node.Recv.List[0].Type) + "." + node.Name.Name
		}
		return node.Name.Name
	case *ast.FuncLit:
		return parent + ".callback"
	}
	return parent
}

func receiverName(expr ast.Expr) string {
	switch expr := expr.(type) {
	case *ast.StarExpr:
		return receiverName(expr.X)
	case *ast.IndexExpr:
		return receiverName(expr.X)
	case *ast.IndexListExpr:
		return receiverName(expr.X)
	case *ast.Ident:
		return expr.Name
	}
	return "receiver"
}

func lifecycleBindings(node ast.Node) []string {
	var flags []string
	switch node := node.(type) {
	case *ast.StructType:
		for _, field := range node.Fields.List {
			if booleanType(field.Type) {
				for _, name := range field.Names {
					flags = append(flags, name.Name)
				}
			}
		}
	case *ast.BlockStmt:
		for _, statement := range node.List {
			flags = append(flags, localBooleans(statement)...)
		}
	}
	return flags
}

func booleanType(expr ast.Expr) bool {
	switch expr := expr.(type) {
	case *ast.Ident:
		return expr.Name == "bool"
	case *ast.SelectorExpr:
		pkg, ok := expr.X.(*ast.Ident)
		return ok && pkg.Name == "atomic" && expr.Sel.Name == "Bool"
	}
	return false
}

func localBooleans(statement ast.Stmt) []string {
	switch statement := statement.(type) {
	case *ast.AssignStmt:
		return shortDeclarationBooleans(statement)
	case *ast.DeclStmt:
		return declaredBooleans(statement)
	}
	return nil
}

func shortDeclarationBooleans(statement *ast.AssignStmt) []string {
	if statement.Tok != token.DEFINE || len(statement.Lhs) != len(statement.Rhs) {
		return nil
	}
	var names []string
	for i, expr := range statement.Rhs {
		name, ok := statement.Lhs[i].(*ast.Ident)
		if ok && booleanLiteral(expr) {
			names = append(names, name.Name)
		}
	}
	return names
}

func declaredBooleans(statement *ast.DeclStmt) []string {
	declaration, ok := statement.Decl.(*ast.GenDecl)
	if !ok || declaration.Tok != token.VAR {
		return nil
	}
	var names []string
	for _, spec := range declaration.Specs {
		value := spec.(*ast.ValueSpec)
		for i, name := range value.Names {
			if booleanType(value.Type) || i < len(value.Values) && booleanLiteral(value.Values[i]) {
				names = append(names, name.Name)
			}
		}
	}
	return names
}

func booleanLiteral(expr ast.Expr) bool {
	value, ok := expr.(*ast.Ident)
	return ok && (value.Name == "true" || value.Name == "false")
}
