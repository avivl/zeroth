package tracker_test

import (
	"testing"

	"github.com/avivl/zeroth/internal/tracker"
	"github.com/avivl/zeroth/internal/tracker/linear"
)

func TestProviderConformance(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		provider tracker.Provider
	}{
		{name: "linear", provider: linear.New()},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if tc.provider == nil {
				t.Fatal("nil provider")
			}
			if got := tc.provider.Name(); got != tc.name {
				t.Fatalf("Name() = %q, want %q", got, tc.name)
			}
		})
	}
}
