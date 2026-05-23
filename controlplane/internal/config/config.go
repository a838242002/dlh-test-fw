package config

import (
	"fmt"
	"os"
	"strings"
	"time"
)

// Config holds runtime configuration. All knobs come from environment
// variables — the binary takes no flags so it slots cleanly into a k8s
// Deployment's env block.
type Config struct {
	ListenAddr           string
	OIDCIssuerURL        string
	OIDCClientID         string
	OIDCRequiredAudience string
	OIDCGroupsClaim      string
	RolesConfigMapNS     string
	RolesConfigMapName   string
	K8sNamespace         string
	MinIOEndpoint        string
	MinIOBucket          string
	MinIOAccessKey       string
	MinIOSecretKey       string
	MinIOSecure          bool
	ShutdownGrace        time.Duration
	// AuthDisabled bypasses OIDC. ONLY for local dev — never set in prod.
	AuthDisabled bool
	// InternalToken is the shared secret for /internal/* endpoints.
	// Required when auth is enabled.
	InternalToken     string
	SessionSigningKey string
	CITrustedIssuers  []string
	CIAudience        string
}

// Load reads env vars and returns a populated Config or an error if any
// required field is missing.
func Load() (*Config, error) {
	c := &Config{
		ListenAddr:           getenv("DLH_LISTEN_ADDR", ":8080"),
		OIDCIssuerURL:        os.Getenv("DLH_OIDC_ISSUER_URL"),
		OIDCClientID:         os.Getenv("DLH_OIDC_CLIENT_ID"),
		OIDCRequiredAudience: os.Getenv("DLH_OIDC_AUDIENCE"),
		OIDCGroupsClaim:      getenv("DLH_OIDC_GROUPS_CLAIM", "groups"),
		RolesConfigMapNS:     getenv("DLH_ROLES_NAMESPACE", "dlh-test-fw"),
		RolesConfigMapName:   getenv("DLH_ROLES_CONFIGMAP", "dlh-roles"),
		K8sNamespace:         getenv("DLH_K8S_NAMESPACE", "dlh-test-fw"),
		MinIOEndpoint:        getenv("DLH_MINIO_ENDPOINT", "dlh-minio.dlh-test-fw.svc.cluster.local:9000"),
		MinIOBucket:          getenv("DLH_MINIO_BUCKET", "artifacts"),
		MinIOAccessKey:       os.Getenv("DLH_MINIO_ACCESS_KEY"),
		MinIOSecretKey:       os.Getenv("DLH_MINIO_SECRET_KEY"),
		MinIOSecure:          os.Getenv("DLH_MINIO_SECURE") == "true",
		ShutdownGrace:        15 * time.Second,
		AuthDisabled:         os.Getenv("DLH_AUTH_DISABLED") == "true",
		InternalToken:        os.Getenv("DLH_INTERNAL_TOKEN"),
		SessionSigningKey:    os.Getenv("DLH_SESSION_SIGNING_KEY"),
		CITrustedIssuers:     parseCSV(getenv("DLH_CI_TRUSTED_ISSUERS", "https://token.actions.githubusercontent.com")),
		CIAudience:           getenv("DLH_CI_AUDIENCE", "dlh-controlplane"),
	}
	if !c.AuthDisabled {
		if c.OIDCIssuerURL == "" {
			return nil, fmt.Errorf("DLH_OIDC_ISSUER_URL is required when auth is enabled")
		}
		if c.OIDCClientID == "" {
			return nil, fmt.Errorf("DLH_OIDC_CLIENT_ID is required when auth is enabled")
		}
	}
	if !c.AuthDisabled && c.InternalToken == "" {
		return nil, fmt.Errorf("DLH_INTERNAL_TOKEN is required when auth is enabled")
	}
	if !c.AuthDisabled && c.SessionSigningKey == "" {
		return nil, fmt.Errorf("DLH_SESSION_SIGNING_KEY is required when auth is enabled")
	}
	return c, nil
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func parseCSV(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
