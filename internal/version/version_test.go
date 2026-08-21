package version

import "testing"

func TestSHANotEmpty(t *testing.T) {
	t.Parallel()
	if got := SHA(); got == "" {
		t.Fatal("SHA() is empty")
	}
}
