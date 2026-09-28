// lb-4gm.8
package beads

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/lesliesrussell/lazybeads/internal/domain"
)

// The `bd create` flag defaults.
const (
	cliDefaultType     = "task"
	cliDefaultPriority = 2
)

// EventHookNames are the bd hooks a mutation made through bd serve does not
// run (bd documents that HTTP writes skip them).
var EventHookNames = []string{"on_create", "on_update", "on_close"}

// EventHooks lists the bd event hooks installed in a beads directory.
func EventHooks(beadsDir string) []string {
	var found []string
	for _, name := range EventHookNames {
		if st, err := os.Stat(filepath.Join(beadsDir, "hooks", name)); err == nil && !st.IsDir() {
			found = append(found, name)
		}
	}
	return found
}

// writes reports whether mutations may go over HTTP: the caller allowed it,
// there is an actor to attribute them to, and the server serves the op.
func (h *HTTP) writes(op string) bool {
	return h.allowWrites && h.actor != "" && h.Supports(op)
}

// sendWrite posts a mutation. It falls back to the CLI only when the request
// provably never reached the server (nothing listening): a write that may
// have landed is never repeated.
func (h *HTTP) sendWrite(ctx context.Context, op, method, path string, body any) ([]byte, bool, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return nil, false, &CommandError{Kind: ErrValidation, Operation: op, Cause: err}
	}
	out, err := h.do(ctx, op, method, path, nil, bytes.NewReader(data))
	if err != nil && notDelivered(err) {
		return nil, true, err
	}
	return out, false, err
}

// notDelivered is true only when the connection itself failed: the request
// was never sent, so running it through the CLI instead cannot apply it
// twice. (A dial that timed out is already classified as ErrTimeout.)
func notDelivered(err error) bool {
	ce, ok := AsCommandError(err)
	if !ok || ce.Kind != ErrUnavailable || ce.Problem != nil {
		return false
	}
	var opErr *net.OpError
	return errors.As(ce.Cause, &opErr) && opErr.Op == "dial"
}

// issueFrom reads the issue a write response carries, either bare or under
// "issue".
func issueFrom(op string, body []byte) (domain.Issue, error) {
	var wrapped struct {
		Issue json.RawMessage `json:"issue"`
	}
	raw := body
	if json.Unmarshal(body, &wrapped) == nil && len(wrapped.Issue) > 0 && string(wrapped.Issue) != "null" {
		raw = wrapped.Issue
	}
	issues, err := decodeIssues(raw)
	if err != nil {
		return domain.Issue{}, err
	}
	if len(issues) == 0 {
		return domain.Issue{}, &CommandError{Kind: ErrDecode, Operation: op, Cause: fmt.Errorf("bd serve returned no issue")}
	}
	return issues[0], nil
}

func issuePath(id, action string) string {
	return "/v0/beads/issues/" + url.PathEscape(id) + action
}

func (h *HTTP) Create(ctx context.Context, in CreateIssueInput) (domain.Issue, error) {
	// Files, dates, metadata and dependency specs keep bd's own parsing.
	if !h.writes("issues.create") || in.DryRun || in.DescriptionFile != "" || in.Due != "" || in.Defer != "" ||
		len(in.Metadata) > 0 || len(in.Deps) > 0 {
		return h.Client.Create(ctx, in)
	}
	if err := ValidateTitle(in.Title); err != nil {
		return domain.Issue{}, err
	}
	// bd serve's defaults differ from `bd create`'s (priority 0, and no type
	// is refused), so send the CLI's: an issue lb creates looks the same
	// whichever way it went.
	body := map[string]any{"actor": h.actor, "title": in.Title, "issue_type": cliDefaultType, "priority": cliDefaultPriority}
	setIfNonEmpty(body, "description", in.Description)
	setIfNonEmpty(body, "issue_type", in.Type)
	setIfNonEmpty(body, "assignee", in.Assignee)
	setIfNonEmpty(body, "parent_id", in.Parent)
	if in.Parent != "" {
		// bd create --parent inherits the parent's labels; the API does
		// only when asked.
		body["inherit_labels_from_parent"] = true
	}
	if in.Priority != nil {
		body["priority"] = *in.Priority
	}
	if len(in.Labels) > 0 {
		body["labels"] = in.Labels
	}
	wctx, cancel := scoped(ctx, in.Scope)
	defer cancel()
	out, fallback, err := h.sendWrite(wctx, "create", http.MethodPost, "/v0/beads/issues", body)
	if fallback {
		return h.Client.Create(ctx, in)
	}
	if err != nil {
		return domain.Issue{}, err
	}
	return issueFrom("create", out)
}

