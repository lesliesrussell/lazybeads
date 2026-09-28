// lb-4gm.3
package app

import (
	"context"
	"fmt"
	"strings"

	"github.com/lesliesrussell/lazybeads/internal/beads"
)

// Transport describes how the service currently reaches Beads.
type Transport struct {
	// Kind is "cli" or "http".
	Kind string `json:"kind"`
	// URL is the bd serve address when Kind is "http".
	URL string `json:"url,omitempty"`
	// Spawned is true when LazyBeads started that server itself.
	Spawned bool `json:"spawned,omitempty"`
	// Note explains why the CLI is in use when HTTP was configured or possible.
	Note string `json:"note,omitempty"`
	// Writes is true when mutations go over HTTP too; WritesNote says why
	// they do not.
	// lb-4gm.8
	Writes     bool   `json:"writes,omitempty"`
	WritesNote string `json:"writes_note,omitempty"`
}

// ServeStarter launches a `bd serve` for scope. Tests replace it.
type ServeStarter func(ctx context.Context, scope beads.Scope, tokenFile string) (*beads.ServeProcess, error)

// workspaceContexter is the part of the CLI client that reports storage mode.
type workspaceContexter interface {
	WorkspaceContext(ctx context.Context, scope beads.Scope) (beads.WorkspaceContext, error)
}

// ConnectServe moves the service onto bd serve's HTTP API when it can:
// a configured serve.url is always tried; with autoStart (long-lived
// sessions) and serve.auto_start = "auto", LazyBeads starts a server itself
// for workspaces backed by a Dolt server. Otherwise, and on any failure, the
// service stays on the bd CLI and Transport says why. The returned stop
// function is never nil and must be called when the session ends.
func (s *Service) ConnectServe(ctx context.Context, autoStart bool) func() {
	noop := func() {}
	if _, already := s.Client.(*beads.HTTP); already {
		return noop
	}
	s.Transport = Transport{Kind: "cli"}
	cfg := s.Config.Serve
	token, err := beads.ReadTokenFile(cfg.TokenFile)
	if err != nil {
		s.Transport.Note = "bd serve token: " + errText(err)
		return noop
	}
	var wc beads.WorkspaceContext
	if wcer, ok := s.Client.(workspaceContexter); ok {
		wc, err = wcer.WorkspaceContext(ctx, s.Scope())
		if err != nil {
			s.Transport.Note = "could not read the workspace mode: " + errText(err)
			return noop
		}
	}

	url, stop := cfg.URL, noop
	spawned := false
	// A server named in configuration is trusted only once it proves it
	// serves this workspace's Beads project.
	if url != "" && wc.ProjectID == "" {
		s.Transport.Note = "serve.url is set, but bd did not report this workspace's project id to check it against"
		return noop
	}
	if url == "" {
		switch {
		case !autoStart:
			return noop
		case cfg.AutoStart != "auto":
			s.Transport.Note = "serve.auto_start is never"
			return noop
		case !wc.ServesHTTP():
			s.Transport.Note = fmt.Sprintf("bd serve needs a Dolt server; this workspace uses %s Dolt", modeName(wc.DoltMode))
			return noop
		}
		start := s.StartServe
		if start == nil {
			start = s.Client.Runner().StartServe
		}
		p, err := start(ctx, s.Scope(), cfg.TokenFile)
		if err != nil {
			s.Transport.Note = "could not start bd serve: " + errText(err)
			return noop
		}
		url, stop, spawned = p.URL, p.Stop, true
	}

	writes, writesNote := s.httpWrites()
	h, err := beads.NewHTTP(ctx, beads.HTTPConfig{BaseURL: url, Token: token, ProjectID: wc.ProjectID,
		Actor: s.Actor, AllowWrites: writes}, s.Client)
	if err != nil {
		stop()
		s.Transport.Note = fmt.Sprintf("bd serve at %s: %s", url, errText(err))
		return noop
	}
	s.Client = h
	s.Transport = Transport{Kind: "http", URL: url, Spawned: spawned, Writes: writes, WritesNote: writesNote}
	s.InvalidateCache()
	return stop
}

func modeName(mode string) string {
	if mode == "" {
		return "embedded"
	}
	return mode
}

func errText(err error) string {
	if ce, ok := beads.AsCommandError(err); ok {
		return ce.Error()
	}
	return err.Error()
}

// httpWrites decides whether mutations go through bd serve. bd serve does
// not run bd's event hooks (on_create, on_update, on_close), so "auto" keeps
// writes on the CLI in a workspace that has any.
// lb-4gm.8
func (s *Service) httpWrites() (bool, string) {
	switch s.Config.Serve.HTTPWrites {
	case "never":
		return false, "serve.http_writes is never"
	case "always":
		return true, ""
	}
	if s.Actor == "" {
		return false, "no actor is configured to attribute HTTP writes to"
	}
	if hooks := beads.EventHooks(s.Workspace.BeadsDir); len(hooks) > 0 {
		return false, "the workspace has bd event hooks (" + strings.Join(hooks, ", ") + "), which HTTP writes would skip"
	}
	return true, ""
}
