package docker

import "testing"

func TestName(t *testing.T) {
	t.Parallel()
	if got := New().Name(); got != driverName {
		t.Fatalf("Name() = %q, want %q", got, driverName)
	}
}
