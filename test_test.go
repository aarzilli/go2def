package go2def

import (
	"bytes"
	"io/ioutil"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

const quoted = true

func must(err error) {
	if err != nil {
		panic(err)
	}
}

func findSel(path, start, end string) (pos [2]int) {
	buf, err := ioutil.ReadFile(path)
	must(err)
	s := string(buf)

	pos[0] = findPos(s, start, true)
	if end == "" {
		pos[1] = pos[0]
	} else {
		pos[1] = findPos(s, end, false)
	}
	return
}

func findPos(s, expr string, isstart bool) int {
	needle := expr
	off := 0
	op := strings.Index(expr, "+")
	if op < 0 {
		op = strings.Index(expr, "-")
	}
	var opch byte
	if op > 0 {
		opch = expr[op]
		needle = expr[:op]
		offstr := expr[op:]
		var err error
		off, err = strconv.Atoi(offstr)
		must(err)
	}

	needle = "/*" + needle + "*/"

	needleidx := strings.Index(s, needle)
	if needleidx < 0 {
		panic("not found")
	}

	if isstart && opch != '-' {
		needleidx += len(needle)
	}

	needleidx += off

	return needleidx
}

type modifyfn func(string) string

func testDescribe(path string, start, end string, modify []modifyfn, tgt string) func(t *testing.T) {
	return func(t *testing.T) {
		var out bytes.Buffer
		oldpath := path
		wd, _ := os.Getwd()
		if path[0] != '/' {
			path = filepath.Join(wd, "internal", path)
		}

		pos := findSel(path, start, end)

		t.Logf("describe %s:#%d:#%d", oldpath, pos[0], pos[1])

		cfg := &Config{Out: &out}

		if len(modify) > 0 {
			cfg.Modfiles = make(map[string][]byte)

			b, err := ioutil.ReadFile(path)
			must(err)

			s := string(b)

			for _, m := range modify {
				if m == nil {
					continue
				}
				s = m(s)
			}

			cfg.Modfiles[path] = []byte(s)
		}

		Describe(path, pos, cfg)
		outs := out.String()

		if !wildmatch(tgt, outs) {
			t.Errorf("output mismatch")
		}

		if quoted {
			t.Logf("%q", outs)
		} else {
			if len(outs) > 0 && outs[len(outs)-1] != '\n' {
				outs += "<no newline>"
			}
			t.Logf("\n%s", outs)
		}
	}
}

func wildmatch(pattern, out string) bool {
	i, j := 0, 0
	for {
		if i >= len(pattern) && j >= len(out) {
			return true
		}
		if (i >= len(pattern) && j < len(out)) || (i < len(pattern) && j >= len(out)) {
			return false
		}

		if pattern[i] == out[j] {
			i++
			j++
			continue
		}

		if pattern[i] == '?' {
			for j < len(out) {
				if out[j] < '0' || out[j] > '9' {
					break
				}
				j++
			}
			i++
			continue
		}

		return false
	}
}

func insert(expr string, ins string) func(s string) string {
	return func(s string) string {
		i := findPos(s, expr, true)
		return s[:i] + ins + s[i:]
	}
}

func TestNoexpansionDescribe(t *testing.T) {
	t.Run("call-to-func-in-same-file", testDescribe("testfixture1/f.go", "a", "b", nil, "func callable(x int) int\n\n/home/a/n/go/src/github.com/aarzilli/go2def/internal/testfixture1/f.go:?\n"))
	t.Run("call-to-func-in-different-file", testDescribe("testfixture1/f.go", "c", "d", nil, "// callable2 is a blah blah blah\nfunc callable2(x int) int\n\n/home/a/n/go/src/github.com/aarzilli/go2def/internal/testfixture1/callable2.go:?\n"))
	t.Run("call-to-func-in-different-package", testDescribe("testfixture1/f.go", "e", "f", nil, "func Callable3(x int) int\n\n/home/a/n/go/src/github.com/aarzilli/go2def/internal/testfixture2/f2.go:?\n"))
	t.Run("call-to-method", testDescribe("testfixture1/s.go", "a", "b", nil, "func (a *Astruct) Method2(b *Astruct) int\n\n/home/a/n/go/src/github.com/aarzilli/go2def/internal/testfixture1/s.go:?\n"))
	t.Run("call-with-package-selector", testDescribe("testfixture1/s.go", "c", "d", nil, "// Atoi returns the result of ParseInt(s, 10, 0) converted to type int.\nfunc Atoi(s string) (int, error)\n\n/usr/local/go-tip/src/strconv/atoi.go:?\n"))
	t.Run("use-of-local-var", testDescribe("testfixture1/s.go", "e", "f", nil, "type: *testfixture1.Astruct\n\t/home/a/n/go/src/github.com/aarzilli/go2def/internal/testfixture1/s.go:?\n\n/home/a/n/go/src/github.com/aarzilli/go2def/internal/testfixture1/s.go:?\n"))
	t.Run("use-of-member-field", testDescribe("testfixture1/s.go", "g", "h", nil, "struct field a.Xmember\nreceiver: *testfixture1.Astruct\n\t/home/a/n/go/src/github.com/aarzilli/go2def/internal/testfixture1/s.go:?\ntype: int\n\n/home/a/n/go/src/github.com/aarzilli/go2def/internal/testfixture1/s.go:?\n"))
	t.Run("use-of-blank-member-field", testDescribe("testfixture1/s.go", "i", "j", nil, "receiver: *testfixture1.Astruct\n\t/home/a/n/go/src/github.com/aarzilli/go2def/internal/testfixture1/s.go:?\n\nMethods:\n\tfunc (*testfixture1.Astruct).Method1(x int) int\n\tfunc (*testfixture1.Astruct).Method2(b *testfixture1.Astruct) int\n\nFields:\n\tfield Xmember int\n\tfield Ymember int\n"))
	t.Run("use-of-variable-with-type-in-other-package", testDescribe("testfixture1/f.go", "g", "h", nil, "type: testfixture2.Bstruct\n\t/home/a/n/go/src/github.com/aarzilli/go2def/internal/testfixture2/f2.go:?\n\n/home/a/n/go/src/github.com/aarzilli/go2def/internal/testfixture1/f.go:?\n"))
}

func TestExpansionDescribe(t *testing.T) {
	// some tests are disabled because we only check if the cursor is at the start or end of the token, not in the middle.

	t.Run("call-to-func-in-same-file-exp-1", testDescribe("testfixture1/f.go", "a", "", nil, "func callable(x int) int\n\n/home/a/n/go/src/github.com/aarzilli/go2def/internal/testfixture1/f.go:?\n"))
	//t.Run("call-to-func-in-same-file-exp-2", testDescribe("testfixture1/f.go", "a+1", "", nil, "func callable(x int) int\n\n/home/a/n/go/src/github.com/aarzilli/go2def/internal/testfixture1/f.go:?\n"))
	t.Run("call-to-func-in-same-file-exp-3", testDescribe("testfixture1/f.go", "b-0", "", nil, "func callable(x int) int\n\n/home/a/n/go/src/github.com/aarzilli/go2def/internal/testfixture1/f.go:?\n"))
	//t.Run("call-to-func-in-same-file-exp-4", testDescribe("testfixture1/f.go", "b-1", "", nil, "func callable(x int) int\n\n/home/a/n/go/src/github.com/aarzilli/go2def/internal/testfixture1/f.go:?\n"))

	t.Run("call-to-func-in-different-file-1", testDescribe("testfixture1/f.go", "c", "", nil, "// callable2 is a blah blah blah\nfunc callable2(x int) int\n\n/home/a/n/go/src/github.com/aarzilli/go2def/internal/testfixture1/callable2.go:?\n"))
	//t.Run("call-to-func-in-different-file-2", testDescribe("testfixture1/f.go", "c+1", "", nil, "// callable2 is a blah blah blah\nfunc callable2(x int) int\n\n/home/a/n/go/src/github.com/aarzilli/go2def/internal/testfixture1/callable2.go:?\n"))
	t.Run("call-to-func-in-different-file-3", testDescribe("testfixture1/f.go", "d-0", "", nil, "// callable2 is a blah blah blah\nfunc callable2(x int) int\n\n/home/a/n/go/src/github.com/aarzilli/go2def/internal/testfixture1/callable2.go:?\n"))
	//t.Run("call-to-func-in-different-file-4", testDescribe("testfixture1/f.go", "d-1", "", nil, "// callable2 is a blah blah blah\nfunc callable2(x int) int\n\n/home/a/n/go/src/github.com/aarzilli/go2def/internal/testfixture1/callable2.go:?\n"))

	t.Run("call-to-func-in-different-package-1", testDescribe("testfixture1/f.go", "e", "", nil, "func Callable3(x int) int\n\n/home/a/n/go/src/github.com/aarzilli/go2def/internal/testfixture2/f2.go:?\n"))
	//t.Run("call-to-func-in-different-package-2", testDescribe("testfixture1/f.go", "e+1", "", nil, "func Callable3(x int) int\n\n/home/a/n/go/src/github.com/aarzilli/go2def/internal/testfixture2/f2.go:?\n"))
	t.Run("call-to-func-in-different-package-3", testDescribe("testfixture1/f.go", "f-0", "", nil, "func Callable3(x int) int\n\n/home/a/n/go/src/github.com/aarzilli/go2def/internal/testfixture2/f2.go:?\n"))
	//t.Run("call-to-func-in-different-package-4", testDescribe("testfixture1/f.go", "f-1", "", nil, "func Callable3(x int) int\n\n/home/a/n/go/src/github.com/aarzilli/go2def/internal/testfixture2/f2.go:?\n"))

	t.Run("call-to-method-1", testDescribe("testfixture1/s.go", "a", "", nil, "func (a *Astruct) Method2(b *Astruct) int\n\n/home/a/n/go/src/github.com/aarzilli/go2def/internal/testfixture1/s.go:?\n"))
	//t.Run("call-to-method-2", testDescribe("testfixture1/s.go", "a+1", "", nil, "func (a *Astruct) Method2(b *Astruct) int\n\n/home/a/n/go/src/github.com/aarzilli/go2def/internal/testfixture1/s.go:?\n"))
	t.Run("call-to-method-3", testDescribe("testfixture1/s.go", "b-0", "", nil, "func (a *Astruct) Method2(b *Astruct) int\n\n/home/a/n/go/src/github.com/aarzilli/go2def/internal/testfixture1/s.go:?\n"))
	//t.Run("call-to-method-4", testDescribe("testfixture1/s.go", "b-1", "", nil, "func (a *Astruct) Method2(b *Astruct) int\n\n/home/a/n/go/src/github.com/aarzilli/go2def/internal/testfixture1/s.go:?\n"))

	t.Run("call-with-package-selector-1", testDescribe("testfixture1/s.go", "c", "", nil, "// Atoi returns the result of ParseInt(s, 10, 0) converted to type int.\nfunc Atoi(s string) (int, error)\n\n/usr/local/go-tip/src/strconv/atoi.go:?\n"))
	//t.Run("call-with-package-selector-2", testDescribe("testfixture1/s.go", "c+1", "", nil, "// Atoi returns the result of ParseInt(s, 10, 0) converted to type int.\nfunc Atoi(s string) (int, error)\n\n/usr/local/go-tip/src/strconv/atoi.go:?\n"))
	t.Run("call-with-package-selector-3", testDescribe("testfixture1/s.go", "d-0", "", nil, "// Atoi returns the result of ParseInt(s, 10, 0) converted to type int.\nfunc Atoi(s string) (int, error)\n\n/usr/local/go-tip/src/strconv/atoi.go:?\n"))
	//t.Run("call-with-package-selector-4", testDescribe("testfixture1/s.go", "d-1", "", nil, "// Atoi returns the result of ParseInt(s, 10, 0) converted to type int.\nfunc Atoi(s string) (int, error)\n\n/usr/local/go-tip/src/strconv/atoi.go:?\n"))

	t.Run("use-of-local-var-1", testDescribe("testfixture1/s.go", "e", "", nil, "type: *testfixture1.Astruct\n\t/home/a/n/go/src/github.com/aarzilli/go2def/internal/testfixture1/s.go:?\n\n/home/a/n/go/src/github.com/aarzilli/go2def/internal/testfixture1/s.go:?\n"))
	//t.Run("use-of-local-var-2", testDescribe("testfixture1/s.go", "e+1", "", nil, "type: *testfixture1.Astruct\n\t/home/a/n/go/src/github.com/aarzilli/go2def/internal/testfixture1/s.go:?\n\n/home/a/n/go/src/github.com/aarzilli/go2def/internal/testfixture1/s.go:?\n"))
	t.Run("use-of-local-var-3", testDescribe("testfixture1/s.go", "f-0", "", nil, "type: *testfixture1.Astruct\n\t/home/a/n/go/src/github.com/aarzilli/go2def/internal/testfixture1/s.go:?\n\n/home/a/n/go/src/github.com/aarzilli/go2def/internal/testfixture1/s.go:?\n"))
	//t.Run("use-of-local-var-4", testDescribe("testfixture1/s.go", "f-1", "", nil, "type: *testfixture1.Astruct\n\t/home/a/n/go/src/github.com/aarzilli/go2def/internal/testfixture1/s.go:?\n\n/home/a/n/go/src/github.com/aarzilli/go2def/internal/testfixture1/s.go:?\n"))

	t.Run("use-of-member-field-1", testDescribe("testfixture1/s.go", "g", "", nil, "struct field a.Xmember\nreceiver: *testfixture1.Astruct\n\t/home/a/n/go/src/github.com/aarzilli/go2def/internal/testfixture1/s.go:?\ntype: int\n\n/home/a/n/go/src/github.com/aarzilli/go2def/internal/testfixture1/s.go:?\n"))
	//t.Run("use-of-member-field-2", testDescribe("testfixture1/s.go", "g+1", "", nil, "struct field a.Xmember\nreceiver: *testfixture1.Astruct\n\t/home/a/n/go/src/github.com/aarzilli/go2def/internal/testfixture1/s.go:?\ntype: int\n\n/home/a/n/go/src/github.com/aarzilli/go2def/internal/testfixture1/s.go:?\n"))
	t.Run("use-of-member-field-3", testDescribe("testfixture1/s.go", "h-0", "", nil, "struct field a.Xmember\nreceiver: *testfixture1.Astruct\n\t/home/a/n/go/src/github.com/aarzilli/go2def/internal/testfixture1/s.go:?\ntype: int\n\n/home/a/n/go/src/github.com/aarzilli/go2def/internal/testfixture1/s.go:?\n"))
	//t.Run("use-of-member-field-4", testDescribe("testfixture1/s.go", "h-1", "", nil, "struct field a.Xmember\nreceiver: *testfixture1.Astruct\n\t/home/a/n/go/src/github.com/aarzilli/go2def/internal/testfixture1/s.go:?\ntype: int\n\n/home/a/n/go/src/github.com/aarzilli/go2def/internal/testfixture1/s.go:?\n"))

	t.Run("use-of-blank-member-field-1", testDescribe("testfixture1/s.go", "i", "", nil, "receiver: *testfixture1.Astruct\n\t/home/a/n/go/src/github.com/aarzilli/go2def/internal/testfixture1/s.go:?\n\nMethods:\n\tfunc (*testfixture1.Astruct).Method1(x int) int\n\tfunc (*testfixture1.Astruct).Method2(b *testfixture1.Astruct) int\n\nFields:\n\tfield Xmember int\n\tfield Ymember int\n"))
	//t.Run("use-of-blank-member-field-2", testDescribe("testfixture1/s.go", "i+1", "", nil, "receiver: *testfixture1.Astruct\n\t/home/a/n/go/src/github.com/aarzilli/go2def/internal/testfixture1/s.go:?\n\nMethods:\n\tfunc (*testfixture1.Astruct).Method1(x int) int\n\tfunc (*testfixture1.Astruct).Method2(b *testfixture1.Astruct) int\n\nFields:\n\tfield Xmember int\n\tfield Ymember int\n"))
	t.Run("use-of-blank-member-field-3", testDescribe("testfixture1/s.go", "j-0", "", nil, "receiver: *testfixture1.Astruct\n\t/home/a/n/go/src/github.com/aarzilli/go2def/internal/testfixture1/s.go:?\n\nMethods:\n\tfunc (*testfixture1.Astruct).Method1(x int) int\n\tfunc (*testfixture1.Astruct).Method2(b *testfixture1.Astruct) int\n\nFields:\n\tfield Xmember int\n\tfield Ymember int\n"))
	//t.Run("use-of-blank-member-field-4", testDescribe("testfixture1/s.go", "j-1", "", nil, "receiver: *testfixture1.Astruct\n\t/home/a/n/go/src/github.com/aarzilli/go2def/internal/testfixture1/s.go:?\n\nMethods:\n\tfunc (*testfixture1.Astruct).Method1(x int) int\n\tfunc (*testfixture1.Astruct).Method2(b *testfixture1.Astruct) int\n\nFields:\n\tfield Xmember int\n\tfield Ymember int\n"))

	t.Run("use-of-variable-with-type-in-other-package-3", testDescribe("testfixture1/f.go", "h-0", "", nil, "type: testfixture2.Bstruct\n\t/home/a/n/go/src/github.com/aarzilli/go2def/internal/testfixture2/f2.go:?\n\n/home/a/n/go/src/github.com/aarzilli/go2def/internal/testfixture1/f.go:?\n"))
}

func TestModified(t *testing.T) {
	t.Run("call-to-func-in-same-file", testDescribe("testfixture1/f.go", "i", "", nil, "nothing found\n"))
	t.Run("call-to-func-in-same-file", testDescribe("testfixture1/f.go", "i", "", []modifyfn{insert("i", "callable2(b.Xmember)")}, "// callable2 is a blah blah blah\nfunc callable2(x int) int\n\n/home/a/n/go/src/github.com/aarzilli/go2def/internal/testfixture1/callable2.go:?\n"))
}

func TestGoMod(t *testing.T) {
	if os.TempDir() == "" {
		panic("notmpdir")
	}
	tmpdir, err := ioutil.TempDir(os.TempDir(), "go2def-test-")
	must(err)
	defer safeRemoveAll(tmpdir)

	ioutil.WriteFile(filepath.Join(tmpdir, "go.mod"), []byte("module testmodule\nrequire (\ngithub.com/aarzilli/go2def v0.0.0-20180921140844-57b3d798eea0\ngolang.org/x/tools v0.0.0-20180917221912-90fa682c2a6e // indirect\n)\n\n"), 0666)

	ioutil.WriteFile(filepath.Join(tmpdir, "t.go"), []byte(`package main

import (
	"github.com/aarzilli/go2def"
)

func main() {
	/*c*/callable2/*d*/(2)
	/*a*/go2def.Describe/*b*/("nonexistent.go:", [2]int{ 1, 1 }, nil)
}`), 0666)

	ioutil.WriteFile(filepath.Join(tmpdir, "f.go"), []byte(`package main

func callable2(x int) {
	println(x+2)
}
`), 0666)

	t.Run("call-imported-function", testDescribe(filepath.Join(tmpdir, "t.go"), "a", "b", nil, "func Describe(path string, pos [2]int, cfg *Config)\n\n/home/a/n/go/pkg/mod/github.com/aarzilli/go2def@v0.0.0-20180921140844-57b3d798eea0/main.go:?\n"))
	t.Run("call-function-in-other-file", testDescribe(filepath.Join(tmpdir, "t.go"), "c", "d", nil, "func callable2(x int)\n\n/tmp/go2def-test-?/f.go:?\n"))
}

func safeRemoveAll(dir string) {
	dh, err := os.Open(dir)
	if err != nil {
		return
	}
	defer dh.Close()
	fis, err := dh.Readdir(-1)
	if err != nil {
		return
	}
	for _, fi := range fis {
		if fi.IsDir() {
			return
		}
	}
	for _, fi := range fis {
		if err := os.Remove(filepath.Join(dir, fi.Name())); err != nil {
			return
		}
	}
	os.Remove(dir)
}
