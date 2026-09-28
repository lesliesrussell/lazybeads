// lb-4gm.4
package beads

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// DefaultJournalPoll is how often the journal is polled when the push stream
// is unavailable.
const DefaultJournalPoll = 2 * time.Second

// The reconnection delay a server asks for is kept within these bounds, so a
// "retry: 0" cannot turn a closing stream into a hot loop.
const (
	minSSERetry = 100 * time.Millisecond
	maxSSERetry = time.Minute
)

// JournalRead pages /v0/beads/events from since, up to limit records
// (0 = until caught up with head).
func (h *HTTP) JournalRead(ctx context.Context, since int64, limit int, scope Scope) ([]JournalRecord, error) {
	if !h.Supports("events.list") {
		return h.Client.JournalRead(ctx, since, limit, scope)
	}
	rctx, cancel := scoped(ctx, scope)
	defer cancel()
	var out []JournalRecord
	for {
		want := 0
		if limit > 0 {
			want = limit - len(out)
		}
		records, head, err := h.eventsPage(rctx, since, want)
		if err != nil {
			if fallbackRead(err) && len(out) == 0 {
				return h.Client.JournalRead(ctx, since, limit, scope)
			}
			return out, err
		}
		out = append(out, records...)
		if len(records) == 0 || (limit > 0 && len(out) >= limit) {
			return out, nil
		}
		since = records[len(records)-1].Seq
		if since >= head {
			return out, nil
		}
	}
}

func (h *HTTP) eventsPage(ctx context.Context, since int64, limit int) ([]JournalRecord, int64, error) {
	v := url.Values{"since": {strconv.FormatInt(since, 10)}}
	if limit > 0 {
		v.Set("limit", strconv.Itoa(limit))
	}
	body, err := h.get(ctx, "events", "/v0/beads/events", v)
	if err != nil {
		return nil, 0, err
	}
	var page struct {
		Records []json.RawMessage `json:"records"`
		Head    int64             `json:"head"`
	}
	if err := json.Unmarshal(body, &page); err != nil {
		return nil, 0, decodeErr(err, body)
	}
	out := make([]JournalRecord, 0, len(page.Records))
	for _, raw := range page.Records {
		rec, err := DecodeJournalRecord(raw)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, rec)
	}
	return out, page.Head, nil
}

// JournalFollow streams records after since from events:watch, reconnecting
// from the last delivered seq when the stream drops. When the server refuses
// the stream for capacity it polls instead, and when the server goes away it
// continues through `bd events tail --follow`, which reads the same journal.
// It returns nil when ctx ends.
func (h *HTTP) JournalFollow(ctx context.Context, since int64, scope Scope, fn JournalFunc) error {
	if !h.Supports("events.watch") && !h.Supports("events.list") {
		return h.Client.JournalFollow(ctx, since, scope, fn)
	}
	last := since
	deliver := func(r JournalRecord) error {
		if r.Seq <= last {
			return nil // a reconnect may replay what was already delivered
		}
		if err := fn(r); err != nil {
			return err
		}
		last = r.Seq
		return nil
	}
	retry := 3 * time.Second
	for ctx.Err() == nil {
		var err error
		if h.Supports("events.watch") {
			err = h.watch(ctx, last, deliver, &retry)
		} else {
			err = &CommandError{Kind: ErrUnavailable, Operation: "events watch",
				Problem: &Problem{Code: "events_watch_saturated"}}
		}
		if ctx.Err() != nil {
			return nil
		}
		var ce *CommandError
		if errors.As(err, &ce) && ce.Problem != nil && ce.Problem.Code == "events_watch_saturated" {
			err = h.pollJournal(ctx, scope, &last, deliver)
			if ctx.Err() != nil {
				return nil
			}
			ce = nil
			errors.As(err, &ce)
		}
		switch {
		case err == nil:
			// The server closed the stream; reconnect after its retry delay.
		case ce != nil && ce.Kind == ErrUnavailable && ce.Problem == nil:
			// Nothing answered at all: the server is gone, but the journal
			// it served is still readable through bd.
			return h.Client.JournalFollow(ctx, last, scope, deliver)
		case ce != nil && ce.Kind == ErrUnavailable:
			// busy or db_unavailable: retryable, so stay on HTTP.
		default:
			return err
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(retry):
		}
	}
	return nil
}

