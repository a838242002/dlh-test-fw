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