func (h *HTTP) Update(ctx context.Context, id string, in UpdateIssueInput) (domain.Issue, error) {
	if err := validateID(id); err != nil {
		return domain.Issue{}, err
	}
	if !h.writes("issues.update") || len(in.Metadata) > 0 || in.Due != nil || in.Defer != nil {
		return h.Client.Update(ctx, id, in)
	}
	if in.Title != nil {
		if err := ValidateTitle(*in.Title); err != nil {
			return domain.Issue{}, err
		}
	}
	patch := map[string]any{}
	setIfSet(patch, "title", in.Title)
	setIfSet(patch, "description", in.Description)
	setIfSet(patch, "issue_type", in.Type)
	setIfSet(patch, "status", in.Status)
	setIfSet(patch, "assignee", in.Assignee)
	setIfSet(patch, "parent_id", in.Parent)
	if in.Priority != nil {
		patch["priority"] = *in.Priority
	}
	if len(in.AddLabels) > 0 {
		patch["add_labels"] = in.AddLabels
	}
	if len(in.RemoveLabels) > 0 {
		patch["remove_labels"] = in.RemoveLabels
	}
	if len(in.SetLabels) > 0 {
		patch["labels"] = in.SetLabels
	}
	if len(patch) == 0 {
		// Nothing to change: the server refuses an empty patch, bd does not.
		return h.Client.Update(ctx, id, in)
	}
	return h.patchAs(ctx, "update", id, patch, in.Assignee != nil, in.Scope, func() (domain.Issue, error) { return h.Client.Update(ctx, id, in) })
}

// patchAs sends a PATCH. The API fences assignee changes away from another
// actor's live claim; `bd update --assignee` has no such fence, so an
// explicit assignee change passes force_assignee_transfer to match it.
func (h *HTTP) patchAs(ctx context.Context, op, id string, patch map[string]any, assigning bool, scope Scope, cli func() (domain.Issue, error)) (domain.Issue, error) {
	wctx, cancel := scoped(ctx, scope)
	defer cancel()
	body := map[string]any{"actor": h.actor, "patch": patch}
	if assigning {
		body["force_assignee_transfer"] = true
	}
	out, fallback, err := h.sendWrite(wctx, op, http.MethodPatch, issuePath(id, ""), body)
	if fallback {
		return cli()
	}
	if err != nil {
		return domain.Issue{}, err
	}
	return issueFrom(op, out)
}

func (h *HTTP) Assign(ctx context.Context, id string, actor string, scope Scope) (domain.Issue, error) {
	if err := validateID(id); err != nil {
		return domain.Issue{}, err
	}
	if !h.writes("issues.update") {
		return h.Client.Assign(ctx, id, actor, scope)
	}
	return h.patchAs(ctx, "assign", id, map[string]any{"assignee": actor}, true, scope,
		func() (domain.Issue, error) { return h.Client.Assign(ctx, id, actor, scope) })
}

func (h *HTTP) Claim(ctx context.Context, id string, in ClaimInput) (domain.Issue, error) {
	if err := validateID(id); err != nil {
		return domain.Issue{}, err
	}
	actor := firstNonEmptyString(in.Actor, h.actor)
	if !h.writes("issues.claim") || actor == "" {
		return h.Client.Claim(ctx, id, in)
	}
	return h.action(ctx, "claim", id, ":claim", map[string]any{"actor": actor}, in.Scope,
		func() (domain.Issue, error) { return h.Client.Claim(ctx, id, in) })
}

func (h *HTTP) Close(ctx context.Context, id string, in CloseInput) (domain.Issue, error) {
	if err := validateID(id); err != nil {
		return domain.Issue{}, err
	}
	if strings.TrimSpace(in.Reason) == "" {
		return domain.Issue{}, &CommandError{Kind: ErrValidation, Operation: "close", Cause: fmt.Errorf("a close reason is required")}
	}
	actor := firstNonEmptyString(in.Actor, h.actor)
	if !h.writes("issues.close") || actor == "" {
		return h.Client.Close(ctx, id, in)
	}
	return h.action(ctx, "close", id, ":close", map[string]any{"actor": actor, "reason": in.Reason}, in.Scope,
		func() (domain.Issue, error) { return h.Client.Close(ctx, id, in) })
}

