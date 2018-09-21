package go2def

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"go/types"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"golang.org/x/tools/go/packages"
)

const verbose = false
const debugLoadPackages = false

var goroot *string

func Goroot() string {
	if goroot != nil {
		return *goroot
	}

	b, _ := exec.Command("go", "env", "GOROOT").CombinedOutput()
	s := strings.TrimSpace(string(b))
	goroot = &s
	return *goroot
}

func Describe(out io.Writer, path string, pos [2]int, modfiles map[string][]byte) {
	pkgs, err := loadPackages(out, path, modfiles)
	if err != nil {
		fmt.Fprintf(out, "loading packages: %v", err)
		return
	}

	if verbose && debugLoadPackages {
		packages.Visit(pkgs, func(pkg *packages.Package) bool {
			log.Printf("package %v\n", pkg)
			return true
		}, nil)
	}

	packages.Visit(pkgs, func(pkg *packages.Package) bool {
		for i := range pkg.Syntax {
			//TODO: better way to match file?
			if strings.HasSuffix(pkg.CompiledGoFiles[i], path) {
				node := findNodeInFile(pkg, pkg.Syntax[i], pos, pos[0] == pos[1])
				if node != nil {
					describeNode(out, pkg, pkgs, node)
				}
				break
			}
		}
		return true
	}, nil)
}

var currentFileSet *token.FileSet

func getPosition(pos token.Pos) token.Position {
	return currentFileSet.Position(pos)
}

func getFileSet(pos token.Pos) *token.FileSet {
	return currentFileSet
}

func loadPackages(out io.Writer, path string, modfiles map[string][]byte) ([]*packages.Package, error) {
	var parseFile func(*token.FileSet, string) (*ast.File, error)
	if modfiles != nil {
		parseFile = func(fset *token.FileSet, name string) (*ast.File, error) {
			return parser.ParseFile(fset, name, modfiles[name], parser.ParseComments)
		}
	}
	currentFileSet = token.NewFileSet()
	wd, _ := os.Getwd()
	return packages.Load(&packages.Config{Mode: packages.LoadSyntax, Dir: wd, Fset: currentFileSet, ParseFile: parseFile}, filepath.Dir(path))
}

func findNodeInFile(pkg *packages.Package, root *ast.File, pos [2]int, autoexpand bool) ast.Node {
	v := &exactVisitor{pos, pkg, autoexpand, nil}
	ast.Walk(v, root)
	return v.ret
}

type exactVisitor struct {
	pos        [2]int
	pkg        *packages.Package
	autoexpand bool
	ret        ast.Node
}

func (v *exactVisitor) Visit(node ast.Node) ast.Visitor {
	if node == nil {
		return nil
	}
	if v.pkg.Fset.Position(node.Pos()).Offset == v.pos[0] && v.pkg.Fset.Position(node.End()).Offset == v.pos[1] {
		v.ret = node
	} else if v.autoexpand && v.ret == nil {
		switch node.(type) {
		case *ast.Ident, *ast.SelectorExpr:
			if v.pkg.Fset.Position(node.Pos()).Offset == v.pos[0] || v.pkg.Fset.Position(node.End()).Offset == v.pos[0] {
				v.ret = node
			}
		}
	}
	return v
}

func describeNode(out io.Writer, pkg *packages.Package, pkgs []*packages.Package, node ast.Node) {
	switch node := node.(type) {
	case *ast.Ident:
		obj := pkg.TypesInfo.Uses[node]
		if obj == nil {
			fmt.Fprintf(out, "unknown identifier %v\n", node)
			return
		}

		declnode := findNodeInPackages(pkgs, obj.Pkg().Path(), obj.Pos())
		if verbose {
			log.Printf("declaration node %v\n", declnode)
		}

		if declnode != nil {
			describeDeclaration(out, declnode, obj.Type())

			pos := getPosition(declnode.Pos())
			fmt.Fprintf(out, "\n%s:%d\n", pos.Filename, pos.Line)

		} else {
			fmt.Fprintf(out, "%s\n", obj)
			describeType(out, "type:", obj.Type())

			pos := getPosition(obj.Pos())
			fmt.Fprintf(out, "\n%s:%d\n", pos.Filename, pos.Line)

		}

	case *ast.SelectorExpr:
		sel := pkg.TypesInfo.Selections[node]
		if sel == nil {
			if idobj := pkg.TypesInfo.Uses[node.Sel]; idobj != nil {
				describeNode(out, pkg, pkgs, node.Sel)
				return
			}
			if typeOfExpr := pkg.TypesInfo.Types[node.X]; typeOfExpr.Type != nil {
				typeAndVal := pkg.TypesInfo.Types[node]
				describeType(out, "receiver:", typeAndVal.Type)
				describeTypeContents(out, typeAndVal.Type, node.Sel.String())
				return
			}
			fmt.Fprintf(out, "unknown selector expression ")
			printer.Fprint(out, getFileSet(node.Pos()), node)
			fmt.Fprintf(out, "\n")
			return
		}

		obj := sel.Obj()

		fallbackdescr := true

		declnode := findNodeInPackages(pkgs, obj.Pkg().Path(), obj.Pos())
		if declnode != nil {
			switch declnode := declnode.(type) {
			case *ast.FuncDecl:
				printFunc(out, declnode)
				fallbackdescr = false
			}
		}

		if fallbackdescr {
			switch sel.Kind() {
			case types.FieldVal:
				fmt.Fprintf(out, "struct field ")
			case types.MethodVal:
				fmt.Fprintf(out, "method ")
			case types.MethodExpr:
				fmt.Fprintf(out, "method expression ")
			default:
				fmt.Fprintf(out, "unknown selector ")
			}

			printer.Fprint(out, getFileSet(node.Pos()), node)
			fmt.Fprintf(out, "\n")
			describeType(out, "receiver:", sel.Recv())
			describeType(out, "type:", sel.Type())
		}

		pos := getPosition(obj.Pos())
		fmt.Fprintf(out, "\n%s:%d\n", pos.Filename, pos.Line)

	case ast.Expr:
		typeAndVal := pkg.TypesInfo.Types[node]
		describeType(out, "type:", typeAndVal.Type)
	}
}

