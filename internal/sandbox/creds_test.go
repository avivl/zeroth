package sandbox_test

import (
	"errors"
	"testing"

	"github.com/avivl/zeroth/internal/sandbox"
)

func TestValidCredPath(t *testing.T) {
	t.Parallel()
	cases := []struct {
		path    string
		wantErr bool
	}{
		{path: sandbox.CredsDir + "/git-credentials"},
		{path: sandbox.CredsDir + "/nested/token"},
		{path: "/tmp/token"},
		{path: "", wantErr: true},
		{path: "relative", wantErr: true},
		{path: sandbox.WorkspaceDir + "/token", wantErr: true},
		{path: sandbox.WorkspaceDir + "/../tmp/token"},
		{path: sandbox.WorkspaceDir + "/../workspace/token", wantErr: true},
		{path: "/tmp/../etc/passwd", wantErr: true},
		{path: sandbox.CredsDir, wantErr: true},
		{path: "/tmp", wantErr: true},
		{path: "/etc/passwd", wantErr: true},
		{path: "/run/other", wantErr: true},
	}
	for _, tc := range cases {
		name := tc.path
		if name == "" {
			name = "empty"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			err := sandbox.ValidCredPath(tc.path)
			if tc.wantErr {
				if !errors.Is(err, sandbox.ErrInvalid) {
					t.Fatalf("ValidCredPath(%q) = %v, want ErrInvalid", tc.path, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("ValidCredPath(%q) = %v", tc.path, err)
			}
		})
	}
}

func TestExcludedFromExport(t *testing.T) {
	t.Parallel()
	cases := []struct {
		rel  string
		want bool
	}{
		{rel: ".git-credentials", want: true},
		{rel: "subdir/.git-credentials", want: true},
		{rel: ".bash_history", want: true},
		{rel: ".config/gh/hosts.yml", want: true},
		{rel: ".config/gh", want: true},
		{rel: ".aws/credentials", want: true},
		{rel: ".claude.json", want: true},
		{rel: ".netrc", want: true},
		{rel: "notes.txt", want: false},
		{rel: ".config/app/settings.json", want: false},
		{rel: "src/main.go", want: false},
		{rel: "", want: false},
	}
	for _, tc := range cases {
		name := tc.rel
		if name == "" {
			name = "empty"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got := sandbox.ExcludedFromExport(tc.rel); got != tc.want {
				t.Fatalf("ExcludedFromExport(%q) = %v, want %v", tc.rel, got, tc.want)
			}
		})
	}
}
