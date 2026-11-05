// Command goifmt formats .goi design files the way gofmt formats Go source.
//
//	goifmt -l .            # list the files that would change
//	goifmt -w file.goi     # rewrite them in place
//	goifmt -d file.goi     # print a unified diff
//
// A file that does not parse is left alone and reported on stderr, so a
// half-typed design never loses its layout.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/goi-gui/goi/internal/goifmt"
	"github.com/goi-gui/goi/internal/syntax"
)

func main() {
	list := flag.Bool("l", false, "list files whose formatting differs")
	write := flag.Bool("w", false, "write the result back to the file")
	diff := flag.Bool("d", false, "print a diff instead of the formatted file")
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: goifmt [-l] [-w] [-d] path...")
		flag.PrintDefaults()
	}
	flag.Parse()
	if flag.NArg() == 0 {
		flag.Usage()
		os.Exit(2)
	}

	files, err := collect(flag.Args())
	if err != nil {
		fmt.Fprintln(os.Stderr, "goifmt:", err)
		os.Exit(1)
	}
	bad := false
	for _, name := range files {
		src, err := os.ReadFile(name)
		if err != nil {
			fmt.Fprintln(os.Stderr, "goifmt:", err)
			bad = true
			continue
		}
		if _, diag := syntax.Parse(src); !diag.Empty() {
			fmt.Fprintf(os.Stderr, "goifmt: %s: %s\n", name, diag[0].Message)
			bad = true
			continue
		}
		out := goifmt.Source(src)
		if bytes.Equal(src, out) {
			continue
		}
		switch {
		case *list:
			fmt.Println(name)
		case *write:
			if err := os.WriteFile(name, out, 0o644); err != nil {
				fmt.Fprintln(os.Stderr, "goifmt:", err)
				bad = true
			}
		case *diff:
			os.Stdout.Write(unified(name, src, out))
		default:
			os.Stdout.Write(out)
		}
	}
	if bad {
		os.Exit(1)
	}
}

// collect expands the arguments: a directory is walked for .goi files.
func collect(args []string) ([]string, error) {
	var out []string
	for _, arg := range args {
		info, err := os.Stat(arg)
		if err != nil {
			return nil, err
		}
		if !info.IsDir() {
			out = append(out, arg)
			continue
		}
		err = filepath.WalkDir(arg, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !d.IsDir() && strings.HasSuffix(path, ".goi") {
				out = append(out, path)
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

// unified prints a line diff. It is deliberately small: the formatter rewrites
// whole files, so a plain line diff is all a reader needs.
func unified(name string, a, b []byte) []byte {
	old := strings.Split(strings.TrimSuffix(string(a), "\n"), "\n")
	new := strings.Split(strings.TrimSuffix(string(b), "\n"), "\n")
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "--- %s\n+++ %s\n", name, name)
	i, j := 0, 0
	for i < len(old) || j < len(new) {
		switch {
		case i < len(old) && j < len(new) && old[i] == new[j]:
			fmt.Fprintf(&buf, " %s\n", old[i])
			i++
			j++
		case i < len(old):
			fmt.Fprintf(&buf, "-%s\n", old[i])
			i++
		default:
			fmt.Fprintf(&buf, "+%s\n", new[j])
			j++
		}
	}
	return buf.Bytes()
}
