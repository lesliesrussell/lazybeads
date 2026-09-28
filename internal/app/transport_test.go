// lb-4gm.3
package app

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lesliesrussell/lazybeads/internal/beads"
	"github.com/lesliesrussell/lazybeads/internal/domain"
)

// fixtureProject is the project_id recorded in http/context.json.
const fixtureProject = "880a36d1-4149-4801-98ed-59bb533b4b47"

func serveFixture(t *testing.T) *httptest.Server {
	t.Helper()
	ctxJSON, err := os.ReadFile(filepath.Join("..", "beads", "testdata", "bd-1.3.0", "http", "context.json"))
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v0/beads/context" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(ctxJSON)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func serverWorkspace() *MemClient {
	f := newFakeClient()
	f.WorkspaceCtx = beads.WorkspaceContext{DoltMode: "proxied-server", ProjectID: fixtureProject}
	return f
}

func TestConnectServeUsesConfiguredURL(t *testing.T) {
	srv := serveFixture(t)
	svc := newTestService(serverWorkspace())
	svc.Config.Serve.URL = srv.URL
	stop := svc.ConnectServe(context.Background(), false)
	defer stop()
	if _, ok := svc.Client.(*beads.HTTP); !ok || svc.Transport.Kind != "http" || svc.Transport.URL != srv.URL || svc.Transport.Spawned {
		t.Errorf("transport = %+v", svc.Transport)
	}
}

func TestConnectServeRefusesAnotherWorkspacesServer(t *testing.T) {
	srv := serveFixture(t)
	f := serverWorkspace()
	f.WorkspaceCtx.ProjectID = "some-other-project"
	svc := newTestService(f)
	svc.Config.Serve.URL = srv.URL
	svc.ConnectServe(context.Background(), false)()
	if svc.Transport.Kind != "cli" || !strings.Contains(svc.Transport.Note, "not this workspace") {
		t.Errorf("transport = %+v", svc.Transport)
	}
	if _, ok := svc.Client.(*MemClient); !ok {
		t.Error("the CLI must stay in place")
	}
}

func TestConnectServeFallsBackWhenConfiguredServerIsDown(t *testing.T) {
	srv := serveFixture(t)
	url := srv.URL
	srv.Close()
	svc := newTestService(serverWorkspace())
	svc.Config.Serve.URL = url
	svc.ConnectServe(context.Background(), true)()
	if svc.Transport.Kind != "cli" || !strings.Contains(svc.Transport.Note, url) {
		t.Errorf("transport = %+v", svc.Transport)
	}
}

func TestConnectServeAutoStart(t *testing.T) {
	srv := serveFixture(t)
	started := 0
	starter := func(context.Context, beads.Scope, string) (*beads.ServeProcess, error) {
		started++
		return &beads.ServeProcess{URL: srv.URL}, nil
	}

	// One-shot commands never start a server.
	svc := newTestService(serverWorkspace())
	svc.StartServe = starter
	svc.ConnectServe(context.Background(), false)()
	if started != 0 || svc.Transport.Kind != "cli" || svc.Transport.Note != "" {
		t.Errorf("one-shot: started=%d transport=%+v", started, svc.Transport)
	}

	// Long-lived sessions on a Dolt server workspace do.
	svc = newTestService(serverWorkspace())
	svc.StartServe = starter
	stop := svc.ConnectServe(context.Background(), true)
	if started != 1 || svc.Transport.Kind != "http" || !svc.Transport.Spawned {
		t.Errorf("long-lived: started=%d transport=%+v", started, svc.Transport)
	}
	stop()

	// Embedded workspaces cannot serve HTTP.
	svc = newTestService(newFakeClient())
	svc.StartServe = starter
	svc.ConnectServe(context.Background(), true)()
	if started != 1 || !strings.Contains(svc.Transport.Note, "embedded") {
		t.Errorf("embedded: started=%d transport=%+v", started, svc.Transport)
	}

	// auto_start = never is respected.
	svc = newTestService(serverWorkspace())
	svc.StartServe = starter
	svc.Config.Serve.AutoStart = "never"
	svc.ConnectServe(context.Background(), true)()
	if started != 1 || svc.Transport.Kind != "cli" {
		t.Errorf("never: started=%d transport=%+v", started, svc.Transport)
	}

	// A server that will not start leaves the CLI in place.
	svc = newTestService(serverWorkspace())
	svc.StartServe = func(context.Context, beads.Scope, string) (*beads.ServeProcess, error) {
		return nil, errors.New("port in use")
	}
	svc.ConnectServe(context.Background(), true)()
	if svc.Transport.Kind != "cli" || !strings.Contains(svc.Transport.Note, "port in use") {
		t.Errorf("start failure: transport=%+v", svc.Transport)
	}
}

