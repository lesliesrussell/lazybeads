// lb-4gm.2
package beads

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/lesliesrussell/lazybeads/internal/domain"
)

// maxHTTPBody caps a single /v0 response, matching the CLI's stdout cap.
const maxHTTPBody = MaxStdoutCapture

// HTTPConfig locates a running `bd serve`.
type HTTPConfig struct {
	// BaseURL is the server root, e.g. http://127.0.0.1:7777.
	BaseURL string
	// Token is sent as a bearer credential when the server requires one. It is
	// never logged or included in errors.
	Token string
	// ProjectID, when set, is sent as Bd-Project-Id so a server for another
	// workspace refuses the request instead of answering it.
	// lb-4gm.3
	ProjectID string
	// HTTPClient overrides the transport; nil uses a client without a global
	// timeout, since each call is bounded by its context.
	HTTPClient *http.Client
}

// ServerInfo is what `GET /v0/beads/context` reports about the server.
type ServerInfo struct {
	APIVersion    string   `json:"api_version"`
	BDVersion     string   `json:"bd_version"`
	SchemaVersion int      `json:"schema_version"`
	Backend       string   `json:"backend"`
	DoltMode      string   `json:"dolt_mode"`
	Database      string   `json:"database"`
	ProjectID     string   `json:"project_id"`
	BeadsDir      string   `json:"beads_dir,omitempty"`
	RepoRoot      string   `json:"repo_root,omitempty"`
	Capabilities  []string `json:"capabilities"`
}

// Problem is an RFC 9457 error body from `bd serve`. Code is the only member
// the spec allows a client to dispatch on.
type Problem struct {
	Status    int    `json:"status"`
	Title     string `json:"title"`
	Code      string `json:"code"`
	Detail    string `json:"detail,omitempty"`
	Param     string `json:"param,omitempty"`
	Reason    string `json:"reason,omitempty"`
	RequestID string `json:"request_id,omitempty"`
	// Journal retention bounds, set on events_journal_truncated.
	Since int64 `json:"since,omitempty"`
	Floor int64 `json:"floor,omitempty"`
	Head  int64 `json:"head,omitempty"`
}

// HTTP talks to Beads through the `bd serve` /v0 API. Every operation the
// server does not advertise, and every read it cannot answer, goes to the
// embedded fallback Client (the bd CLI), so callers see one Client either way.
type HTTP struct {
	Client
	base  *url.URL
	token string
	// lb-4gm.3
	projectID string
	hc        *http.Client
	info      ServerInfo
	caps      map[string]bool
}

// NewHTTP connects to a `bd serve` and reads what it supports. It fails with
// ErrUnavailable when the server cannot be reached.
func NewHTTP(ctx context.Context, cfg HTTPConfig, fallback Client) (*HTTP, error) {
	if fallback == nil {
		return nil, &CommandError{Kind: ErrValidation, Operation: "connect to bd serve",
			Cause: fmt.Errorf("the HTTP transport needs the bd CLI as its fallback")}
	}
	base, err := url.Parse(strings.TrimRight(cfg.BaseURL, "/"))
	if err != nil || base.Scheme == "" || base.Host == "" {
		return nil, &CommandError{Kind: ErrValidation, Operation: "connect to bd serve",
			Cause: fmt.Errorf("invalid server URL %q", cfg.BaseURL)}
	}
	hc := cfg.HTTPClient
	if hc == nil {
		hc = &http.Client{}
	}
	h := &HTTP{Client: fallback, base: base, token: cfg.Token, projectID: cfg.ProjectID, hc: hc, caps: map[string]bool{}}
	cctx, cancel := scoped(ctx, Scope{})
	defer cancel()
	body, err := h.get(cctx, "context", "/v0/beads/context", nil)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(body, &h.info); err != nil {
		return nil, decodeErr(err, body)
	}
	for _, c := range h.info.Capabilities {
		h.caps[c] = true
	}
	// lb-4gm.3
	if cfg.ProjectID != "" && h.info.ProjectID != cfg.ProjectID {
		return nil, &CommandError{Kind: ErrWorkspaceNotFound, Operation: "connect to bd serve",
			Cause: fmt.Errorf("the server at %s serves Beads project %s, not this workspace (%s)", h.base, h.info.ProjectID, cfg.ProjectID)}
	}
	return h, nil
}

