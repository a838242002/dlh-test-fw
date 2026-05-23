package auth

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
)

// parseIssuerOnly extracts the iss claim from a JWT without verifying
// the signature. Used by Exchanger to choose the right OIDC provider
// before paying for JWKS fetch + verification.
func parseIssuerOnly(raw string) (string, error) {
	parts := strings.Split(raw, ".")
	if len(parts) != 3 {
		return "", errors.New("not a JWT (wrong segment count)")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", err
	}
	var c struct {
		Iss string `json:"iss"`
	}
	if err := json.Unmarshal(payload, &c); err != nil {
		return "", err
	}
	if c.Iss == "" {
		return "", errors.New("iss claim missing")
	}
	return c.Iss, nil
}
