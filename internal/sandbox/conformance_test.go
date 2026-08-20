package sandbox_test

import (
	"testing"

	"github.com/avivl/zeroth/internal/sandbox"
	"github.com/avivl/zeroth/internal/sandbox/docker"
)

func TestDriverConformance(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		driver sandbox.Driver
	}{
		{name: "docker", driver: docker.New()},
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
