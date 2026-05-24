package runs

import (
	"context"
	"sync"
)

// reportReader is the subset of minio.ReportReader the cache needs.
type reportReader interface {
	Read(ctx context.Context, workflowName string) (map[string]any, error)
}

// VerdictCache resolves a run's pass/fail score (1.0/0.0) from its immutable
// MinIO report, caching finished runs forever (the report never changes).
type VerdictCache struct {
	reader reportReader
	mu     sync.RWMutex
	scores map[string]float64
}

func NewVerdictCache(r reportReader) *VerdictCache {
	return &VerdictCache{reader: r, scores: map[string]float64{}}
}

// Score returns (score, true) where score is 1.0 (pass) or 0.0 (fail).
// ok=false when the run isn't terminal, has no report, or errors (render "—").
// Only terminal runs are read/cached.
func (c *VerdictCache) Score(ctx context.Context, workflow string, terminal bool) (float64, bool) {
	if !terminal {
		return 0, false
	}
	c.mu.RLock()
	if s, ok := c.scores[workflow]; ok {
		c.mu.RUnlock()
		return s, true
	}
	c.mu.RUnlock()

	rep, err := c.reader.Read(ctx, workflow)
	if err != nil || rep == nil {
		return 0, false
	}
	overall, ok := rep["overall"].(bool)
	if !ok {
		return 0, false
	}
	score := 0.0
	if overall {
		score = 1.0
	}
	c.mu.Lock()
	c.scores[workflow] = score
	c.mu.Unlock()
	return score, true
}
