package secretscan

import (
	"archive/tar"
	"bytes"
	"fmt"
	"io"
	"path"
	"regexp"
	"strings"
)

// Finding is one detector hit. The matched secret is never included.
type Finding struct {
	Path string
	Rule string
	Line int
}

type detector struct {
	rule string
	re   *regexp.Regexp
}

var detectors = []detector{
	{rule: "aws-access-key-id", re: regexp.MustCompile(`AKIA[0-9A-Z]{16}`)},
	{rule: "github-pat", re: regexp.MustCompile(`ghp_[A-Za-z0-9]{36}`)},
	{rule: "github-fine-grained", re: regexp.MustCompile(`github_pat_[A-Za-z0-9_]{20,}`)},
	{rule: "anthropic-api-key", re: regexp.MustCompile(`sk-ant-[A-Za-z0-9_-]{20,}`)},
	{rule: "slack-token", re: regexp.MustCompile(`xox[baprs]-[A-Za-z0-9-]{10,}`)},
	{rule: "private-key", re: regexp.MustCompile(`-----BEGIN [A-Z ]{0,20}PRIVATE KEY-----`)},
}

const maxLine = 1024 * 1024

// Scan returns findings in data for a workspace-relative name.
func Scan(name string, data []byte) []Finding {
	if len(data) == 0 {
		return nil
	}
	var out []Finding
	line := 1
	start := 0
	for i := 0; i < len(data); i++ {
		if data[i] != '\n' {
			continue
		}
		out = append(out, scanLine(name, line, data[start:i])...)
		line++
		start = i + 1
	}
	if start < len(data) {
		out = append(out, scanLine(name, line, data[start:])...)
	}
	return out
}

func scanLine(name string, line int, chunk []byte) []Finding {
	if len(chunk) == 0 {
		return nil
	}
	if len(chunk) > maxLine {
		chunk = chunk[:maxLine]
	}
	var out []Finding
	for _, d := range detectors {
		if d.re.Find(chunk) == nil {
			continue
		}
		out = append(out, Finding{Path: name, Rule: d.rule, Line: line})
	}
	return out
}

// ScanTar reads an uncompressed tar and returns findings in regular
// files. It does not error on a hit: the caller fails closed.
func ScanTar(r io.Reader) ([]Finding, error) {
	tr := tar.NewReader(r)
	var out []Finding
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return out, nil
		}
		if err != nil {
			return nil, fmt.Errorf("secretscan tar: %w", err)
		}
		if hdr.Typeflag != tar.TypeReg && hdr.Typeflag != 0 {
			continue
		}
		name := strings.TrimPrefix(path.Clean("/"+strings.ReplaceAll(hdr.Name, `\`, "/")), "/")
		if name == "" || name == "." {
			continue
		}
		findings, err := scanReader(name, tr)
		if err != nil {
			return nil, fmt.Errorf("secretscan %s: %w", name, err)
		}
		out = append(out, findings...)
	}
}

func scanReader(name string, r io.Reader) ([]Finding, error) {
	var (
		out  []Finding
		line = 1
		buf  []byte
		tmp  = make([]byte, 32*1024)
	)
	for {
		n, err := r.Read(tmp)
		if n > 0 {
			buf = append(buf, tmp[:n]...)
			for {
				i := bytes.IndexByte(buf, '\n')
				if i < 0 {
					break
				}
				out = append(out, scanLine(name, line, buf[:i])...)
				line++
				buf = buf[i+1:]
			}
			if len(buf) > maxLine {
				out = append(out, scanLine(name, line, buf[:maxLine])...)
				buf = buf[maxLine:]
			}
		}
		if err == io.EOF {
			if len(buf) > 0 {
				out = append(out, scanLine(name, line, buf)...)
			}
			return out, nil
		}
		if err != nil {
			return nil, err
		}
	}
}