// Supports reports whether the server advertised an operation token such as
// "ready.list".
func (h *HTTP) Supports(op string) bool { return h.caps[op] }

// ServerInfo returns the server's self-description.
func (h *HTTP) ServerInfo() ServerInfo { return h.info }

// BaseURL is the server root this client talks to.
func (h *HTTP) BaseURL() string { return h.base.String() }

func (h *HTTP) Ready(ctx context.Context, q ReadyQuery) ([]domain.Issue, error) {
	if !h.Supports("ready.list") {
		return h.Client.Ready(ctx, q)
	}
	v := url.Values{}
	if q.PriorityMax != nil {
		// Like the CLI, the server has no ceiling filter: fetch everything and
		// enforce it locally.
		v.Set("limit", "0")
	} else if q.Limit > 0 {
		v.Set("limit", strconv.Itoa(q.Limit))
	}
	if q.Priority != nil {
		v.Set("priority", strconv.Itoa(*q.Priority))
	}
	addAll(v, "label", q.Labels)
	addAll(v, "label_any", q.LabelsAny)
	addAll(v, "exclude_label", q.ExcludeLabels)
	setIf(v, "type", q.Type)
	setIf(v, "assignee", q.Assignee)
	setIf(v, "parent", q.Parent)
	if q.Unassigned {
		v.Set("unassigned", "true")
	}
	rctx, cancel := scoped(ctx, q.Scope)
	defer cancel()
	issues, err := h.readItems(rctx, "ready", "/v0/beads/ready", v)
	if err != nil {
		if fallbackRead(err) {
			return h.Client.Ready(ctx, q)
		}
		return nil, err
	}
	if q.PriorityMax != nil {
		issues = filterPriorityMax(issues, *q.PriorityMax)
		if q.Limit > 0 && len(issues) > q.Limit {
			issues = issues[:q.Limit]
		}
	}
	return issues, nil
}

func (h *HTTP) List(ctx context.Context, q ListQuery) ([]domain.Issue, error) {
	// listIssues has no priority, updated-at, id or no-assignee filters and
	// serves only two orders; the CLI answers everything else.
	sortBy := q.Sort
	if sortBy == "" {
		// The server defaults to created order; `bd list` defaults to
		// priority, and a truncated page must hold the same issues.
		sortBy = "priority"
	}
	if !h.Supports("issues.list") || q.Priority != nil || q.PriorityMax != nil || q.PriorityMin != nil ||
		q.UpdatedAfter != "" || q.UpdatedBefore != "" || len(q.IDs) > 0 || q.NoAssignee ||
		(sortBy != "priority" && sortBy != "created") {
		return h.Client.List(ctx, q)
	}
	v := url.Values{"sort": {sortBy}}
	setIf(v, "status", q.Status)
	setIf(v, "type", q.Type)
	addAll(v, "label", q.Labels)
	addAll(v, "label_any", q.LabelsAny)
	addAll(v, "exclude_label", q.ExcludeLabels)
	setIf(v, "assignee", q.Assignee)
	setIf(v, "parent", q.Parent)
	setIf(v, "created_after", q.CreatedAfter)
	setIf(v, "created_before", q.CreatedBefore)
	if q.All {
		v.Set("all", "true")
	}
	// `bd list --json` without --limit returns every match despite its
	// documented default of 50, while the server applies that default; ask
	// for what the CLI actually returns.
	v.Set("limit", strconv.Itoa(q.Limit))
	rctx, cancel := scoped(ctx, q.Scope)
	defer cancel()
	issues, err := h.readItems(rctx, "list", "/v0/beads/issues", v)
	if err != nil && fallbackRead(err) {
		return h.Client.List(ctx, q)
	}
	return issues, err
}

