//go:build !windows

// lb-4gm.3
package beads

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeBD writes a shell script standing in for bd. Its serve subcommand runs
// body; every invocation appends its argv to args.log.
func fakeBD(t *testing.T, body string) (bin, dir string) {
	t.Helper()
	dir = t.TempDir()
	bin = filepath.Join(dir, "bd")
	script := "#!/bin/sh\necho \"$@\" >> " + filepath.Join(dir, "args.log") + "\n" + body + "\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return bin, dir
}

func readyServer(t *testing.T) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v0/beads/ready" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"items":[],"has_more":false}`))
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestStartServeReadsAddressAndStops(t *testing.T) {
	srv := readyServer(t)
	bin, dir := fakeBD(t, `echo "bd serve: listening on `+srv.URL+`"; trap 'exit 0' TERM; while :; do sleep 0.05; done`)
	tokenFile := filepath.Join(dir, "token")
	if err := os.WriteFile(tokenFile, []byte("\n  s3cret \nold\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	p, err := NewRunner(bin).StartServe(context.Background(), Scope{Project: dir}, tokenFile)
	if err != nil {
		t.Fatalf("StartServe: %v", err)
	}
	if p.URL != srv.URL {
		t.Errorf("URL = %q, want %q", p.URL, srv.URL)
	}
	p.Stop()
	select {
	case <-p.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("server still running after Stop")
	}
	p.Stop() // idempotent
	log, _ := os.ReadFile(filepath.Join(dir, "args.log"))
	if !strings.Contains(string(log), "serve --addr 127.0.0.1:0 --auth-token-file "+tokenFile) {
		t.Errorf("argv = %s", log)
	}
	if strings.Contains(string(log), "s3cret") {
		t.Error("a token must never reach argv")
	}
}

func TestStartServeReportsEmbeddedRefusal(t *testing.T) {
	bin, dir := fakeBD(t, `echo 'Error: operation "serve" not supported by the embedded-dolt backend: bd serve requires a Dolt SQL server; this workspace uses embedded Dolt' >&2; exit 1`)
	_, err := NewRunner(bin).StartServe(context.Background(), Scope{Project: dir}, "")
	ce, ok := AsCommandError(err)
	if !ok || ce.Kind != ErrUnavailable || !strings.Contains(ce.Stderr, "embedded") {
		t.Errorf("err = %v", err)
	}
}

func TestStartServeTimesOutAndCleansUp(t *testing.T) {
	old := ServeStartTimeout
	ServeStartTimeout = 300 * time.Millisecond
	defer func() { ServeStartTimeout = old }()
	bin, dir := fakeBD(t, `trap 'exit 0' TERM; while :; do sleep 0.05; done`)
	start := time.Now()
	_, err := NewRunner(bin).StartServe(context.Background(), Scope{Project: dir}, "")
	if ce, ok := AsCommandError(err); !ok || ce.Kind != ErrUnavailable {
		t.Errorf("err = %v", err)
	}
	if time.Since(start) > 4*time.Second {
		t.Error("a silent server must be stopped promptly")
	}
}

func TestListeningURLOnlyTrustsLoopback(t *testing.T) {
	for line, want := range map[string]string{
		"bd serve: listening on http://127.0.0.1:64695": "http://127.0.0.1:64695",
		"bd serve: listening on http://[::1]:7777":      "http://[::1]:7777",
		"bd serve: listening on http://10.0.0.5:7777":   "",
		"bd serve: event=limits max_inflight=16":        "",
	} {
		got, _ := listeningURL(line)
		if got != want {
			t.Errorf("listeningURL(%q) = %q, want %q", line, got, want)
		}
	}
}

func TestIsLoopbackURL(t *testing.T) {
	for u, want := range map[string]bool{
		"http://127.0.0.1:7777":  true,
		"http://localhost:7777":  true,
		"http://[::1]:7777":      true,
		"http://192.168.1.2:80":  false,
		"http://evil.example:80": false,
		"https://127.0.0.1:7777": false,
		"127.0.0.1:7777":         false,
	} {
		if got := IsLoopbackURL(u); got != want {
			t.Errorf("IsLoopbackURL(%q) = %v", u, got)
		}
	}
}

func TestReadTokenFile(t *testing.T) {
	dir := t.TempDir()
	empty := filepath.Join(dir, "empty")
	_ = os.WriteFile(empty, []byte("\n \n"), 0o600)
	if _, err := ReadTokenFile(empty); err == nil {
		t.Error("a token file without a token is an error")
	}
	if tok, err := ReadTokenFile(""); tok != "" || err != nil {
		t.Errorf("no path = %q, %v", tok, err)
	}
	if _, err := ReadTokenFile(filepath.Join(dir, "missing")); err == nil {
		t.Error("a missing token file is an error")
	}
}

// lb-4gm.3
func TestStopDoesNotHangOnAChildHoldingStderr(t *testing.T) {
	srv := readyServer(t)
	// The background sleep inherits stderr and outlives the fake server, as a
	// dolt sql-server started by bd could.
	bin, dir := fakeBD(t, `sleep 30 & echo "bd serve: listening on `+srv.URL+`"; trap 'exit 0' TERM; while :; do sleep 0.05; done`)
	p, err := NewRunner(bin).StartServe(context.Background(), Scope{Project: dir}, "")
	if err != nil {
		t.Fatalf("StartServe: %v", err)
	}
	stopped := make(chan struct{})
	go func() { p.Stop(); close(stopped) }()
	select {
	case <-stopped:
	case <-time.After(12 * time.Second):
		t.Fatal("Stop hung on a grandchild holding stderr")
	}
}
