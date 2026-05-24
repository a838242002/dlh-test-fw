package api

// TestNewRouter_SSERouteServesRealHandler verifies that GET /api/runs/{id}/events
// is served by the REAL SSEHandler (which sets Content-Type: text/event-stream and
// streams data), NOT by the generated strict-handler stub
// (gen.StreamRunEvents200TexteventStreamResponse with nil Body, which panics in
// io.Copy).
//
// Root cause being tested: chi v5 uses last-registration-wins for identical route
// patterns.  NewRouter previously registered the explicit SSE route BEFORE
// gen.HandlerFromMux, so the generated stub (registered later) shadowed it.
// The stub sets Content-Type+200 then panics in io.Copy(w, nil) — the Recoverer
// cannot change the already-written status, so the client sees 200+text/event-stream
// then an immediate EOF (server closes the connection after the panic).
//
// The REAL SSEHandler writes headers, flushes, then blocks streaming keepalives —
// so the connection stays open and data arrives.
//
// This test:
//   1. Builds a full router via NewRouter (AuthDisabled=true, no auth middleware).
//   2. Issues a GET to /api/runs/test-id/events via httptest.Server.
//   3. Reads from the response body with a short deadline.
//   4. Asserts: (a) Content-Type is text/event-stream; (b) the connection was NOT
//      immediately closed (EOF); specifically that we can read at least one byte /
//      that the body read blocks rather than returning EOF right away.
//      Immediate EOF = stub panicked and server closed connection.
//      Body stays open = real handler is streaming.

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5/middleware"
)

// TestNewRouter_SSERouteServesRealHandler is the regression test for the
// panic caused by the generated stub shadowing the explicit SSE route.
func TestNewRouter_SSERouteServesRealHandler(t *testing.T) {
	wfs := &fakeWorkflows{} // no workflows stored — SSEHandler skips snapshot, streams keepalives
	deps := &Deps{
		Workflows: wfs,
		AuthInfo:  AuthInfoConfig{AuthDisabled: true},
	}

	// Wrap with Recoverer so a panic turns into a 500 (rather than crashing the
	// test binary) and the connection is closed.  The real handler never panics.
	rawHandler := NewRouter(deps, nil, "")
	handler := middleware.Recoverer(rawHandler)

	srv := httptest.NewServer(handler)
	defer srv.Close()

	// Open the SSE connection.  We don't pass a client Timeout here so the
	// connection attempt itself doesn't time out; we control reading below.
	resp, err := http.Get(srv.URL + "/api/runs/test-id/events") //nolint:noctx
	if err != nil {
		t.Fatalf("GET /api/runs/test-id/events: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d; want 200", resp.StatusCode)
	}

	ct := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "text/event-stream") {
		t.Fatalf("Content-Type = %q; want text/event-stream", ct)
	}

	// Read from the body with a short deadline.
	// - REAL handler: blocks waiting for events / keepalive tick (15s tick in
	//   production, but the connection is still open) → read times out with
	//   no data yet, but crucially the connection is NOT immediately closed.
	//   We use a 300ms read window; if the stub route is served instead:
	// - STUB path: server panics in io.Copy, Recoverer closes connection → we
	//   get io.EOF immediately (no data, connection gone).
	//
	// Discriminator: attempt a read with a 300ms deadline.
	// - EOF before deadline = stub (bad, connection was closed after panic).
	// - Timeout / partial data = real handler (good, connection is live).
	type readResult struct {
		n   int
		err error
	}
	done := make(chan readResult, 1)
	buf := make([]byte, 256)
	go func() {
		n, err := resp.Body.Read(buf)
		done <- readResult{n, err}
	}()

	select {
	case res := <-done:
		// Got a result within 300ms — could be data (snapshot if workflow existed)
		// or EOF (stub panic closed connection).
		if res.err == io.EOF && res.n == 0 {
			t.Fatalf("body returned immediate EOF — stub panicked and closed connection; real SSE handler not reached")
		}
		// Any non-EOF result (or EOF with data) means the real handler ran.
		t.Logf("read %d bytes (err=%v) — real handler reached", res.n, res.err)
	case <-time.After(300 * time.Millisecond):
		// Read blocked — real SSEHandler is alive and waiting for events.  Good.
		t.Logf("read blocked for 300ms — real SSEHandler is streaming (connection live)")
	}
}
