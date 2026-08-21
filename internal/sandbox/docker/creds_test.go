package docker

import (
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/avivl/zeroth/internal/sandbox"
)

func TestCredsTmpfsSpec(t *testing.T) {
	t.Parallel()
	got := credsTmpfsSpec()
	if !strings.HasPrefix(got, sandbox.CredsDir+":") {
		t.Fatalf("tmpfs spec = %q", got)
	}
	if !strings.Contains(got, "rw") || !strings.Contains(got, "exec") {
		t.Fatalf("tmpfs spec missing rw,exec: %q", got)
	}
	if os.Getuid() != 0 && !strings.Contains(got, "uid="+strconv.Itoa(os.Getuid())) {
		t.Fatalf("tmpfs spec missing uid: %q", got)
	}
}
