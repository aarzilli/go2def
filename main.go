package main

import (
	"bufio"
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"go/types"
	"io"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"

	"golang.org/x/tools/go/packages"
)

const verbose = true
const debugLoadPackages = false

var Goroot string

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
		b, _ := exec.Command("go", "env", "GOROOT").CombinedOutput()
		Goroot = strings.TrimSpace(string(b))
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

		if len(os.Args) >= 3 && os.Args[1] == "describe" && os.Args[2] == "-modified" {
			buf, _ := ioutil.ReadAll(os.Stdin)
			buf = append(buf, 0)
			_, err = client.Write(buf)
			if err != nil {
				fmt.Printf("write: %v\n", err)
				os.Exit(1)
			}
		}

		fmt.Printf("copying client stuff to stdout\n")
		io.Copy(os.Stdout, client)
	}
}

var serveMu sync.Mutex
var requestWorkingDirectory string

func serve(conn io.ReadWriteCloser) (close bool) {
	serveMu.Lock()
	defer serveMu.Unlock()
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

	requestWorkingDirectory = fields[0]

	switch fields[1] {
	case "describe":
		describe(conn, rd, fields[2])
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

func describe(out io.Writer, rd *bufio.Reader, args string) {
	modified, path, pos, ok := parseDescribeArgs(out, args)
	if !ok {
		return
	}

	if verbose {
		log.Printf("describe modified=%v path=%q start=%d end=%d", modified, path, pos[0], pos[1])
	}

	var modfiles map[string][]byte

	if modified {
		modbuf, err := rd.ReadBytes(0)
		if err != nil {
			log.Printf("error reading request: %v", err)
			return
		}
		if len(modbuf) > 0 {
			modbuf = modbuf[:len(modbuf)-1]
			modfiles = parseModified(modbuf)
		}
	}

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

func parseModified(buf []byte) map[string][]byte {
	r := map[string][]byte{}
	for len(buf) > 0 {
		nl := bytes.Index(buf, []byte{'\n'})
		if nl < 0 {
			log.Fatalf("error parsing modified input")
			return nil
		}
		filename := string(buf[:nl])
		buf = buf[nl+1:]

		nl = bytes.Index(buf, []byte{'\n'})
		if nl < 0 {
			log.Fatalf("error parsing modified input")
			return nil
		}
		szstr := string(buf[:nl])
		buf = buf[nl+1:]

		sz, err := strconv.Atoi(szstr)
		if err != nil {
			log.Fatalf("error parsing modified input: %v", err)
			return nil
		}
		r[filename] = buf[:sz]
		buf = buf[sz:]
	}
	return r
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
	return packages.Load(&packages.Config{Mode: packages.LoadSyntax, Dir: requestWorkingDirectory, Fset: currentFileSet, ParseFile: parseFile}, filepath.Dir(path))
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
			pkgs2, err := packages.Load(&packages.Config{Mode: packages.LoadSyntax, Dir: requestWorkingDirectory, Fset: pkg.Fset}, pkg.PkgPath)
			if err != nil {
				return true
			}

			pkg = pkgs2[0]
			p := pkg.Fset.Position(pos)
			filename := p.Filename
			const gorootPrefix = "$GOROOT"
			if strings.HasPrefix(filename, gorootPrefix) {
				filename = Goroot + filename[len(gorootPrefix):]
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
