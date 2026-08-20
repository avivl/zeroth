package store_test

import (
	"testing"

	"github.com/avivl/zeroth/internal/store"
	"github.com/avivl/zeroth/internal/store/sqlite"
)

func TestStoreConformance(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		store store.Store
	}{
		{name: "sqlite", store: sqlite.New()},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if tc.store == nil {
				t.Fatal("nil store")
			}
			if got := tc.store.Name(); got != tc.name {
				t.Fatalf("Name() = %q, want %q", got, tc.name)
			}
			if err := tc.store.Close(); err != nil {
				t.Fatalf("Close() = %v", err)
			}
		})
	}
}
