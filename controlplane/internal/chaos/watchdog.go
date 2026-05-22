package chaos

import (
	"context"
	"log/slog"
	"time"
)

// Watchdog periodically scans chaos CRs and force-deletes ones whose
// associated Run has reached a terminal phase (or whose Workflow CR has
// already been TTL-collected). Prevents chaos from outliving its workflow
// when cleanup steps fail or the workflow is forcibly terminated.
type Watchdog struct {
	Chaos        Client
	RunsTerminal RunsTerminalChecker
	Interval     time.Duration
}

// RunsTerminalChecker reports whether a given run id is in a terminal phase
// (or absent — both cases mean "no longer running; chaos may be reaped").
type RunsTerminalChecker interface {
	IsTerminal(runID string) bool
}

// RunsTerminalCheckerFunc is a function-typed adapter for RunsTerminalChecker.
type RunsTerminalCheckerFunc func(runID string) bool

func (f RunsTerminalCheckerFunc) IsTerminal(runID string) bool { return f(runID) }

// Run blocks until ctx is cancelled. Default Interval is 30s.
func (w *Watchdog) Run(ctx context.Context) {
	if w.Interval == 0 {
		w.Interval = 30 * time.Second
	}
	t := time.NewTicker(w.Interval)
	defer t.Stop()
	// Tick once immediately so tests don't have to wait a full interval.
	w.tick(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			w.tick(ctx)
		}
	}
}

func (w *Watchdog) tick(ctx context.Context) {
	all, err := w.Chaos.ListManaged(ctx)
	if err != nil {
		slog.Warn("watchdog list", "err", err)
		return
	}
	for runID, refs := range all {
		if !w.RunsTerminal.IsTerminal(runID) {
			continue
		}
		for _, ref := range refs {
			if err := w.Chaos.Delete(ctx, ref); err != nil {
				slog.Warn("watchdog delete", "runID", runID, "ref", ref.Name, "err", err)
			} else {
				slog.Info("watchdog cleaned chaos", "runID", runID, "ref", ref.Name)
			}
		}
	}
}
