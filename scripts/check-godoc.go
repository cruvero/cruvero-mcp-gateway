package main

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

type goListPackage struct {
	Name       string
	ImportPath string
	Dir        string
	GoFiles    []string
}

type missingDoc struct {
	File   string
	Line   int
	Symbol string
}

func main() {
	pkgs, err := listPackages()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to list packages: %v\n", err)
		os.Exit(1)
	}

	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to resolve working directory: %v\n", err)
		os.Exit(1)
	}

	missing := make([]missingDoc, 0)
	for _, pkg := range pkgs {
		if pkg.Name == "main" || len(pkg.GoFiles) == 0 {
			continue
		}
		pkgMissing, checkErr := checkPackageDocs(cwd, pkg)
		if checkErr != nil {
			fmt.Fprintf(os.Stderr, "failed to check docs in %s: %v\n", pkg.ImportPath, checkErr)
			os.Exit(1)
		}
		missing = append(missing, pkgMissing...)
	}

	if len(missing) == 0 {
		fmt.Println("All exported declarations have doc comments.")
		return
	}

	sort.Slice(missing, func(i, j int) bool {
		if missing[i].File == missing[j].File {
			if missing[i].Line == missing[j].Line {
				return missing[i].Symbol < missing[j].Symbol
			}
			return missing[i].Line < missing[j].Line
		}
		return missing[i].File < missing[j].File
	})

	fmt.Println("Missing doc comments for exported declarations:")
	for _, item := range missing {
		fmt.Printf("- %s:%d %s\n", item.File, item.Line, item.Symbol)
	}
	os.Exit(1)
}

func listPackages() ([]goListPackage, error) {
	cmd := exec.Command("go", "list", "-json", "./...")
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	dec := json.NewDecoder(strings.NewReader(string(out)))
	pkgs := make([]goListPackage, 0)
	for dec.More() {
		var pkg goListPackage
		if err := dec.Decode(&pkg); err != nil {
			return nil, err
		}
		pkgs = append(pkgs, pkg)
	}
	return pkgs, nil
}

func checkPackageDocs(cwd string, pkg goListPackage) ([]missingDoc, error) {
	fset := token.NewFileSet()
	missing := make([]missingDoc, 0)
	for _, file := range pkg.GoFiles {
		if strings.HasSuffix(file, "_generated.go") {
			continue
		}
		fullPath := filepath.Join(pkg.Dir, file)
		parsed, err := parser.ParseFile(fset, fullPath, nil, parser.ParseComments)
		if err != nil {
			return nil, err
		}
		for _, decl := range parsed.Decls {
			switch typed := decl.(type) {
			case *ast.FuncDecl:
				if typed.Name == nil || !typed.Name.IsExported() {
					continue
				}
				if typed.Recv != nil && !isExportedReceiver(typed.Recv) {
					continue
				}
				if hasDocComment(typed.Doc) {
					continue
				}
				pos := fset.Position(typed.Pos())
				missing = append(missing, missingDoc{
					File:   toRelative(cwd, pos.Filename),
					Line:   pos.Line,
					Symbol: typed.Name.Name,
				})
			case *ast.GenDecl:
				if typed.Tok != token.CONST && typed.Tok != token.VAR && typed.Tok != token.TYPE {
					continue
				}
				for _, spec := range typed.Specs {
					for _, ident := range exportedNames(spec) {
						if hasDocComment(specDoc(spec)) || hasDocComment(typed.Doc) {
							continue
						}
						pos := fset.Position(ident.Pos())
						missing = append(missing, missingDoc{
							File:   toRelative(cwd, pos.Filename),
							Line:   pos.Line,
							Symbol: ident.Name,
						})
					}
				}
			}
		}
	}
	return missing, nil
}

func hasDocComment(group *ast.CommentGroup) bool {
	if group == nil {
		return false
	}
	return strings.TrimSpace(group.Text()) != ""
}

func specDoc(spec ast.Spec) *ast.CommentGroup {
	switch typed := spec.(type) {
	case *ast.TypeSpec:
		return typed.Doc
	case *ast.ValueSpec:
		return typed.Doc
	default:
		return nil
	}
}

func exportedNames(spec ast.Spec) []*ast.Ident {
	names := make([]*ast.Ident, 0)
	switch typed := spec.(type) {
	case *ast.TypeSpec:
		if typed.Name != nil && typed.Name.IsExported() {
			names = append(names, typed.Name)
		}
	case *ast.ValueSpec:
		for _, name := range typed.Names {
			if name != nil && name.IsExported() {
				names = append(names, name)
			}
		}
	}
	return names
}

func isExportedReceiver(recv *ast.FieldList) bool {
	if recv == nil || len(recv.List) == 0 {
		return false
	}
	recvType := receiverTypeName(recv.List[0].Type)
	return recvType != "" && ast.IsExported(recvType)
}

func receiverTypeName(expr ast.Expr) string {
	switch typed := expr.(type) {
	case *ast.Ident:
		return typed.Name
	case *ast.StarExpr:
		return receiverTypeName(typed.X)
	case *ast.IndexExpr:
		return receiverTypeName(typed.X)
	case *ast.IndexListExpr:
		return receiverTypeName(typed.X)
	default:
		return ""
	}
}

func toRelative(cwd, filename string) string {
	rel, err := filepath.Rel(cwd, filename)
	if err != nil {
		return filename
	}
	return rel
}
