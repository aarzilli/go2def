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

func testDescribe(path string, start, end string, modify []modifyfn, tgt Description) func(t *testing.T) {
	return func(t *testing.T) {
		oldpath := path
		wd, _ := os.Getwd()
		if path[0] != '/' {
			path = filepath.Join(wd, "internal", path)
		}

		pos := findSel(path, start, end)

		t.Logf("describe %s:#%d:#%d", oldpath, pos[0], pos[1])

		cfg := &Config{Out: ioutil.Discard}

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

		out := Describe(path, pos, cfg)

		if len(out) != len(tgt) {
			t.Errorf("length mismatch out:%d tgt:%d", len(out), len(tgt))
		} else {
			for i := range out {
				if out[i].Kind != tgt[i].Kind {
					t.Errorf("kind mismatch at %d:\n\texp\t%s\n\tgot\t%s", i, tgt[i].Kind, out[i].Kind)
					continue
				}
				switch out[i].Kind {
				case InfoFunction:
					// comment can change from version to version...
					if tgt[i].Text != "" {
						if tgt[i].Text[0] == '@' {
							if !strings.Contains(out[i].Text, tgt[i].Text[1:]) {
								t.Errorf("text mismatch at %d:\n\texp\t%q\n\tgot\t%q", i, tgt[i].Text, out[i].Text)
							}
						} else {
							if out[i].Text != tgt[i].Text {
								t.Errorf("text mismatch at %d:\n\texp\t%q\n\tgot\t%q", i, tgt[i].Text, out[i].Text)
							}
						}
					}
				case InfoErr, InfoObject, InfoSelection, InfoTypeContents:
					if out[i].Text != tgt[i].Text {
						t.Errorf("text mismatch at %d\n\texp\t%q\n\tgot\t%q", i, tgt[i].Text, out[i].Text)
					}
				case InfoType:
					if out[i].Text != tgt[i].Text {
						t.Errorf("text mismatch at %d\n\texp\t%q\n\tgot\t%q", i, tgt[i].Text, out[i].Text)
					}
					fallthrough
				case InfoPos:
					if tgt[i].Pos != "" {
						tgtpos := strings.Replace(tgt[i].Pos, "$INTERNAL", filepath.Join(wd, "internal"), -1)
						if tgtpos[0] == '/' {
							if !strings.HasPrefix(out[i].Pos, tgtpos) {
								t.Errorf("pos mismatch at %d:\n\texp\t%q\n\tgot\t%q", i, tgtpos, out[i].Pos)
							}
						} else {
							if !strings.Contains(out[i].Pos, tgtpos) {
								t.Errorf("pos mismatch at %d:\n\texp\t%q\n\tgot\t%q", i, tgtpos, out[i].Pos)
							}
						}
					}
				default:
					t.Errorf("unknown kind %s", out[i].Kind)
				}
			}
		}

		var outbuf bytes.Buffer
		out.writeTo(&outbuf)
		outs := outbuf.String()

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

func TestMain(m *testing.M) {
	os.Setenv("GOFLAGS", "") // -mod=vendor can break all tests
	os.Exit(m.Run())
}

func TestNoexpansionDescribe(t *testing.T) {
	t.Run("call-to-func-in-same-file", testDescribe("testfixture1/f.go", "a", "b", nil, Description{
		Info{Kind: InfoFunction, Text: "func callable(x int) int"},
		Info{Kind: InfoPos, Pos: "$INTERNAL/testfixture1/f.go:"},
	}))
	t.Run("call-to-func-in-different-file", testDescribe("testfixture1/f.go", "c", "d", nil, Description{
		Info{Kind: InfoFunction, Text: "// callable2 is a blah blah blah\nfunc callable2(x int) int"},
		Info{Kind: InfoPos, Pos: "$INTERNAL/testfixture1/callable2.go:"},
	}))
	t.Run("call-to-func-in-different-package", testDescribe("testfixture1/f.go", "e", "f", nil, Description{
		Info{Kind: InfoFunction, Text: "func Callable3(x int) int"},
		Info{Kind: InfoPos, Pos: "$INTERNAL/testfixture2/f2.go:"},
	}))
	t.Run("call-to-method", testDescribe("testfixture1/s.go", "a", "b", nil, Description{
		Info{Kind: InfoFunction, Text: "func (a *Astruct) Method2(b *Astruct) int"},
		Info{Kind: InfoPos, Pos: "$INTERNAL/testfixture1/s.go:"},
	}))
	t.Run("call-with-package-selector", testDescribe("testfixture1/s.go", "c", "d", nil, Description{
		Info{Kind: InfoFunction, Text: "@\nfunc Atoi(s string) (int, error)"},
		Info{Kind: InfoPos, Pos: "src/strconv/atoi.go:"},
	}))
	t.Run("use-of-local-var", testDescribe("testfixture1/s.go", "e", "f", nil, Description{
		Info{Kind: InfoType, Text: "type: *testfixture1.Astruct", Pos: "$INTERNAL/testfixture1/s.go:"},
		Info{Kind: InfoPos, Pos: "$INTERNAL/testfixture1/s.go:"},
	}))
	t.Run("use-of-member-field", testDescribe("testfixture1/s.go", "g", "h", nil, Description{
		Info{Kind: InfoSelection, Text: "struct field a.Xmember"},
		Info{Kind: InfoType, Text: "receiver: *testfixture1.Astruct", Pos: "$INTERNAL/testfixture1/s.go:"},
		Info{Kind: InfoType, Text: "type: int"},
		Info{Kind: InfoPos, Pos: "$INTERNAL/testfixture1/s.go:"},
	}))
	t.Run("use-of-blank-member-field", testDescribe("testfixture1/s.go", "i", "j", nil, Description{
		Info{Kind: InfoType, Text: "receiver: *testfixture1.Astruct", Pos: "$INTERNAL/testfixture1/s.go:"},
		Info{Kind: InfoTypeContents, Text: "\nMethods:\n\tfunc (*testfixture1.Astruct).Method1(x int) int\n\tfunc (*testfixture1.Astruct).Method2(b *testfixture1.Astruct) int\n\nFields:\n\tfield Xmember int\n\tfield Ymember int\n"},
	}))
	t.Run("use-of-variable-with-type-in-other-package", testDescribe("testfixture1/f.go", "g", "h", nil, Description{
		Info{Kind: InfoType, Text: "type: testfixture2.Bstruct", Pos: "$INTERNAL/testfixture2/f2.go:"},
		Info{Kind: InfoPos, Pos: "$INTERNAL/testfixture1/f.go:"},
	}))
	t.Run("call-to-iface-method-from-stdlib", testDescribe("testfixture1/s.go", "k", "l", nil, Description{
		Info{Kind: InfoSelection, Text: "method out.Write"},
		Info{Kind: InfoType, Text: "receiver: io.Writer", Pos: "src/io/io.go:"},
		Info{Kind: InfoType, Text: "type: func(p []byte) (n int, err error)"},
		Info{Kind: InfoPos, Pos: "src/io/io.go:"},
	}))
}

func TestExpansionDescribe(t *testing.T) {
	t.Run("call-to-func-in-same-file-exp-1", testDescribe("testfixture1/f.go", "a", "", nil, Description{
		Info{Kind: InfoFunction, Text: "func callable(x int) int"},
		Info{Kind: InfoPos, Pos: "$INTERNAL/testfixture1/f.go:"},
	}))
	t.Run("call-to-func-in-same-file-exp-3", testDescribe("testfixture1/f.go", "b-0", "", nil, Description{
		Info{Kind: InfoFunction, Text: "func callable(x int) int"},
		Info{Kind: InfoPos, Pos: "$INTERNAL/testfixture1/f.go:"},
	}))

	t.Run("call-to-func-in-different-file-1", testDescribe("testfixture1/f.go", "c", "", nil, Description{
		Info{Kind: InfoFunction, Text: "// callable2 is a blah blah blah\nfunc callable2(x int) int"},
		Info{Kind: InfoPos, Pos: "$INTERNAL/testfixture1/callable2.go:"},
	}))
	t.Run("call-to-func-in-different-file-3", testDescribe("testfixture1/f.go", "d-0", "", nil, Description{
		Info{Kind: InfoFunction, Text: "// callable2 is a blah blah blah\nfunc callable2(x int) int"},
		Info{Kind: InfoPos, Pos: "$INTERNAL/testfixture1/callable2.go:"},
	}))

	t.Run("call-to-func-in-different-package-1", testDescribe("testfixture1/f.go", "e", "", nil, Description{
		Info{Kind: InfoFunction, Text: "func Callable3(x int) int"},
		Info{Kind: InfoPos, Pos: "$INTERNAL/testfixture2/f2.go:"},
	}))
	t.Run("call-to-func-in-different-package-3", testDescribe("testfixture1/f.go", "f-0", "", nil, Description{
		Info{Kind: InfoFunction, Text: "func Callable3(x int) int"},
		Info{Kind: InfoPos, Pos: "$INTERNAL/testfixture2/f2.go:"},
	}))

	t.Run("call-to-method-1", testDescribe("testfixture1/s.go", "a", "", nil, Description{
		Info{Kind: InfoFunction, Text: "func (a *Astruct) Method2(b *Astruct) int"},
		Info{Kind: InfoPos, Pos: "$INTERNAL/testfixture1/s.go:"},
	}))
	t.Run("call-to-method-3", testDescribe("testfixture1/s.go", "b-0", "", nil, Description{
		Info{Kind: InfoFunction, Text: "func (a *Astruct) Method2(b *Astruct) int"},
		Info{Kind: InfoPos, Pos: "$INTERNAL/testfixture1/s.go:"},
	}))

	t.Run("call-with-package-selector-1", testDescribe("testfixture1/s.go", "c", "", nil, Description{
		Info{Kind: InfoFunction, Text: "@\nfunc Atoi(s string) (int, error)"},
		Info{Kind: InfoPos, Pos: "src/strconv/atoi.go:"},
	}))
	t.Run("call-with-package-selector-3", testDescribe("testfixture1/s.go", "d-0", "", nil, Description{
		Info{Kind: InfoFunction, Text: "@\nfunc Atoi(s string) (int, error)"},
		Info{Kind: InfoPos, Pos: "src/strconv/atoi.go:"},
	}))

	t.Run("use-of-local-var-1", testDescribe("testfixture1/s.go", "e", "", nil, Description{
		Info{Kind: InfoType, Text: "type: *testfixture1.Astruct", Pos: "$INTERNAL/testfixture1/s.go:"},
		Info{Kind: InfoPos, Pos: "$INTERNAL/testfixture1/s.go:"},
	}))
	t.Run("use-of-local-var-3", testDescribe("testfixture1/s.go", "f-0", "", nil, Description{
		Info{Kind: InfoType, Text: "type: *testfixture1.Astruct", Pos: "$INTERNAL/testfixture1/s.go:"},
		Info{Kind: InfoPos, Pos: "$INTERNAL/testfixture1/s.go:"},
	}))

	t.Run("use-of-member-field-1", testDescribe("testfixture1/s.go", "g", "", nil, Description{
		Info{Kind: InfoSelection, Text: "struct field a.Xmember"},
		Info{Kind: InfoType, Text: "receiver: *testfixture1.Astruct", Pos: "$INTERNAL/testfixture1/s.go:"},
		Info{Kind: InfoType, Text: "type: int"},
		Info{Kind: InfoPos, Pos: "$INTERNAL/testfixture1/s.go:"},
	}))
	t.Run("use-of-member-field-3", testDescribe("testfixture1/s.go", "h-0", "", nil, Description{
		Info{Kind: InfoSelection, Text: "struct field a.Xmember"},
		Info{Kind: InfoType, Text: "receiver: *testfixture1.Astruct", Pos: "$INTERNAL/testfixture1/s.go:"},
		Info{Kind: InfoType, Text: "type: int"},
		Info{Kind: InfoPos, Pos: "$INTERNAL/testfixture1/s.go:"},
	}))

	t.Run("use-of-blank-member-field-1", testDescribe("testfixture1/s.go", "i", "", nil, Description{
		Info{Kind: InfoType, Text: "receiver: *testfixture1.Astruct", Pos: "$INTERNAL/testfixture1/s.go:"},
		Info{Kind: InfoTypeContents, Text: "\nMethods:\n\tfunc (*testfixture1.Astruct).Method1(x int) int\n\tfunc (*testfixture1.Astruct).Method2(b *testfixture1.Astruct) int\n\nFields:\n\tfield Xmember int\n\tfield Ymember int\n"},
	}))
	t.Run("use-of-blank-member-field-3", testDescribe("testfixture1/s.go", "j-0", "", nil, Description{
		Info{Kind: InfoType, Text: "receiver: *testfixture1.Astruct", Pos: "$INTERNAL/testfixture1/s.go:"},
		Info{Kind: InfoTypeContents, Text: "\nMethods:\n\tfunc (*testfixture1.Astruct).Method1(x int) int\n\tfunc (*testfixture1.Astruct).Method2(b *testfixture1.Astruct) int\n\nFields:\n\tfield Xmember int\n\tfield Ymember int\n"},
	}))

	t.Run("use-of-variable-with-type-in-other-package-3", testDescribe("testfixture1/f.go", "h-0", "", nil, Description{
		Info{Kind: InfoType, Text: "type: testfixture2.Bstruct", Pos: "$INTERNAL/testfixture2/f2.go:"},
		Info{Kind: InfoPos, Pos: "$INTERNAL/testfixture1/f.go:"},
	}))
}

func TestModified(t *testing.T) {
	t.Run("call-to-func-in-same-file-modified-1", testDescribe("testfixture1/f.go", "i", "", nil, Description{}))
	//"\n"
	t.Run("call-to-func-in-same-file-modified-2", testDescribe("testfixture1/f.go", "i", "", []modifyfn{insert("i", "callable2(b.Xmember)")}, Description{
		Info{Kind: InfoFunction, Text: "// callable2 is a blah blah blah\nfunc callable2(x int) int"},
		Info{Kind: InfoPos, Pos: "$INTERNAL/testfixture1/callable2.go:"},
	}))
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

	t.Run("call-imported-function", testDescribe(filepath.Join(tmpdir, "t.go"), "a", "b", nil, Description{
		Info{Kind: InfoFunction, Text: "func Describe(path string, pos [2]int, cfg *Config)"},
		Info{Kind: InfoPos, Pos: "github.com/aarzilli/go2def@v0.0.0-20180921140844-57b3d798eea0/main.go:"},
	}))
	t.Run("call-function-in-other-file", testDescribe(filepath.Join(tmpdir, "t.go"), "c", "d", nil, Description{
		Info{Kind: InfoFunction, Text: "func callable2(x int)"},
		Info{Kind: InfoPos, Pos: "f.go:"},
	}))
}

func TestTests(t *testing.T) {
	t.Run("run-inside-test-1", testDescribe("testfixture1/testfixture1_test.go", "a", "b", nil, Description{
		Info{Kind: InfoFunction, Text: "@func (c *common) Fatalf(format string, args ...interface{})"},
		Info{Kind: InfoPos, Pos: "src/testing/testing.go:"},
	}))
	t.Run("run-inside-test-2", testDescribe("testfixture1/testfixture1_test.go", "c", "d", nil, Description{
		Info{Kind: InfoFunction, Text: "func somefn()"},
		Info{Kind: InfoPos, Pos: "$INTERNAL/testfixture1/testfixture1_test.go:"},
	}))
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

