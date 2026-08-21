// Command genfixtures writes deterministic S scripts and L binary padding.
//
// M is a real git clone plus module cache (see fixtures/build.sh). L starts
// from that tree; this command only adds incompressible assets.
package main

import (
	"crypto/sha256"
	"encoding/binary"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	kind := flag.String("kind", "", "S, Lpad, or Mfill")
	out := flag.String("out", "", "output directory")
	src := flag.String("src", "", "source directory (Mfill)")
	bytes := flag.Int64("bytes", 0, "target size in bytes")
	flag.Parse()
	if *out == "" || *bytes <= 0 {
		fmt.Fprintln(os.Stderr, "usage: genfixtures -kind S|Lpad|Mfill -out DIR -bytes N [-src DIR]")
		os.Exit(2)
	}
	if err := os.MkdirAll(*out, 0o755); err != nil {
		fatal(err)
	}
	switch *kind {
	case "S":
		if err := writeS(*out, *bytes); err != nil {
			fatal(err)
		}
	case "Lpad":
		if err := writeLpad(*out, *bytes); err != nil {
			fatal(err)
		}
	case "Mfill":
		if *src == "" {
			fmt.Fprintln(os.Stderr, "Mfill requires -src")
			os.Exit(2)
		}
		if err := fillM(*src, *out, *bytes); err != nil {
			fatal(err)
		}
	default:
		fmt.Fprintf(os.Stderr, "unknown kind %q\n", *kind)
		os.Exit(2)
	}
}

func fatal(err error) {
	fmt.Fprintf(os.Stderr, "genfixtures: %v\n", err)
	os.Exit(1)
}

func writeS(dir string, target int64) error {
	scripts := filepath.Join(dir, "scripts")
	if err := os.MkdirAll(scripts, 0o755); err != nil {
		return err
	}
	const files = 40
	per := target / files
	readme := []byte("# Spike fixture S\n\nShell scripts totaling about 10 MB. Content is\ndeterministic so compression measurements are repeatable.\n")
	if err := os.WriteFile(filepath.Join(dir, "README"), readme, 0o644); err != nil {
		return err
	}
	for i := 0; i < files; i++ {
		path := filepath.Join(scripts, fmt.Sprintf("s%02d.sh", i))
		f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
		if err != nil {
			return err
		}
		header := fmt.Sprintf("#!/bin/sh\n# zeroth-spike fixture S script %d\necho s%02d\n# payload follows (sha256 stream, not /dev/zero)\n", i, i)
		if _, err := f.WriteString(header); err != nil {
			_ = f.Close()
			return err
		}
		if err := writeStream(f, fmt.Sprintf("S-%d", i), per-int64(len(header))); err != nil {
			_ = f.Close()
			return err
		}
		if err := f.Close(); err != nil {
			return err
		}
	}
	return nil
}

func writeLpad(dir string, target int64) error {
	assets := filepath.Join(dir, "assets")
	if err := os.MkdirAll(assets, 0o755); err != nil {
		return err
	}
	path := filepath.Join(assets, "binaries.bin")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	if err := writeStream(f, "L-assets", target); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

func writeStream(f *os.File, seed string, n int64) error {
	if n <= 0 {
		return nil
	}
	buf := make([]byte, 1<<20)
	var counter uint64
	var ctr [8]byte
	for n > 0 {
		off := 0
		for off+sha256.Size <= len(buf) {
			h := sha256.New()
			_, _ = h.Write([]byte(seed))
			binary.BigEndian.PutUint64(ctr[:], counter)
			_, _ = h.Write(ctr[:])
			counter++
			off += copy(buf[off:], h.Sum(nil))
		}
		chunk := buf[:off]
		if int64(len(chunk)) > n {
			chunk = chunk[:n]
		}
		if _, err := f.Write(chunk); err != nil {
			return err
		}
		n -= int64(len(chunk))
	}
	return nil
}

func fillM(src, dst string, target int64) error {
	var copied int64
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		if rel == "cache" || strings.HasPrefix(rel, "golang.org"+string(os.PathSeparator)+"toolchain@") {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.Contains(rel, "golang.org"+string(os.PathSeparator)+"toolchain@") {
			return nil
		}
		dest := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(dest, 0o755)
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		if copied >= target {
			return filepath.SkipAll
		}
		if err := copyFile(path, dest); err != nil {
			return err
		}
		copied += info.Size()
		return nil
	})
}

func copyFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}