func (h *HTTP) Show(ctx context.Context, id string, scope Scope) (domain.IssueDetail, error) {
	if err := validateID(id); err != nil {
		return domain.IssueDetail{}, err
	}
	if !h.Supports("issues.get") {
		return h.Client.Show(ctx, id, scope)
	}
	v := url.Values{"include_comments": {"true"}, "include_dependents": {"true"}}
	rctx, cancel := scoped(ctx, scope)
	defer cancel()
	body, err := h.get(rctx, "show", "/v0/beads/issues/"+url.PathEscape(id), v)
	if err != nil {
		// The server wants the canonical id; bd resolves a short or partial
		// one, so a miss is re-asked there.
		if ce, ok := AsCommandError(err); fallbackRead(err) || (ok && ce.Kind == ErrNotFound) {
			return h.Client.Show(ctx, id, scope)
		}
		return domain.IssueDetail{}, err
	}
	return decodeDetail(body)
}

func (h *HTTP) Stats(ctx context.Context, scope Scope) (Stats, error) {
	if !h.Supports("stats.get") {
		return h.Client.Stats(ctx, scope)
	}
	rctx, cancel := scoped(ctx, scope)
	defer cancel()
	body, err := h.get(rctx, "status", "/v0/beads/stats", nil)
	if err != nil {
		if fallbackRead(err) {
			return h.Client.Stats(ctx, scope)
		}
		return Stats{}, err
	}
	return decodeStats(body)
}

func (h *HTTP) Cycles(ctx context.Context, scope Scope) ([][]string, error) {
	if !h.Supports("dependencies.cycles") {
		return h.Client.Cycles(ctx, scope)
	}
	rctx, cancel := scoped(ctx, scope)
	defer cancel()
	body, err := h.get(rctx, "dep cycles", "/v0/beads/dependencies/cycles", nil)
	if err != nil {
		if fallbackRead(err) {
			return h.Client.Cycles(ctx, scope)
		}
		return nil, err
	}
	var page struct {
		Items json.RawMessage `json:"items"`
	}
	if err := json.Unmarshal(body, &page); err != nil {
		return nil, decodeErr(err, body)
	}
	return decodeCycles(page.Items)
}

// readItems fetches a page whose issues travel under "items". Callers pass
// limit=0 for an unlimited read; otherwise the server's page limit applies,
// exactly as the CLI's --limit does.
func (h *HTTP) readItems(ctx context.Context, op, path string, v url.Values) ([]domain.Issue, error) {
	body, err := h.get(ctx, op, path, v)
	if err != nil {
		return nil, err
	}
	var page struct {
		Items json.RawMessage `json:"items"`
	}
	if err := json.Unmarshal(body, &page); err != nil {
		return nil, decodeErr(err, body)
	}
	if len(page.Items) == 0 || string(page.Items) == "null" {
		return nil, nil
	}
	return decodeIssues(page.Items)
}

func (h *HTTP) get(ctx context.Context, op, path string, v url.Values) ([]byte, error) {
	return h.do(ctx, op, http.MethodGet, path, v, nil)
}

func (h *HTTP) do(ctx context.Context, op, method, path string, v url.Values, body io.Reader) ([]byte, error) {
	u := *h.base
	u.Path = strings.TrimRight(u.Path, "/") + path
	u.RawQuery = v.Encode()
	req, err := http.NewRequestWithContext(ctx, method, u.String(), body)
	if err != nil {
		return nil, &CommandError{Kind: ErrValidation, Operation: op, Cause: err}
	}
	req.Header.Set("Accept", "application/json, application/problem+json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if h.token != "" {
		req.Header.Set("Authorization", "Bearer "+h.token)
	}
	// lb-4gm.3
	if h.projectID != "" {
		req.Header.Set("Bd-Project-Id", h.projectID)
	}
	resp, err := h.hc.Do(req)
	if err != nil {
		return nil, transportError(ctx, op, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxHTTPBody))
	if err != nil {
		return nil, transportError(ctx, op, err)
	}
	if resp.StatusCode >= 400 {
		return nil, problemError(op, resp.StatusCode, resp.Header.Get("Content-Type"), data)
	}
	return data, nil
}

