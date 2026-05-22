package runs

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
)

// Manifest is the controlplane's authoritative record for a Run.
// Written to MinIO at submit time and on terminal Workflow phase.
type Manifest struct {
	RunID        string            `json:"runId"`
	Scenario     string            `json:"scenario"`
	Target       string            `json:"target,omitempty"`
	WorkflowName string            `json:"workflowName"`
	Parameters   map[string]string `json:"parameters,omitempty"`
	CreatedBy    string            `json:"createdBy,omitempty"`
	Status       string            `json:"status"` // Submitted/Running/Succeeded/Failed/Error/Unknown
	StartedAt    time.Time         `json:"startedAt"`
	FinishedAt   *time.Time        `json:"finishedAt,omitempty"`
	Score        *float64          `json:"score,omitempty"`
}

// ManifestWriter writes manifests + index objects to MinIO.
// Client may be nil (test mode); Write and Read become no-ops.
type ManifestWriter struct {
	Client *minio.Client
	Bucket string
}

// Write puts the manifest at runs/by-id/{runID}/manifest.json AND writes
// pointer copies under runs/index/by-scenario/{scenario}/{YYYY-MM-DD}/{runID}.json
// and (if Target is non-empty) runs/index/by-target/{target}/{YYYY-MM-DD}/{runID}.json.
func (w *ManifestWriter) Write(ctx context.Context, m Manifest) error {
	if w == nil || w.Client == nil {
		return nil // test-mode no-op
	}
	body, err := json.Marshal(m)
	if err != nil {
		return fmt.Errorf("marshal manifest: %w", err)
	}
	primary := fmt.Sprintf("runs/by-id/%s/manifest.json", m.RunID)
	if err := w.putJSON(ctx, primary, body); err != nil {
		return fmt.Errorf("put primary: %w", err)
	}
	day := m.StartedAt.UTC().Format("2006-01-02")
	idx := fmt.Sprintf("runs/index/by-scenario/%s/%s/%s.json", sanitize(m.Scenario), day, m.RunID)
	if err := w.putJSON(ctx, idx, body); err != nil {
		return fmt.Errorf("put index: %w", err)
	}
	if m.Target != "" {
		idxT := fmt.Sprintf("runs/index/by-target/%s/%s/%s.json", sanitize(m.Target), day, m.RunID)
		if err := w.putJSON(ctx, idxT, body); err != nil {
			return fmt.Errorf("put by-target index: %w", err)
		}
	}
	return nil
}

// Read fetches a manifest by run id.
func (w *ManifestWriter) Read(ctx context.Context, runID string) (*Manifest, error) {
	if w == nil || w.Client == nil {
		return nil, fmt.Errorf("manifest writer not configured")
	}
	key := fmt.Sprintf("runs/by-id/%s/manifest.json", runID)
	obj, err := w.Client.GetObject(ctx, w.Bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, err
	}
	defer obj.Close()
	var m Manifest
	if err := json.NewDecoder(obj).Decode(&m); err != nil {
		return nil, fmt.Errorf("decode manifest: %w", err)
	}
	return &m, nil
}

func (w *ManifestWriter) putJSON(ctx context.Context, key string, body []byte) error {
	_, err := w.Client.PutObject(ctx, w.Bucket, key, bytes.NewReader(body), int64(len(body)),
		minio.PutObjectOptions{ContentType: "application/json"})
	return err
}

// sanitize replaces characters that aren't safe in S3-prefix paths.
// Scenario IDs are k8s names so already safe; defensive trim anyway.
func sanitize(s string) string {
	return strings.ReplaceAll(s, "/", "_")
}
