package main

import (
	"os"
	"fmt"
	"io"
	"bufio"
	"log"
	"strings"
	"runtime"
	"strconv"
	"path/filepath"
	"go/ast"
	"go/token"
	"go/printer"
	"go/types"
	
	"golang.org/x/tools/go/packages"
)

const verbose = true
const debugLoadPackages = false

func usage() {
	fmt.Printf("usage:\n")
	fmt.Printf("\tgo2def daemon\n")
	fmt.Printf("\t\tstarts go2def daemon\n")
	fmt.Printf("\tgo2def describe [-modified] <filename>:#<startpos>[,#<endpos>]\n")
	fmt.Printf("\t\tdescribes the specified selection, if -modified is specified it reads an archive of modified files from standard input\n")
	fmt.Printf("\tgo2def quit\n")
	fmt.Printf("\t\tstops daemon\n")
	os.Exit(1)
}

func main() {
	if len(os.Args) < 2 {
		fmt.Printf("not enough arguments\n")
		usage()
	}
	
	switch os.Args[1] {
	case "daemon":
		startServer()
	default:
		client := connect()
		
		defer client.Close()
		
		cwd, _ := os.Getwd()
		req := []byte(cwd)
		req = append(req, ' ')
		req = append(req, []byte(strings.Join(os.Args[1:], " "))...)
		req = append(req, 0)
		
		_, err := client.Write(req)
		if err != nil {
			fmt.Printf("write: %v\n", err)
			os.Exit(1)
		}
		
		//TODO: -modified
		
		io.Copy(os.Stdout, client)
	}
}

func serve(conn io.ReadWriteCloser) (close bool) {
	defer conn.Close()
	defer func() {
		if ierr := recover(); ierr != nil {
			log.Printf("panic: %v", ierr)
			skip := 1
			for {
				pc, file, line, ok := runtime.Caller(skip)
				if !ok {
					break
				}
				skip++
				log.Printf("\t%s:%d %#x", file, line, pc)
			}
		}
	}()
	rd := bufio.NewReader(conn)
	req, err := rd.ReadBytes(0)
	if err != nil {
		log.Printf("error reading request: %v", err)
		return false
	}
	
	req = req[:len(req)-1]
	
	fields := strings.SplitN(string(req), " ", 3)
	
	cwd := fields[0]
	
	switch fields[1] {
	case "describe":
		describe(conn, cwd, fields[2])
	case "quit":
		if verbose {
			log.Printf("quitting")
		}
		return true
	default:
		fmt.Fprintf(conn, "unknown command: %q\n", fields[0])
	}
	
	return false
}

