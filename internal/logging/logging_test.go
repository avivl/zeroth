package logging

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func TestParseLevel(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in      string
		want    zapcore.Level
		wantErr bool
	}{
		{in: "", want: zapcore.InfoLevel},
		{in: " debug ", want: zapcore.DebugLevel},
		{in: "INFO", want: zapcore.InfoLevel},
		{in: "warn", want: zapcore.WarnLevel},
		{in: "error", want: zapcore.ErrorLevel},
		{in: "fatal", wantErr: true},
		{in: "nope", wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			t.Parallel()
			got, err := ParseLevel(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("ParseLevel(%q) err = nil", tc.in)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseLevel(%q): %v", tc.in, err)
			}
			if got != tc.want {
				t.Fatalf("ParseLevel(%q) = %s, want %s", tc.in, got, tc.want)
			}
		})
	}
}

func TestParseEncoding(t *testing.T) {
	t.Parallel()
	got, err := ParseEncoding("")
	if err != nil || got != "console" {
		t.Fatalf("empty encoding: got %q err %v", got, err)
	}
	got, err = ParseEncoding("JSON")
	if err != nil || got != "json" {
		t.Fatalf("json encoding: got %q err %v", got, err)
	}
	if _, err := ParseEncoding("xml"); err == nil {
		t.Fatal("expected error for xml")
	}
}

func TestNewJSONInfoDropsDebug(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	log, err := New(Options{Level: "info", Encoding: "json", Writer: &buf})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	log.Debug("hidden")
	log.Info("visible", zap.String("k", "v"))
	_ = log.Sync()

	out := buf.String()
	if strings.Contains(out, "hidden") {
		t.Fatalf("debug leaked at info: %s", out)
	}
	var rec struct {
		Msg string `json:"msg"`
		K   string `json:"k"`
	}
	if err := json.Unmarshal([]byte(out), &rec); err != nil {
		t.Fatalf("json: %v\n%s", err, out)
	}
	if rec.Msg != "visible" {
		t.Fatalf("msg = %q, want visible", rec.Msg)
	}
	if rec.K != "v" {
		t.Fatalf("k = %q, want v", rec.K)
	}
}

func TestNewConsole(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	log, err := New(Options{Encoding: "console", Writer: &buf})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	log.Info("hello")
	_ = log.Sync()
	if !strings.Contains(buf.String(), "hello") {
		t.Fatalf("console log missing hello: %s", buf.String())
	}
	if strings.HasPrefix(strings.TrimSpace(buf.String()), "{") {
		t.Fatalf("console encoding looked like json: %s", buf.String())
	}
}

func TestFromContextNopWithoutLogger(t *testing.T) {
	t.Parallel()
	FromContext(t.Context()).Info("must not panic")
}
