package harness_test

import (
	"testing"

	"github.com/avivl/zeroth/internal/harness"
	"github.com/avivl/zeroth/internal/harness/claudecode"
)

func TestDriverConformance(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		driver harness.Driver
	}{
		{name: "claudecode", driver: claudecode.New()},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if tc.driver == nil {
				t.Fatal("nil driver")
			}
			if got := tc.driver.Name(); got != tc.name {
				t.Fatalf("Name() = %q, want %q", got, tc.name)
			}
		})
	}
}
