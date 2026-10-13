package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/doc"
	"go/parser"
	"go/printer"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type entry struct {
	Name        string     `json:"name"`
	Kind        string     `json:"kind"`
	Signature   string     `json:"signature"`
	Description string     `json:"description"`
	Methods     []entry    `json:"methods,omitempty"`
	Parameters  []argument `json:"parameters,omitempty"`
	Returns     string     `json:"returns,omitempty"`
}

type argument struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Optional bool   `json:"optional"`
}

type catalog struct {
	set   *token.FileSet
	files map[string]*ast.File
	pkg   *doc.Package
	err   error
}

func readCatalog(dir string) (*catalog, error) {
	set := token.NewFileSet()
	packages, err := parser.ParseDir(set, dir, func(info os.FileInfo) bool { return !strings.HasSuffix(info.Name(), "_test.go") }, parser.ParseComments)
	if err != nil {
		return nil, err
	}
	var parsed *ast.Package
	for _, candidate := range packages {
		parsed = candidate
		break
	}
	return &catalog{set: set, files: parsed.Files, pkg: doc.New(parsed, dir, doc.PreserveAST)}, nil
}

func short(text string) string {
	text = strings.TrimSpace(text)
	if at := strings.Index(text, "\n\n"); at >= 0 {
		text = text[:at]
	}
	return strings.ReplaceAll(text, "\n", " ")
}

func (c *catalog) signature(node ast.Node) string {
	ast.Inspect(node, func(n ast.Node) bool {
		switch value := n.(type) {
		case *ast.FuncDecl:
			value.Body = nil
			value.Doc = nil
		case *ast.GenDecl:
			value.Doc = nil
		case *ast.TypeSpec:
			value.Doc = nil
			value.Comment = nil
		case *ast.ValueSpec:
			value.Doc = nil
			value.Comment = nil
		case *ast.Field:
			value.Doc = nil
			value.Comment = nil
		}
		return true
	})
	var result bytes.Buffer
	if err := printer.Fprint(&result, c.set, node); err != nil {
		c.err = err
	}
	return result.String()
}

func (c *catalog) function(fn *doc.Func) entry {
	description := short(fn.Doc)
	item := entry{Name: fn.Name, Kind: "function", Signature: c.signature(fn.Decl), Description: description}
	for _, field := range fn.Decl.Type.Params.List {
		for _, name := range field.Names {
			item.Parameters = append(item.Parameters, argument{Name: name.Name, Type: c.signature(field.Type)})
		}
	}
	if fn.Decl.Type.Results != nil {
		results := []string{}
		for _, field := range fn.Decl.Type.Results.List {
			results = append(results, c.signature(field.Type))
		}
		item.Returns = strings.Join(results, ", ")
	}
	return item
}

func (c *catalog) alias(spec *ast.TypeSpec) (*catalog, *doc.Type) {
	selector, ok := spec.Type.(*ast.SelectorExpr)
	if !ok || !spec.Assign.IsValid() {
		return nil, nil
	}
	qualifier, ok := selector.X.(*ast.Ident)
	if !ok {
		return nil, nil
	}
	for _, file := range c.files {
		if spec.Pos() < file.Pos() || spec.Pos() > file.End() {
			continue
		}
		for _, imported := range file.Imports {
			path, err := strconv.Unquote(imported.Path.Value)
			if err != nil {
				continue
			}
			name := filepath.Base(path)
			if imported.Name != nil {
				name = imported.Name.Name
			}
			if name != qualifier.Name || !strings.HasPrefix(path, "github.com/chester-hill-solutions/stow-s3/") {
				continue
			}
			other, err := readCatalog(strings.TrimPrefix(path, "github.com/chester-hill-solutions/stow-s3/"))
			if err != nil {
				c.err = err
				return nil, nil
			}
			for _, typ := range other.pkg.Types {
				if typ.Name == selector.Sel.Name {
					return other, typ
				}
			}
		}
	}
	return nil, nil
}

func (c *catalog) typeEntry(typ *doc.Type) entry {
	item := entry{Name: typ.Name, Kind: "type", Description: short(typ.Doc)}
	for _, method := range typ.Methods {
		item.Methods = append(item.Methods, c.function(method))
	}
	for _, raw := range typ.Decl.Specs {
		spec, ok := raw.(*ast.TypeSpec)
		if !ok || spec.Name.Name != typ.Name {
			continue
		}
		if other, aliased := c.alias(spec); aliased != nil {
			for _, method := range aliased.Methods {
				item.Methods = append(item.Methods, other.function(method))
			}
			if other.err != nil {
				c.err = other.err
			}
			for _, original := range aliased.Decl.Specs {
				target := original.(*ast.TypeSpec)
				if target.Name.Name == aliased.Name {
					spec.Type = target.Type
					spec.Assign = token.NoPos
				}
			}
		}
		item.Signature = c.signature(&ast.GenDecl{Tok: token.TYPE, Specs: []ast.Spec{spec}})
	}
	return item
}

func (c *catalog) valueEntries(kind string, values []*doc.Value) []entry {
	result := []entry{}
	for _, value := range values {
		description := short(value.Doc)
		for _, raw := range value.Decl.Specs {
			spec := raw.(*ast.ValueSpec)
			for i, name := range spec.Names {
				if !ast.IsExported(name.Name) {
					continue
				}
				single := *spec
				single.Names = []*ast.Ident{name}
				if len(spec.Values) == len(spec.Names) {
					single.Values = []ast.Expr{spec.Values[i]}
				}
				decl := &ast.GenDecl{Tok: value.Decl.Tok, Specs: []ast.Spec{&single}}
				result = append(result, entry{Name: name.Name, Kind: kind, Signature: c.signature(decl), Description: description})
			}
		}
	}
	return result
}

func (c *catalog) entries() []entry {
	result := []entry{}
	for _, fn := range c.pkg.Funcs {
		result = append(result, c.function(fn))
	}
	constants, variables := c.pkg.Consts, c.pkg.Vars
	for _, typ := range c.pkg.Types {
		for _, fn := range typ.Funcs {
			result = append(result, c.function(fn))
		}
		result = append(result, c.typeEntry(typ))
		constants = append(constants, typ.Consts...)
		variables = append(variables, typ.Vars...)
	}
	result = append(result, c.valueEntries("constant", constants)...)
	return append(result, c.valueEntries("variable", variables)...)
}

func run() error {
	catalog, err := readCatalog("pkg/stow")
	if err != nil {
		return err
	}
	entries := catalog.entries()
	if catalog.err != nil {
		return catalog.err
	}
	return json.NewEncoder(os.Stdout).Encode(entries)
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
