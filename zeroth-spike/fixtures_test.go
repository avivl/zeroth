package spike_test

import (
	"bufio"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestCommittedSTarExists(t *testing.T) {
	t.Parallel()
	path := filepath.Join(fixturesDir(t), "S.tar")
	st, err := os.Stat(path)
	if err != nil {
		t.Fatalf("S.tar missing (commit the tar from fixtures/build.sh): %v", err)
	}
	// ~10 MB target. Allow a wide band so a generator tweak does not
	// fail CI. Still catch an empty or multi-hundred-MB file.
	if st.Size() < 5*1024*1024 || st.Size() > 20*1024*1024 {
		t.Fatalf("S.tar size = %d, want about 10 MB", st.Size())
	}
}

func TestManifestRecordsThreeSizes(t *testing.T) {
	t.Parallel()
	path := filepath.Join(fixturesDir(t), "MANIFEST.md")
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("MANIFEST.md: %v", err)
	}
	defer f.Close()

	seen := map[string]bool{"S": false, "M": false, "L": false}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if !strings.Contains(line, "|") {
			continue
		}
		for _, size := range []string{"S", "M", "L"} {
			if strings.HasPrefix(strings.TrimSpace(line), "| "+size+" ") {
				seen[size] = true
				if !strings.Contains(line, "tar") && !strings.Contains(line, "Tar") {
					// Row should mention the tar path or bytes column.
				}
				fields := strings.Split(line, "|")
				// | Size | Tar | Tar bytes | Unpacked bytes | Contents |
				if len(fields) < 5 {
					t.Fatalf("short manifest row: %q", line)
				}
				if strings.TrimSpace(fields[3]) == "" {
					t.Fatalf("%s tar bytes cell is empty", size)
				}
			}
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	for size, ok := range seen {
		if !ok {
			t.Fatalf("MANIFEST.md missing %s row", size)
		}
	}
}

func fixturesDir(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Join(filepath.Dir(file), "fixtures")
}
