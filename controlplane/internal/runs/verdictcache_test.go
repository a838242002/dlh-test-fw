package runs

import (
	"context"
	"testing"

	mio "github.com/dlh/dlh-test-fw/controlplane/internal/minio"
)

type fakeReader struct {
	calls int
	rep   map[string]any
	err   error
}

func (f *fakeReader) Read(_ context.Context, _ string) (map[string]any, error) {
	f.calls++
	return f.rep, f.err
}

func TestVerdictCache_PassCachedOnce(t *testing.T) {
	fr := &fakeReader{rep: map[string]any{"overall": true}}
	c := NewVerdictCache(fr)
	s, ok := c.Score(context.Background(), "wf-1", true)
	if !ok || s != 1.0 {
		t.Fatalf("want (1.0,true), got (%v,%v)", s, ok)
	}
	_, _ = c.Score(context.Background(), "wf-1", true)
	if fr.calls != 1 {
		t.Fatalf("expected 1 read (cached), got %d", fr.calls)
	}
}

func TestVerdictCache_FailMapsToZero(t *testing.T) {
	fr := &fakeReader{rep: map[string]any{"overall": false}}
	c := NewVerdictCache(fr)
	s, ok := c.Score(context.Background(), "wf-2", true)
	if !ok || s != 0.0 {
		t.Fatalf("want (0.0,true), got (%v,%v)", s, ok)
	}
}

func TestVerdictCache_NotFoundNotCached(t *testing.T) {
	fr := &fakeReader{err: mio.ErrReportNotFound}
	c := NewVerdictCache(fr)
	if _, ok := c.Score(context.Background(), "wf-3", true); ok {
		t.Fatal("want ok=false for missing report")
	}
	_, _ = c.Score(context.Background(), "wf-3", true)
	if fr.calls != 2 {
		t.Fatalf("missing report must NOT cache; want 2 reads, got %d", fr.calls)
	}
}

func TestVerdictCache_NonTerminalSkipsRead(t *testing.T) {
	fr := &fakeReader{rep: map[string]any{"overall": true}}
	c := NewVerdictCache(fr)
	if _, ok := c.Score(context.Background(), "wf-4", false); ok {
		t.Fatal("non-terminal runs must not be read")
	}
	if fr.calls != 0 {
		t.Fatalf("want 0 reads for non-terminal, got %d", fr.calls)
	}
}