func TestConnectServeBadTokenFileStaysOnCLI(t *testing.T) {
	svc := newTestService(serverWorkspace())
	svc.Config.Serve.URL = "http://127.0.0.1:1"
	svc.Config.Serve.TokenFile = filepath.Join(t.TempDir(), "missing")
	svc.ConnectServe(context.Background(), true)()
	if svc.Transport.Kind != "cli" || !strings.Contains(svc.Transport.Note, "token") {
		t.Errorf("transport = %+v", svc.Transport)
	}
}

// lb-4gm.3
func TestConnectServeNeedsProjectIDsForAConfiguredServer(t *testing.T) {
	srv := serveFixture(t)
	f := serverWorkspace()
	f.WorkspaceCtx.ProjectID = ""
	svc := newTestService(f)
	svc.Config.Serve.URL = srv.URL
	svc.ConnectServe(context.Background(), false)()
	if svc.Transport.Kind != "cli" || !strings.Contains(svc.Transport.Note, "project id") {
		t.Errorf("transport = %+v", svc.Transport)
	}

	anonymous := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"api_version":"v0","bd_version":"1.3.0","capabilities":["ready.list"]}`))
	}))
	defer anonymous.Close()
	svc = newTestService(serverWorkspace())
	svc.Config.Serve.URL = anonymous.URL
	svc.ConnectServe(context.Background(), false)()
	if svc.Transport.Kind != "cli" {
		t.Errorf("a server that hides its project was trusted: %+v", svc.Transport)
	}
}

// lb-4gm.4
func TestDoctorReportsJournalState(t *testing.T) {
	check := func(f *MemClient) domain.HealthCheck {
		got, err := newTestService(f).Doctor(context.Background(), DoctorRequest{})
		if err != nil {
			t.Fatal(err)
		}
		for _, c := range got.Health.Checks {
			if c.Name == "events_journal" {
				return c
			}
		}
		t.Fatal("no events_journal check")
		return domain.HealthCheck{}
	}
	if c := check(newFakeClient()); c.Level != domain.HealthOK {
		t.Errorf("on = %+v", c)
	}
	off := newFakeClient()
	off.JournalOff = true
	if c := check(off); c.Level != domain.HealthInfo || !strings.Contains(c.Hint, "bd config set events-journal true") {
		t.Errorf("off = %+v", c)
	}
}

// lb-4gm.5
func TestStartMirrorServesReadsAndStops(t *testing.T) {
	f := newFakeClient()
	f.Add(domain.Issue{ID: "lb-1", Title: "one", Status: domain.StatusOpen, Priority: 1})
	svc := newTestService(f)
	stop := svc.StartMirror(context.Background())
	if svc.Mirror == nil || svc.Client != beads.Client(svc.Mirror) {
		t.Fatal("the mirror must front the client")
	}
	deadline := time.Now().Add(5 * time.Second)
	for svc.Mirror.Status().State != "live" {
		if time.Now().After(deadline) {
			t.Fatalf("mirror = %+v", svc.Mirror.Status())
		}
		time.Sleep(5 * time.Millisecond)
	}
	svc.Refresh()
	stop()
	if got := svc.LiveMode(); got.Kind != "polling" {
		t.Errorf("a stopped mirror must not claim to be live: %+v", got)
	}
	if svc.Mirror.Status().State != "off" {
		t.Error("a stopped mirror answers nothing")
	}
}
