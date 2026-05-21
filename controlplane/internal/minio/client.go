package minio

import (
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// New returns a configured MinIO client. The endpoint should be host:port
// without scheme; pass secure=true for HTTPS.
func New(endpoint, accessKey, secretKey string, secure bool) (*minio.Client, error) {
	return minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure: secure,
	})
}