func describe(out io.Writer, cwd, args string) {
	modified, path, pos, ok := parseDescribeArgs(out, args)
	if !ok {
		return
	}
	
	if verbose {
		log.Printf("describe modified=%v path=%q start=%d end=%d", modified, path, pos[0], pos[1])
	}
	
	pkgs, err := loadPackages(out, cwd, path)
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

func parseDescribeArgs(out io.Writer, args string) (modified bool, path string, pos [2]int, ok bool) {
	const modifiedFlag = "-modified "
	modified = false
	if strings.HasPrefix(args, modifiedFlag) {
		args = args[len(modifiedFlag):]
		modified = true
	}
	
	
	colon := strings.LastIndex(args, ":")
	if colon < 0 {
		fmt.Fprintf(out, "could not parse describe argument %q", args)
		return
	}
	path = args[:colon]
	v := strings.SplitN(args[colon+1:], ",", 2)
	for i := range v {
		if len(v[i]) < 2 || v[i][0] != '#' {
			fmt.Fprintf(out, "could not parse describe argument %q", args)
			return
		}
		var err error
		pos[i], err = strconv.Atoi(v[i][1:])
		if err != nil {
			fmt.Fprintf(out, "could not parse describe argument %q: %v", args, err)
			return
		}
	}
	if len(v) == 1 {
		pos[1] = pos[0]
	}
	
	ok = true
	return
}

func loadPackages(out io.Writer, cwd, path string) ([]*packages.Package, error) {
	//TODO: 
	// - replace modified files in parse
	// - use package cache
	return packages.Load(&packages.Config{ Mode: packages.LoadAllSyntax, Dir: cwd }, filepath.Dir(path))
}

func findNodeInFile(pkg *packages.Package, root *ast.File, pos [2]int, autoexpand bool) ast.Node {
	//TODO: implement autoexpand
	v := &exactVisitor{ pos, pkg, nil }
	ast.Walk(v, root)
	return v.ret
}

type exactVisitor struct {
	pos [2]int
	pkg *packages.Package
	ret ast.Node
}

func (v *exactVisitor) Visit(node ast.Node) ast.Visitor {
	if node == nil {
		return nil
	}
	if v.pkg.Fset.Position(node.Pos()).Offset != v.pos[0] || v.pkg.Fset.Position(node.End()).Offset != v.pos[1] {
		return v
	}
	v.ret = node
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
		
		pos := pkg.Fset.Position(obj.Pos())
		fmt.Fprintf(out, "%s:%d\n\n", pos.Filename, pos.Line)
		
		declnode := findNodeInPackages(pkgs, obj.Pkg().Path(), obj.Pos())
		if verbose {
			log.Printf("declaration node %v\n", declnode)
		}
		
		if declnode != nil {
			describeDeclaration(out, pkg, declnode, obj.Type())
		} else {
			fmt.Fprintf(out, "%s\n", obj)
		}
		
		
	case *ast.SelectorExpr:
		sel := pkg.TypesInfo.Selections[node]
		if sel == nil {
			if idobj := pkg.TypesInfo.Uses[node.Sel]; idobj != nil {
				describeNode(out, pkg, pkgs, node.Sel)
				return
			}
			fmt.Fprintf(out, "unknown selector expression ")
			printer.Fprint(out, pkg.Fset, node)
			fmt.Fprintf(out, "\n")
			return
		}
		
		obj := sel.Obj()
		pos := pkg.Fset.Position(obj.Pos())
		fmt.Fprintf(out, "%s:%d\n\n", pos.Filename, pos.Line)
		
		
		fallbackdescr := true
		
		declnode := findNodeInPackages(pkgs, obj.Pkg().Path(), obj.Pos())
		if declnode != nil {
			switch declnode := declnode.(type) {
			case *ast.FuncDecl:
				printFunc(out, pkg, declnode)
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
			
			printer.Fprint(out, pkg.Fset, node)
			fmt.Fprintf(out, "\n")
			describeType(out, pkg, "receiver:", sel.Recv())
			describeType(out, pkg, "type:", sel.Type())
		}
		
	case ast.Expr:
		typeAndVal := pkg.TypesInfo.Types[node]
		describeType(out, pkg, "type:", typeAndVal.Type)
	}
	
	
	//TODO: implement
	// - if it's a selector expression and the first element is a package name do the same thing
}

func describeDeclaration(out io.Writer, pkg *packages.Package, declnode ast.Node, typ types.Type) {
	normaldescr := true
	
	switch declnode := declnode.(type) {
	case *ast.FuncDecl:
		printFunc(out, pkg, declnode)
		normaldescr = false
	default:
	}
	
	if normaldescr {
		describeType(out, pkg, "type:", typ)
		fmt.Fprintf(out, "\n")
		fmt.Fprintf(out, "\n")
	}
}

func printFunc(out io.Writer, pkg *packages.Package, declnode *ast.FuncDecl) {
	body := declnode.Body
	declnode.Body = nil
	printer.Fprint(out, pkg.Fset, declnode)
	fmt.Fprintf(out, "\n")
	declnode.Body = body
}

func describeType(out io.Writer, pkg *packages.Package, prefix string, typ types.Type) {
	fmt.Fprintf(out, "%s %v\n", prefix, typ)
	ntyp, isnamed := typ.(*types.Named)
	if !isnamed {
		return
	}
	obj := ntyp.Obj()
	if obj == nil {
		return
	}
	pos := pkg.Fset.Position(obj.Pos())
	fmt.Fprintf(out, "\t%s:%d\n", pos.Filename, pos.Line)
}

func findNodeInPackages(pkgs []*packages.Package, pkgpath string, pos token.Pos) ast.Node {
	var r ast.Node
	packages.Visit(pkgs, func(pkg *packages.Package) bool {
		if pkg.PkgPath != pkgpath {
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
	v := &exactVisitorForDecl{ pos, nil  }
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
		switch node.(type) {
		case *ast.GenDecl, *ast.FuncDecl, *ast.AssignStmt, *ast.DeclStmt:
			v.ret = node
		}
	}
	return v
}