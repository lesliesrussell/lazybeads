// lb-4gm.4
package beads

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/lesliesrussell/lazybeads/internal/domain"
)

// Journal operations, as bd names them in `bd events tail` records.
const (
	JournalOpCreate    = "create"
	JournalOpUpdate    = "update"
	JournalOpClose     = "close"
	JournalOpDelete    = "delete"
	JournalOpDepAdd    = "dep_add"
	JournalOpDepRemove = "dep_remove"
	JournalOpComment   = "comment"
)

// JournalRecord is one entry of the Beads events journal: an ordered,
// committed mutation carrying the issue's full state after it.
type JournalRecord struct {
	// Seq is gapless and strictly increasing within one clone's journal.
	Seq     int64
	TS      time.Time
	Op      string
	IssueID string
	// Actor is empty for derived records, such as the blocked-state updates
	// a close or claim emits for other issues.
	Actor string
	// Issue is the state after the mutation; nil on delete.
	Issue   *domain.Issue
	Dep     *JournalDep
	Comment *JournalComment
	Raw     json.RawMessage
}

// JournalDep is the edge a dep_add or dep_remove record changed.
type JournalDep struct {
	Kind     string `json:"kind"`
	Target   string `json:"target"`
	Metadata string `json:"metadata,omitempty"`
}

// JournalComment is the comment a comment record added.
type JournalComment struct {
	// ID is a UUID in bd 1.3.0; kept raw so a numeric id also decodes.
	ID        json.RawMessage `json:"id"`
	Author    string          `json:"author"`
	Text      string          `json:"text"`
	CreatedAt string          `json:"created_at"`
	Source    string          `json:"source,omitempty"`
}

// JournalFunc receives records in seq order. Returning an error stops the read.
type JournalFunc func(JournalRecord) error

// DecodeJournalRecord parses one journal record, as `bd events tail` prints it
// and as bd serve returns it.
func DecodeJournalRecord(data []byte) (JournalRecord, error) {
	var raw struct {
		Seq     *int64          `json:"seq"`
		TS      string          `json:"ts"`
		Op      string          `json:"op"`
		IssueID string          `json:"issue_id"`
		Actor   string          `json:"actor"`
		Issue   json.RawMessage `json:"issue"`
		Dep     *JournalDep     `json:"dep"`
		Comment *JournalComment `json:"comment"`
	}
	data = trimJSON(data)
	if err := json.Unmarshal(data, &raw); err != nil {
		return JournalRecord{}, decodeErr(err, data)
	}
	if raw.Seq == nil || raw.Op == "" {
		return JournalRecord{}, decodeErr(fmt.Errorf("not an events journal record"), data)
	}
	rec := JournalRecord{
		Seq: *raw.Seq, Op: raw.Op, IssueID: raw.IssueID, Actor: raw.Actor,
		Dep: raw.Dep, Comment: raw.Comment, Raw: append(json.RawMessage(nil), data...),
	}
	if ts := parseTime(raw.TS); ts != nil {
		rec.TS = *ts
	}
	if len(raw.Issue) > 0 && string(raw.Issue) != "null" {
		issues, err := decodeIssues(raw.Issue)
		if err != nil {
			return JournalRecord{}, err
		}
		if len(issues) == 1 {
			rec.Issue = &issues[0]
		}
	}
	return rec, nil
}

// Event renders a record for the activity feed.
func (r JournalRecord) Event() domain.Event {
	id := r.IssueID
	ev := domain.Event{
		ID:        strconv.FormatInt(r.Seq, 10),
		IssueID:   &id,
		Kind:      domain.EventUnknown,
		Timestamp: r.TS,
		Raw:       r.Raw,
	}
	if r.Actor != "" {
		ev.Actor = actorFrom(r.Actor)
	}
	title := id
	if r.Issue != nil && r.Issue.Title != "" {
		title = id + " " + r.Issue.Title
	}
	switch r.Op {
	case JournalOpCreate:
		ev.Kind, ev.Summary = domain.EventCreated, "created "+title
	case JournalOpClose:
		ev.Kind, ev.Summary = domain.EventClosed, "closed "+title
	case JournalOpDelete:
		ev.Kind, ev.Summary = domain.EventUpdated, "deleted "+id
	case JournalOpDepAdd, JournalOpDepRemove:
		ev.Kind = domain.EventDependencyEdit
		verb := "added"
		if r.Op == JournalOpDepRemove {
			verb = "removed"
		}
		if r.Dep != nil {
			ev.Summary = fmt.Sprintf("%s %s dependency %s -> %s", verb, r.Dep.Kind, id, r.Dep.Target)
		} else {
			ev.Summary = verb + " a dependency on " + id
		}
	case JournalOpComment:
		ev.Kind, ev.Summary = domain.EventMessage, "commented on "+title
		if r.Comment != nil && r.Comment.Text != "" {
			ev.Summary += ": " + firstLine(r.Comment.Text)
		}
	case JournalOpUpdate:
		ev.Kind, ev.Summary = domain.EventUpdated, "updated "+title
		if r.Actor == "" {
			ev.Summary = "blocked state changed for " + title
		}
	default:
		ev.Summary = r.Op + " " + title
	}
	return ev
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i] + "…"
	}
	return s
}

// journalDisabledNote is how `bd events tail` says the journal is off; it
// still exits 0.
const journalDisabledNote = "events journal is disabled"

