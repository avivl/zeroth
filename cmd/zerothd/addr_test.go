package main

import "testing"

func TestResolveAddr(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		flagAddr string
		envAddr  string
		want     string
	}{
		{name: "default", want: defaultAddr},
		{name: "env", envAddr: "127.0.0.1:9000", want: "127.0.0.1:9000"},
		{name: "flag wins over env", flagAddr: "127.0.0.1:1", envAddr: "127.0.0.1:2", want: "127.0.0.1:1"},
		{name: "whitespace flag ignored", flagAddr: "  ", envAddr: "127.0.0.1:9", want: "127.0.0.1:9"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := resolveAddr(tc.flagAddr, tc.envAddr)
			if got != tc.want {
				t.Fatalf("resolveAddr(%q, %q) = %q, want %q", tc.flagAddr, tc.envAddr, got, tc.want)
			}
		})
	}
}

func TestParseAddrFlag(t *testing.T) {
	t.Parallel()
	got, err := parseAddr([]string{"--addr", "127.0.0.1:7999"})
	if err != nil {
		t.Fatalf("parseAddr: %v", err)
	}
	if got != "127.0.0.1:7999" {
		t.Fatalf("parseAddr --addr = %q, want 127.0.0.1:7999", got)
	}
}
