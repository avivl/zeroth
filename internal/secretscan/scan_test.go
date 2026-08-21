package secretscan

import (
	"archive/tar"
	"bytes"
	"io"
	"strings"
	"testing"
)

func awsAccessKey() string {
	return "AKI" + "AZEROTHTESTFAKE01"
}

func githubPAT() string {
	return "ghp_" + strings.Repeat("A", 36)
}

func TestScanAWSAccessKey(t *testing.T) {
	t.Parallel()
	key := awsAccessKey()
	if len(key) != 20 {
		t.Fatalf("fixture length %d", len(key))
	}
	got := Scan("notes.txt", []byte("id="+key+"\n"))
	if len(got) != 1 {
		t.Fatalf("findings = %+v", got)
	}
	if got[0].Rule != "aws-access-key-id" || got[0].Path != "notes.txt" || got[0].Line != 1 {
		t.Fatalf("finding = %+v", got[0])
	}
	if strings.Contains(got[0].Rule, key) || strings.Contains(got[0].Path, key) {
		t.Fatal("finding leaked the secret")
	}
}

func TestScanGitHubPAT(t *testing.T) {
	t.Parallel()
	got := Scan("app.env", []byte("token="+githubPAT()))
	if len(got) != 1 || got[0].Rule != "github-pat" {
		t.Fatalf("findings = %+v", got)
	}
}

func TestScanClean(t *testing.T) {
	t.Parallel()
	if got := Scan("readme.md", []byte("no credentials here\n")); len(got) != 0 {
		t.Fatalf("false positive: %+v", got)
	}
}

func TestScanTar(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	body := "key=" + awsAccessKey() + "\n"
	hdr := &tar.Header{Name: "leak.txt", Mode: 0o644, Size: int64(len(body)), Typeflag: tar.TypeReg}
	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(tw, body); err != nil {
		t.Fatal(err)
	}
	clean := "ok\n"
	hdr = &tar.Header{Name: "ok.txt", Mode: 0o644, Size: int64(len(clean)), Typeflag: tar.TypeReg}
	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(tw, clean); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	got, err := ScanTar(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Path != "leak.txt" || got[0].Rule != "aws-access-key-id" {
		t.Fatalf("findings = %+v", got)
	}
}