// journalProblem recognises the error object `bd events tail --json` prints
// on stdout when it fails, such as a truncated checkpoint.
func journalProblem(line []byte) (*CommandError, bool) {
	var p struct {
		Seq   *int64 `json:"seq"`
		Code  string `json:"code"`
		Error string `json:"error"`
		Since int64  `json:"since"`
		Floor int64  `json:"floor"`
		Head  int64  `json:"head"`
	}
	if json.Unmarshal(trimJSON(line), &p) != nil || p.Seq != nil || p.Code == "" {
		return nil, false
	}
	ce := &CommandError{Operation: "events tail", Stderr: p.Error,
		Problem: &Problem{Code: p.Code, Detail: p.Error, Since: p.Since, Floor: p.Floor, Head: p.Head}}
	switch p.Code {
	case "events_journal_truncated":
		ce.Kind = ErrJournalTruncated
	case "events_journal_disabled":
		ce.Kind = ErrJournalDisabled
	default:
		ce.Kind = classifyStderr(p.Error, 1, nil)
	}
	return ce, true
}

// JournalRead returns the records after since, up to limit (0 = all).
func (c *CLI) JournalRead(ctx context.Context, since int64, limit int, scope Scope) ([]JournalRecord, error) {
	var out []JournalRecord
	timeout := scope.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	err := c.tail(ctx, since, limit, false, timeout, scope, func(r JournalRecord) error {
		out = append(out, r)
		return nil
	})
	return out, err
}

// JournalFollow delivers the records after since, then each new one as it is
// committed, until ctx ends (which returns nil) or the journal fails.
func (c *CLI) JournalFollow(ctx context.Context, since int64, scope Scope, fn JournalFunc) error {
	err := c.tail(ctx, since, 0, true, 0, scope, fn)
	if ctx.Err() != nil {
		return nil
	}
	if err == nil {
		// --follow only ends when stopped; ending by itself is a failure the
		// caller must be able to tell from a normal stop.
		return &CommandError{Kind: ErrUnavailable, Operation: "events tail",
			Cause: fmt.Errorf("bd events tail --follow exited")}
	}
	return err
}

func (c *CLI) tail(ctx context.Context, since int64, limit int, follow bool, timeout time.Duration, scope Scope, fn JournalFunc) error {
	args := []string{"events", "tail", "--since", strconv.FormatInt(since, 10), "--json"}
	if limit > 0 {
		args = append(args, "--limit", strconv.Itoa(limit))
	}
	if follow {
		args = append(args, "--follow")
	}
	// Records arrive one compact object per line; anything else is bd's
	// (indented, multi-line) error object, collected and read at the end.
	var rest bytes.Buffer
	err := c.runner.Stream(ctx, "events tail", scope, StreamOptions{
		Timeout: timeout,
		Stdout: func(line []byte) error {
			trimmed := bytes.TrimSpace(line)
			if len(trimmed) == 0 {
				return nil
			}
			if bytes.HasPrefix(trimmed, []byte(`{"seq"`)) {
				rec, err := DecodeJournalRecord(trimmed)
				if err != nil {
					return err
				}
				return fn(rec)
			}
			// Stray notices must not hold records back or grow without bound.
			if rest.Len() < MaxStderrCapture {
				rest.Write(line)
				rest.WriteByte('\n')
			}
			return nil
		},
		Stderr: func(line string) error {
			// A disabled journal records nothing new, so following it would
			// wait forever; report it instead.
			if strings.Contains(line, journalDisabledNote) {
				return &CommandError{Kind: ErrJournalDisabled, Operation: "events tail", Stderr: line}
			}
			return nil
		},
	}, args...)
	// Anything else bd printed is a notice; it only matters if bd failed.
	if rest.Len() > 0 {
		if problem, ok := journalProblem(rest.Bytes()); ok {
			return problem
		}
	}
	return err
}

// headReader is implemented by clients that can report the journal head
// without reading the journal.
type headReader interface {
	journalHead(ctx context.Context, scope Scope) (int64, error)
}

// JournalHead returns the highest seq the journal has assigned. Over bd serve
// it is one request; over the CLI, which has no head query, a truncated read
// reports it, and otherwise the retained journal is read to its end.
func JournalHead(ctx context.Context, c Client, scope Scope) (int64, error) {
	if hr, ok := c.(headReader); ok {
		if head, err := hr.journalHead(ctx, scope); err == nil {
			return head, nil
		} else if !fallbackRead(err) {
			return 0, err
		}
	}
	recs, err := c.JournalRead(ctx, 0, 0, scope)
	if err != nil {
		if ce, ok := AsCommandError(err); ok && ce.Kind == ErrJournalTruncated && ce.Problem != nil {
			return ce.Problem.Head, nil
		}
		return 0, err
	}
	if len(recs) == 0 {
		return 0, nil
	}
	return recs[len(recs)-1].Seq, nil
}

// JournalTail returns up to n of the most recent journal records, oldest
// first. knownHead, when positive, saves looking the head up (a live mirror
// knows it). Records pruned by retention are skipped, not an error.
// lb-4gm.7
func JournalTail(ctx context.Context, c Client, scope Scope, n int, knownHead int64) ([]JournalRecord, error) {
	head := knownHead
	if head <= 0 {
		if hr, ok := c.(headReader); ok {
			if h, err := hr.journalHead(ctx, scope); err == nil {
				head = h
			}
		}
	}
	since := int64(0)
	if head > 0 && head > int64(n) {
		since = head - int64(n)
	}
	recs, err := c.JournalRead(ctx, since, 0, scope)
	if ce, ok := AsCommandError(err); ok && ce.Kind == ErrJournalTruncated && ce.Problem != nil && ce.Problem.Floor > 0 {
		recs, err = c.JournalRead(ctx, ce.Problem.Floor-1, 0, scope)
	}
	if err != nil {
		return nil, err
	}
	if len(recs) > n {
		recs = recs[len(recs)-n:]
	}
	return recs, nil
}
