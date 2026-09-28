// lb-4gm.2
package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lesliesrussell/lazybeads/internal/beads"
	"github.com/lesliesrussell/lazybeads/internal/config"
	"github.com/lesliesrussell/lazybeads/internal/workspace"
)

// TestServiceOverHTTPTransport runs the Ready and Show services over a
// `bd serve` replaying recorded 1.3.0 responses, with the in-memory client as
// the CLI fallback: the service must not notice which transport answered.
func TestServiceOverHTTPTransport(t *testing.T) {
	fixture := func(name string) []byte {
		data, err := os.ReadFile(filepath.Join("..", "beads", "testdata", "bd-1.3.0", "http", name))
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	routes := map[string]string{
		"/v0/beads/context":       "context.json",
		"/v0/beads/ready":         "ready.json",
		"/v0/beads/issues/sv-379": "issue-claimed.json",
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name, ok := routes[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(fixture(name))
	}))
	defer srv.Close()

	fallback := newFakeClient()
	client, err := beads.NewHTTP(context.Background(), beads.HTTPConfig{BaseURL: srv.URL}, fallback)
	if err != nil {
		t.Fatalf("NewHTTP: %v", err)
	}
	ws := workspace.Workspace{RootPath: "/fake", BeadsDir: "/fake/.beads", BDVersion: "1.3.0"}
	svc := NewService(client, config.Default(), ws, "operator", workspace.NewInProcessLocks())

	ready, err := svc.Ready(context.Background(), ReadyRequest{})
	if err != nil {
		t.Fatalf("Ready: %v", err)
	}
	if len(ready.Issues) != 1 || ready.Issues[0].Issue.ID != "sv-kk9" {
		t.Errorf("ready = %+v, want [sv-kk9]", ready.Issues)
	}
	shown, err := svc.Show(context.Background(), "sv-379", ShowRequest{})
	if err != nil {
		t.Fatalf("Show: %v", err)
	}
	if shown.Detail.ID != "sv-379" {
		t.Errorf("show = %+v", shown.Detail)
	}
	for _, call := range fallback.calls {
		if call == "ready" || strings.HasPrefix(call, "show:") {
			t.Errorf("%q reached the CLI fallback", call)
		}
	}
}