func (h *HTTP) Reopen(ctx context.Context, id string, in ReopenInput) (domain.Issue, error) {
	if err := validateID(id); err != nil {
		return domain.Issue{}, err
	}
	if strings.TrimSpace(in.Reason) == "" {
		return domain.Issue{}, &CommandError{Kind: ErrValidation, Operation: "reopen", Cause: fmt.Errorf("a reopen reason is required")}
	}
	actor := firstNonEmptyString(in.Actor, h.actor)
	if !h.writes("issues.reopen") || !h.Supports("issues.update") || actor == "" {
		return h.Client.Reopen(ctx, id, in)
	}
	issue, err := h.action(ctx, "reopen", id, ":reopen", map[string]any{"actor": actor, "reason": in.Reason}, in.Scope,
		func() (domain.Issue, error) { return h.Client.Reopen(ctx, id, in) })
	if err != nil {
		return issue, err
	}
	// The CLI path records the reason as a note; keep the same trail. Like
	// there, a failed note does not undo the reopen.
	wctx, cancel := scoped(ctx, in.Scope)
	defer cancel()
	if out, _, err := h.sendWrite(wctx, "reopen note", http.MethodPatch, issuePath(id, ""),
		map[string]any{"actor": actor, "patch": map[string]any{"append_notes": "Reopened: " + in.Reason}}); err == nil {
		if noted, err := issueFrom("reopen note", out); err == nil {
			issue = noted
		}
	}
	return issue, nil
}

func (h *HTTP) action(ctx context.Context, op, id, action string, body map[string]any, scope Scope, cli func() (domain.Issue, error)) (domain.Issue, error) {
	wctx, cancel := scoped(ctx, scope)
	defer cancel()
	out, fallback, err := h.sendWrite(wctx, op, http.MethodPost, issuePath(id, action), body)
	if fallback {
		return cli()
	}
	if err != nil {
		return domain.Issue{}, err
	}
	return issueFrom(op, out)
}

func (h *HTTP) AddDependency(ctx context.Context, in DependencyInput) error {
	if err := validateID(in.Blocked); err != nil {
		return err
	}
	if err := validateID(in.Blocker); err != nil {
		return err
	}
	if in.Blocked == in.Blocker {
		return &CommandError{Kind: ErrValidation, Operation: "dep add", Cause: fmt.Errorf("an issue cannot depend on itself")}
	}
	if !h.writes("dependencies.add") {
		return h.Client.AddDependency(ctx, in)
	}
	kind := string(in.Type)
	if kind == "" {
		kind = string(domain.RelBlocks)
	}
	edge := map[string]any{"issue_id": in.Blocked, "depends_on_id": in.Blocker, "type": kind}
	wctx, cancel := scoped(ctx, in.Scope)
	defer cancel()
	_, fallback, err := h.sendWrite(wctx, "dep add", http.MethodPost, "/v0/beads/dependencies:add",
		map[string]any{"actor": h.actor, "edges": []any{edge}})
	if fallback {
		return h.Client.AddDependency(ctx, in)
	}
	return err
}

func (h *HTTP) RemoveDependency(ctx context.Context, in DependencyInput) error {
	if err := validateID(in.Blocked); err != nil {
		return err
	}
	if err := validateID(in.Blocker); err != nil {
		return err
	}
	if !h.writes("dependencies.remove") {
		return h.Client.RemoveDependency(ctx, in)
	}
	wctx, cancel := scoped(ctx, in.Scope)
	defer cancel()
	_, fallback, err := h.sendWrite(wctx, "dep remove", http.MethodPost, "/v0/beads/dependencies:remove",
		map[string]any{"actor": h.actor, "issue_id": in.Blocked, "depends_on_id": in.Blocker})
	if fallback {
		return h.Client.RemoveDependency(ctx, in)
	}
	return err
}

func setIfNonEmpty(m map[string]any, key, v string) {
	if v != "" {
		m[key] = v
	}
}

func setIfSet(m map[string]any, key string, v *string) {
	if v != nil {
		m[key] = *v
	}
}
