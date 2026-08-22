package plan

import (
	"strings"
	"testing"
)

func TestApplyPatchG4UnifiedDiff(t *testing.T) {
	t.Parallel()
	original := []byte("# demo\n")
	diff := "--- a/README.md\n+++ b/README.md\n@@ -1 +1,2 @@\n # demo\n+Version: 2\n"
	got, err := ApplyPatch(original, diff)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "# demo\nVersion: 2\n" {
		t.Fatalf("got %q", got)
	}
}

func TestApplyPatchMalformedHunkAppendsInsteadOfOverwrite(t *testing.T) {
	t.Parallel()
	original := []byte("# Zeroth\n\n## Why\nAgents work at machine speed.\n\n## Layout\ncmd/\n\n## License\nMIT\n")
	payload := "--- a/README.md\n+++ b/README.md\n@@\n+## Connecting Linear (assign-to-Zeroth)\n+\n+Assign an issue to the agent identity.\n"
	got, err := Materialize(OpModify, original, payload)
	if err != nil {
		t.Fatal(err)
	}
	body := string(got)
	for _, keep := range []string{"# Zeroth", "## Why", "Agents work at machine speed.", "## Layout", "## License", "MIT"} {
		if !strings.Contains(body, keep) {
			t.Fatalf("lost %q in %q", keep, body)
		}
	}
	if !strings.Contains(body, "## Connecting Linear (assign-to-Zeroth)") {
		t.Fatalf("missing added section: %q", body)
	}
	if strings.HasPrefix(strings.TrimSpace(body), "--- a/README.md") {
		t.Fatal("wrote the diff headers as file content")
	}
	if strings.Contains(body, "\n+## Connecting") {
		t.Fatal("left + prefixes in the file")
	}
}

func TestApplyPatchRawSearchReplace(t *testing.T) {
	t.Parallel()
	got, err := Materialize(OpModify, []byte("typo\n"), "-typo\n+fixed")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "fixed\n" {
		t.Fatalf("got %q", got)
	}
}

func TestApplyPatchConflict(t *testing.T) {
	t.Parallel()
	_, err := ApplyPatch([]byte("other\n"), "@@ -1 +1,2 @@\n # demo\n+Version: 2\n")
	if err == nil {
		t.Fatal("expected conflict")
	}
	if !strings.Contains(err.Error(), "conflict") {
		t.Fatalf("err %v", err)
	}
}

func TestMaterializeModifyRefusesShrinkingOverwrite(t *testing.T) {
	t.Parallel()
	var old strings.Builder
	old.WriteString("# Zeroth\n\n")
	for i := 0; i < 80; i++ {
		old.WriteString("This is a real overview paragraph that must survive apply.\n")
	}
	old.WriteString("## License\nMIT\n")
	payload := "## Connecting Linear\n\nAssign an issue.\n"
	_, err := Materialize(OpModify, []byte(old.String()), payload)
	if err == nil {
		t.Fatal("expected shrinking overwrite to be refused")
	}
	if !strings.Contains(err.Error(), "unified diff") && !strings.Contains(err.Error(), "replace") {
		t.Fatalf("err %v", err)
	}
}

func TestMaterializeModifyTinyFileMayReplace(t *testing.T) {
	t.Parallel()
	got, err := Materialize(OpModify, []byte("old\n"), "new\n")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "new\n" {
		t.Fatalf("got %q", got)
	}
}

func TestMaterializeCreateIsFullContents(t *testing.T) {
	t.Parallel()
	got, err := Materialize(OpCreate, nil, "# new file\n")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "# new file\n" {
		t.Fatalf("got %q", got)
	}
}

func TestLooksLikeDiff(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   string
		want bool
	}{
		{"--- a/README.md\n+++ b/README.md\n@@\n+hi\n", true},
		{"@@ -1 +1,2 @@\n # demo\n+x\n", true},
		{"-typo\n+fixed", true},
		{"+only added\n", true},
		{"new\n", false},
		{"# heading\nbody\n", false},
	}
	for _, tc := range cases {
		if got := LooksLikeDiff(tc.in); got != tc.want {
			t.Fatalf("%q: got %v want %v", tc.in, got, tc.want)
		}
	}
}

func TestPropertyModifyAddOnlyPreservesOriginal(t *testing.T) {
	t.Parallel()
	original := []byte("alpha\nbeta\ngamma\n")
	got, err := Materialize(OpModify, original, "+delta\n")
	if err != nil {
		t.Fatal(err)
	}
	body := string(got)
	for _, keep := range []string{"alpha", "beta", "gamma", "delta"} {
		if !strings.Contains(body, keep) {
			t.Fatalf("missing %q in %q", keep, body)
		}
	}
}