// eventsPageAll reads everything after since over HTTP only: a poll that
// fails must surface, not quietly switch to another reader.
func (h *HTTP) eventsPageAll(ctx context.Context, scope Scope, since int64) ([]JournalRecord, error) {
	rctx, cancel := scoped(ctx, scope)
	defer cancel()
	var out []JournalRecord
	for {
		records, head, err := h.eventsPage(rctx, since, 0)
		if err != nil {
			return out, err
		}
		out = append(out, records...)
		if len(records) == 0 {
			return out, nil
		}
		since = records[len(records)-1].Seq
		if since >= head {
			return out, nil
		}
	}
}

// pollJournal reads pages until the stream might be worth retrying: it
// returns nil after a while so JournalFollow can try events:watch again.
func (h *HTTP) pollJournal(ctx context.Context, scope Scope, last *int64, deliver JournalFunc) error {
	interval := h.PollInterval
	if interval <= 0 {
		interval = DefaultJournalPoll
	}
	for i := 0; i < 30; i++ {
		records, err := h.eventsPageAll(ctx, scope, *last)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		for _, r := range records {
			if err := deliver(r); err != nil {
				return err
			}
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(interval):
		}
	}
	return nil
}

// watch holds one events:watch connection open, delivering records until the
// stream ends. A nil return means the server closed it normally.
func (h *HTTP) watch(ctx context.Context, since int64, deliver JournalFunc, retry *time.Duration) error {
	u := *h.base
	u.Path = strings.TrimRight(u.Path, "/") + "/v0/beads/events:watch"
	u.RawQuery = url.Values{"since": {strconv.FormatInt(since, 10)}}.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return &CommandError{Kind: ErrValidation, Operation: "events watch", Cause: err}
	}
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Last-Event-ID", strconv.FormatInt(since, 10))
	if h.token != "" {
		req.Header.Set("Authorization", "Bearer "+h.token)
	}
	if h.projectID != "" {
		req.Header.Set("Bd-Project-Id", h.projectID)
	}
	resp, err := h.hc.Do(req)
	if err != nil {
		return transportError(ctx, "events watch", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, maxHTTPBody))
		return problemError("events watch", resp.StatusCode, resp.Header.Get("Content-Type"), body)
	}
	return readSSE(resp.Body, func(ev sseEvent) error {
		switch ev.event {
		case "", "message":
			rec, err := DecodeJournalRecord([]byte(ev.data))
			if err != nil {
				return err
			}
			return deliver(rec)
		case "truncated":
			return problemError("events watch", http.StatusGone, "application/problem+json", []byte(ev.data))
		}
		return nil // an event kind this build does not know
	}, retry)
}

type sseEvent struct {
	id, event, data string
}

// readSSE parses a text/event-stream body, calling fn per dispatched event.
// Comments (heartbeats) are skipped; retry: updates *retry.
func readSSE(r io.Reader, fn func(sseEvent) error, retry *time.Duration) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64<<10), MaxStreamLine)
	var ev sseEvent
	var data []string
	for sc.Scan() {
		line := sc.Text()
		if line == "" {
			if len(data) > 0 {
				ev.data = strings.Join(data, "\n")
				if err := fn(ev); err != nil {
					return err
				}
			}
			ev, data = sseEvent{}, nil
			continue
		}
		if strings.HasPrefix(line, ":") {
			continue
		}
		field, value, _ := strings.Cut(line, ":")
		value = strings.TrimPrefix(value, " ")
		switch field {
		case "id":
			ev.id = value
		case "event":
			ev.event = value
		case "data":
			data = append(data, value)
		case "retry":
			if ms, err := strconv.Atoi(value); err == nil && retry != nil {
				*retry = min(max(time.Duration(ms)*time.Millisecond, minSSERetry), maxSSERetry)
			}
		}
	}
	// A dropped stream is the normal case: the caller reconnects from the
	// last delivered seq, and only a failed reconnect means the server is gone.
	// A record too large to read would come back on every reconnect, so that
	// one is an error.
	if errors.Is(sc.Err(), bufio.ErrTooLong) {
		return &CommandError{Kind: ErrDecode, Operation: "events watch", Cause: sc.Err()}
	}
	return nil
}
