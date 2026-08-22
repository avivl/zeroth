package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRejectRequiresComment(t *testing.T) {
	t.Parallel()
	cmd := newRoot()
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"reject", "p_1"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error")
	}
}

func TestRejectSendsCommentToDaemon(t *testing.T) {
	t.Parallel()
	var gotPath string
	var gotBody map[string]string
	hs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		if r.Method != http.MethodPost {
			t.Errorf("method %s", r.Method)
		}
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Errorf("decode: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{
			"id":"p_1",
			"run_id":"s_1",
			"status":"changes_requested",
			"summary":"edit README",
			"effects":[],
			"review_comment":"that heading doesn't exist, use the real one",
			"created_at":"2026-08-22T00:00:00Z",
			"updated_at":"2026-08-22T00:00:00Z"
		}`)
	}))
	t.Cleanup(hs.Close)
	host := strings.TrimPrefix(hs.URL, "http://")

	var out bytes.Buffer
	cmd := newRoot()
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{
		"--addr", host,
		"reject", "p_1",
		"--comment", "that heading doesn't exist, use the real one",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("reject: %v\n%s", err, out.String())
	}
	if !strings.Contains(gotPath, "/plans/p_1/request-changes") {
		t.Fatalf("path %s", gotPath)
	}
	if gotBody["comment"] != "that heading doesn't exist, use the real one" {
		t.Fatalf("body %+v", gotBody)
	}
	if strings.TrimSpace(out.String()) != "p_1" {
		t.Fatalf("stdout %q", out.String())
	}
}