// transportError classifies a failure to talk to the server at all. The URL
// in the underlying error carries no credential: the token travels in a header.
func transportError(ctx context.Context, op string, err error) error {
	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		return &CommandError{Kind: ErrTimeout, Operation: op, Cause: err}
	case errors.Is(ctx.Err(), context.Canceled):
		return &CommandError{Kind: ErrCancelled, Operation: op, Cause: err}
	}
	return &CommandError{Kind: ErrUnavailable, Operation: op, Cause: err}
}

// problemError maps a `bd serve` failure onto the error taxonomy by its
// machine-readable code, falling back to the status class for codes this
// build does not know, as the spec requires.
func problemError(op string, status int, contentType string, body []byte) error {
	ce := &CommandError{Operation: op, ExitCode: status}
	var p Problem
	mt, _, _ := mime.ParseMediaType(contentType)
	if (mt == "application/problem+json" || mt == "application/json") && json.Unmarshal(body, &p) == nil && p.Code != "" {
		ce.Problem = &p
		ce.Stderr = firstNonEmptyString(p.Detail, p.Title)
	} else {
		ce.Stderr = truncateForError(string(body))
		p = Problem{Status: status}
	}
	switch p.Code {
	case "not_found":
		ce.Kind = ErrNotFound
	case "invalid_argument", "invalid_cursor":
		ce.Kind = ErrValidation
	case "already_claimed", "not_claimable", "not_closable", "not_releasable",
		"dependency_cycle", "dependency_exists", "already_exists", "precondition_failed":
		ce.Kind = ErrConflict
	case "events_journal_disabled":
		ce.Kind = ErrJournalDisabled
	case "events_journal_truncated":
		ce.Kind = ErrJournalTruncated
	case "busy", "db_unavailable", "events_watch_saturated":
		ce.Kind = ErrUnavailable
	case "unauthenticated":
		ce.Kind = ErrBDExecution
		ce.Hint = "The server requires a token; configure the token file LazyBeads should send."
	default:
		if status == http.StatusServiceUnavailable {
			ce.Kind = ErrUnavailable
		} else {
			ce.Kind = ErrBDExecution
		}
	}
	return ce
}

// fallbackRead reports whether a failed read should be retried through the
// CLI: the server is gone or overloaded, or it refused an argument (an older
// server without a parameter, or limit=0 on a non-loopback bind) that the CLI
// still accepts. A genuinely bad argument fails again there, in bd's words.
func fallbackRead(err error) bool {
	ce, ok := AsCommandError(err)
	if !ok {
		return false
	}
	return ce.Kind == ErrUnavailable || (ce.Problem != nil && ce.Problem.Code == "invalid_argument")
}

func decodeStats(out []byte) (Stats, error) {
	var payload struct {
		Summary struct {
			Total      int `json:"total_issues"`
			Open       int `json:"open_issues"`
			InProgress int `json:"in_progress_issues"`
			Blocked    int `json:"blocked_issues"`
			Ready      int `json:"ready_issues"`
			Closed     int `json:"closed_issues"`
			Deferred   int `json:"deferred_issues"`
		} `json:"summary"`
	}
	data := trimJSON(out)
	if len(data) == 0 {
		return Stats{}, nil
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return Stats{}, decodeErr(err, data)
	}
	return Stats{
		Total:      payload.Summary.Total,
		Open:       payload.Summary.Open,
		InProgress: payload.Summary.InProgress,
		Blocked:    payload.Summary.Blocked,
		Ready:      payload.Summary.Ready,
		Closed:     payload.Summary.Closed,
		Deferred:   payload.Summary.Deferred,
		Available:  true,
	}, nil
}

func setIf(v url.Values, key, val string) {
	if val != "" {
		v.Set(key, val)
	}
}

func addAll(v url.Values, key string, vals []string) {
	for _, val := range vals {
		v.Add(key, val)
	}
}

var _ Client = (*HTTP)(nil)

// scoped bounds a call by the scope's timeout, as the CLI runner does.
func scoped(ctx context.Context, scope Scope) (context.Context, context.CancelFunc) {
	timeout := scope.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	return context.WithTimeout(ctx, timeout)
}
