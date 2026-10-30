// Command docgen writes the files that release archives and packages ship
// besides the binary: the bash, zsh, and fish completion scripts and the
// spmeta(1) man page, plain and gzip-compressed. All of them come from
// internal/reference. goreleaser runs it as a before hook:
//
//	go run ./internal/docgen -version 0.3.0 -date 2026-10-30T17:40:00Z dist-extras
//
// which writes dist-extras/completions/spmeta.{bash,zsh,fish} and
// dist-extras/manpages/spmeta.1{,.gz}.
package main

import (
	"bytes"
	"compress/gzip"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/bkuberek/spmeta/internal/reference"
)

const usage = "usage: docgen [-version VERSION] [-date DATE] DIR"

func main() {
	os.Exit(run(os.Args[1:], os.Stderr))
}

// run executes docgen with args and returns the exit code: 2 for a usage
// error, 1 when a file cannot be written.
func run(args []string, stderr io.Writer) int {
	set := flag.NewFlagSet("docgen", flag.ContinueOnError)
	set.SetOutput(stderr)
	set.Usage = func() {
		_, _ = fmt.Fprintln(stderr, usage)
		set.PrintDefaults()
	}
	version := set.String("version", "", "spmeta version for the man page header")
	date := set.String("date", "", "release date for the man page header, RFC 3339 or YYYY-MM-DD (default today)")
	if err := set.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if set.NArg() != 1 {
		_, _ = fmt.Fprintln(stderr, usage)
		return 2
	}
	day, err := manDate(*date)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "docgen: invalid -date %q: want RFC 3339 or YYYY-MM-DD\n", *date)
		return 2
	}
	if err := write(set.Arg(0), reference.ManInfo{Version: *version, Date: day}); err != nil {
		_, _ = fmt.Fprintf(stderr, "docgen: %v\n", err)
		return 1
	}
	return 0
}

// manDate turns the -date value into the YYYY-MM-DD the man page shows.
func manDate(s string) (string, error) {
	const day = "2006-01-02"
	if s == "" {
		return time.Now().UTC().Format(day), nil
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.UTC().Format(day), nil
	}
	t, err := time.Parse(day, s)
	if err != nil {
		return "", err
	}
	return t.Format(day), nil
}

// write writes the completion scripts and the man page under dir.
func write(dir string, info reference.ManInfo) error {
	type file struct {
		name string
		data []byte
	}
	var files []file
	for _, shell := range reference.Shells() {
		script, err := reference.Completion(shell)
		if err != nil {
			return err
		}
		files = append(files, file{filepath.Join("completions", "spmeta."+shell), []byte(script)})
	}
	page := []byte(reference.ManPage(info))
	gz, err := compress(page)
	if err != nil {
		return err
	}
	files = append(files,
		file{filepath.Join("manpages", "spmeta.1"), page},
		file{filepath.Join("manpages", "spmeta.1.gz"), gz},
	)
	for _, f := range files {
		path := filepath.Join(dir, f.name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(path, f.data, 0o644); err != nil {
			return err
		}
	}
	return nil
}

// compress gzips data at the best compression with an empty header, so the
// same input always gives the same bytes, as Debian expects of man pages.
func compress(data []byte) ([]byte, error) {
	var buf bytes.Buffer
	w, err := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	if err != nil {
		return nil, err
	}
	if _, err := w.Write(data); err != nil {
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