func describeDeclaration(out io.Writer, declnode ast.Node, typ types.Type) {
	normaldescr := true

	switch declnode := declnode.(type) {
	case *ast.FuncDecl:
		printFunc(out, declnode)
		normaldescr = false
	default:
	}

	if normaldescr {
		describeType(out, "type:", typ)
	}
}

func printFunc(out io.Writer, declnode *ast.FuncDecl) {
	body := declnode.Body
	declnode.Body = nil
	printer.Fprint(out, getFileSet(declnode.Pos()), declnode)
	fmt.Fprintf(out, "\n")
	declnode.Body = body
}

func describeType(out io.Writer, prefix string, typ types.Type) {
	fmt.Fprintf(out, "%s %v\n", prefix, typ)
	ntyp, isnamed := typ.(*types.Named)
	if !isnamed {
		return
	}
	obj := ntyp.Obj()
	if obj == nil {
		return
	}
	pos := getPosition(obj.Pos())
	fmt.Fprintf(out, "\t%s:%d\n", pos.Filename, pos.Line)
}

func describeTypeContents(out io.Writer, typ types.Type, prefix string) {
	switch styp := typ.(type) {
	case *types.Named:
		if styp.NumMethods() > 0 {
			fmt.Fprintf(out, "\nMethods:\n")
			for i := 0; i < styp.NumMethods(); i++ {
				m := styp.Method(i)
				if strings.HasPrefix(m.Name(), prefix) {
					fmt.Fprintf(out, "\t%s\n", m)
				}
			}
		}
	case *types.Interface:
		if styp.NumMethods() > 0 {
			fmt.Fprintf(out, "\nMethods:\n")
			for i := 0; i < styp.NumMethods(); i++ {
				m := styp.Method(i)
				if strings.HasPrefix(m.Name(), prefix) {
					fmt.Fprintf(out, "\t%s\n", m)
				}
			}
		}
	}

	if typ, isstruct := typ.(*types.Struct); isstruct {
		if typ.NumFields() > 0 {
			fmt.Fprintf(out, "\nFields:\n")
			for i := 0; i < typ.NumFields(); i++ {
				f := typ.Field(i)
				if strings.HasPrefix(f.Name(), prefix) {
					fmt.Fprintf(out, "\t%s\n", f)
				}
			}
		}
	}
}

func findNodeInPackages(pkgs []*packages.Package, pkgpath string, pos token.Pos) ast.Node {
	var r ast.Node
	packages.Visit(pkgs, func(pkg *packages.Package) bool {
		if r != nil {
			return false
		}
		if pkg.PkgPath != pkgpath {
			return true
		}

		if pkg.Syntax == nil {
			if verbose {
				log.Printf("loading syntax for %q", pkg.PkgPath)
			}
			wd, _ := os.Getwd()
			pkgs2, err := packages.Load(&packages.Config{Mode: packages.LoadSyntax, Dir: wd, Fset: pkg.Fset}, pkg.PkgPath)
			if err != nil {
				return true
			}

			pkg = pkgs2[0]
			p := pkg.Fset.Position(pos)
			filename := p.Filename
			const gorootPrefix = "$GOROOT"
			if strings.HasPrefix(filename, gorootPrefix) {
				filename = Goroot() + filename[len(gorootPrefix):]
			}
			//XXX: ideally we would look for pkg.Fset.File(pos).Offset(pos) instead but it seems to be wrong.
			for i := range pkg.Syntax {
				node := findDeclByLine(pkg.Syntax[i], filename, p.Line)
				if node != nil {
					r = node
				}
			}
			return true
		}

		for i := range pkg.Syntax {
			node := findDecl(pkg.Syntax[i], pos)
			if node != nil {
				r = node
			}
		}
		return true
	}, nil)
	return r
}

func findDecl(root *ast.File, pos token.Pos) ast.Node {
	v := &exactVisitorForDecl{pos, nil}
	ast.Walk(v, root)
	return v.ret
}

type exactVisitorForDecl struct {
	pos token.Pos
	ret ast.Node
}

func (v *exactVisitorForDecl) Visit(node ast.Node) ast.Visitor {
	if node == nil {
		return v
	}
	if v.pos >= node.Pos() && v.pos < node.End() {
		switch node := node.(type) {
		case *ast.GenDecl, *ast.AssignStmt, *ast.DeclStmt:
			v.ret = node
		case *ast.FuncDecl:
			if v.pos == node.Name.Pos() {
				v.ret = node
			}
		}
	}
	return v
}

func findDeclByLine(root *ast.File, filename string, line int) ast.Node {
	v := &exactVisitorForFileLine{filename, line, nil}
	ast.Walk(v, root)
	return v.ret
}

type exactVisitorForFileLine struct {
	filename string
	line     int
	ret      ast.Node
}

func (v *exactVisitorForFileLine) Visit(node ast.Node) ast.Visitor {
	if node == nil {
		return v
	}
	p := getPosition(node.Pos())
	if v.filename == p.Filename && v.line == p.Line {
		switch node := node.(type) {
		case *ast.GenDecl, *ast.AssignStmt, *ast.DeclStmt, *ast.FuncDecl:
			v.ret = node
		}
	}
	return v
}
