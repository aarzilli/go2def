package main

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"log"
	"os"
	"strconv"
	"strings"

	"github.com/aarzilli/go2def"
)

const verbose = false

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
	case "describe":
		w := bufio.NewWriter(os.Stdout)
		defer w.Flush()
		describe(w, bufio.NewReader(os.Stdin), os.Args[2:])
	default:
		fmt.Printf("unknown command: %q\n", os.Args[1])

	}
}

func describe(out io.Writer, rd *bufio.Reader, args []string) {
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

	go2def.Describe(path, pos, &go2def.Config{Out: out, Modfiles: modfiles})
}

func parseDescribeArgs(out io.Writer, argv []string) (modified bool, path string, pos [2]int, ok bool) {
	if len(argv) <= 0 {
		fmt.Fprintf(out, "could not parse describe argument %q", argv)
		return
	}

	args := argv[0]

	if argv[0] == "-modified" {
		modified = true
		args = argv[1]
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
