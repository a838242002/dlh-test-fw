package config

import "testing"

func TestLoad_AuthDisabledBypassesIssuerCheck(t *testing.T) {
	t.Setenv("DLH_AUTH_DISABLED", "true")
	t.Setenv("DLH_OIDC_ISSUER_URL", "")
	t.Setenv("DLH_OIDC_CLIENT_ID", "")
	c, err := Load()
	if err != nil {
		t.Fatalf("Load(): %v", err)
	}
	if !c.AuthDisabled {
		t.Fatal("expected AuthDisabled=true")
	}
}

func TestLoad_AuthEnabledRequiresIssuer(t *testing.T) {
	t.Setenv("DLH_AUTH_DISABLED", "")
	t.Setenv("DLH_OIDC_ISSUER_URL", "")
	t.Setenv("DLH_OIDC_CLIENT_ID", "")
	if _, err := Load(); err == nil {
		t.Fatal("expected error when auth enabled and issuer missing")
	}
}

func TestLoad_DefaultsApplied(t *testing.T) {
	t.Setenv("DLH_AUTH_DISABLED", "true")
	c, err := Load()
	if err != nil {
		t.Fatalf("Load(): %v", err)
	}
	if c.ListenAddr != ":8080" {
		t.Errorf("ListenAddr default: got %q", c.ListenAddr)
	}
	if c.K8sNamespace != "dlh-test-fw" {
		t.Errorf("K8sNamespace default: got %q", c.K8sNamespace)
	}
}

func TestLoad_RequiresSessionSigningKey(t *testing.T) {
	t.Setenv("DLH_AUTH_DISABLED", "")
	t.Setenv("DLH_OIDC_ISSUER_URL", "https://example.com")
	t.Setenv("DLH_OIDC_CLIENT_ID", "client")
	t.Setenv("DLH_INTERNAL_TOKEN", "internal-secret")
	t.Setenv("DLH_SESSION_SIGNING_KEY", "")
	if _, err := Load(); err == nil {
		t.Fatal("expected error when session key missing")
	}
}

func TestLoad_DefaultCITrustedIssuers(t *testing.T) {
	t.Setenv("DLH_AUTH_DISABLED", "true")
	t.Setenv("DLH_CI_TRUSTED_ISSUERS", "")
	c, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(c.CITrustedIssuers) != 1 || c.CITrustedIssuers[0] != "https://token.actions.githubusercontent.com" {
		t.Errorf("default CITrustedIssuers: %v", c.CITrustedIssuers)
	}
}

func TestLoad_CSVTrustedIssuers(t *testing.T) {
	t.Setenv("DLH_AUTH_DISABLED", "true")
	t.Setenv("DLH_CI_TRUSTED_ISSUERS", "https://a.example.com, https://b.example.com")
	c, _ := Load()
	if len(c.CITrustedIssuers) != 2 {
		t.Errorf("expected 2 issuers, got %v", c.CITrustedIssuers)
	}
}

func TestLoad_DeepLinkURLs(t *testing.T) {
	t.Setenv("DLH_AUTH_DISABLED", "true")
	t.Setenv("DLH_ARGO_BASE_URL", "https://argo.example.com")
	t.Setenv("DLH_GRAFANA_BASE_URL", "https://grafana.example.com")
	c, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if c.ArgoBaseURL != "https://argo.example.com" {
		t.Errorf("ArgoBaseURL = %q", c.ArgoBaseURL)
	}
	if c.GrafanaBaseURL != "https://grafana.example.com" {
		t.Errorf("GrafanaBaseURL = %q", c.GrafanaBaseURL)
	}
}

func TestLoad_DeepLinkURLs_DefaultEmpty(t *testing.T) {
	t.Setenv("DLH_AUTH_DISABLED", "true")
	c, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if c.ArgoBaseURL != "" || c.GrafanaBaseURL != "" {
		t.Errorf("expected empty deep-link URLs, got argo=%q grafana=%q", c.ArgoBaseURL, c.GrafanaBaseURL)
	}
}
