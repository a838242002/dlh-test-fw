package minio

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/minio/minio-go/v7"
)

// ReportReader fetches verdict-job's report.json from the artifact bucket.
type ReportReader struct {
	client *minio.Client
	bucket string
}

// NewReportReader binds a client to a bucket.
func NewReportReader(client *minio.Client, bucket string) *ReportReader {
	return &ReportReader{client: client, bucket: bucket}
}

// ErrReportNotFound signals "no report yet", distinct from transport errors.
var ErrReportNotFound = errors.New("report not found")

// Read returns the parsed report.json for a workflow name. If the object
// is absent, returns (nil, ErrReportNotFound). The object key follows
// the existing artifact-repository convention: under the workflow's
// name prefix, a verdict step writes verdict/report.json. We walk
// objects under <workflowName>/ and return the first matching suffix.
func (r *ReportReader) Read(ctx context.Context, workflowName string) (map[string]any, error) {
	prefix := fmt.Sprintf("%s/", workflowName)
	for objInfo := range r.client.ListObjects(ctx, r.bucket, minio.ListObjectsOptions{
		Prefix:    prefix,
		Recursive: true,
	}) {
		if objInfo.Err != nil {
			return nil, objInfo.Err
		}
		if hasReportSuffix(objInfo.Key) {
			obj, err := r.client.GetObject(ctx, r.bucket, objInfo.Key, minio.GetObjectOptions{})
			if err != nil {
				return nil, err
			}
			defer obj.Close()
			raw, err := io.ReadAll(obj)
			if err != nil {
				return nil, err
			}
			out := map[string]any{}
			if err := json.Unmarshal(raw, &out); err != nil {
				return nil, fmt.Errorf("decode report.json: %w", err)
			}
			return out, nil
		}
	}
	return nil, ErrReportNotFound
}

func hasReportSuffix(key string) bool {
	const suffix = "/verdict/report.json"
	const bare = "verdict/report.json"
	if key == bare {
		return true
	}
	return len(key) >= len(suffix) && key[len(key)-len(suffix):] == suffix
}
