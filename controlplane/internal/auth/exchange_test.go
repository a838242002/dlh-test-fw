package auth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

func TestParseIssuerOnly_HappyPath(t *testing.T) {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256"}`))
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"iss":"https://issuer.example.com","sub":"x"}`))
	tok := header + "." + payload + ".sig"
	iss, err := parseIssuerOnly(tok)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if iss != "https://issuer.example.com" {
		t.Errorf("iss: %q", iss)
	}
}

func TestParseIssuerOnly_MalformedTokens(t *testing.T) {
	cases := []string{"", "notajwt", "a.b", strings.Repeat("a", 100)}
	for _, c := range cases {
		if _, err := parseIssuerOnly(c); err == nil {
			t.Errorf("expected error for %q", c)
		}
	}
}

func TestParseIssuerOnly_MissingIss(t *testing.T) {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256"}`))
	payloadJSON, _ := json.Marshal(map[string]string{"sub": "x"})
	payload := base64.RawURLEncoding.EncodeToString(payloadJSON)
	tok := header + "." + payload + ".sig"
	if _, err := parseIssuerOnly(tok); err == nil {
		t.Fatal("expected error when iss missing")
	}
}

func TestExchanger_RejectsUnknownIssuer(t *testing.T) {
	e := &Exchanger{TrustedIssuers: []string{"https://allowed.example.com"}}
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256"}`))
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"iss":"https://forbidden.example.com","sub":"x"}`))
	tok := header + "." + payload + ".sig"
	_, err := e.Validate(context.Background(), tok)
	if err == nil {
		t.Fatal("expected error for untrusted issuer")
	}
	if !strings.Contains(err.Error(), "not in trusted allowlist") {
		t.Errorf("unexpected error: %v", err)
	}
}
