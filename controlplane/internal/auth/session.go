package auth

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// SessionIssuer mints and verifies short-lived controlplane session
// tokens. Issued in exchange for a CI-OIDC token (POST /api/oidc/exchange)
// or for a dlh login device-code flow. Backed by an HMAC secret held in
// the dlh-session-signing-key Secret.
type SessionIssuer struct {
	Key      []byte
	Lifetime time.Duration // default 1 hour
	Issuer   string        // exposed in iss claim — defaults to "dlh-controlplane"
}

// SessionClaims is the JWT claims set we issue.
type SessionClaims struct {
	Subject string   `json:"sub"`
	Email   string   `json:"email,omitempty"`
	Groups  []string `json:"groups,omitempty"`
	jwt.RegisteredClaims
}

// Issue produces a signed JWT for the given identity.
func (s *SessionIssuer) Issue(id *Identity) (string, error) {
	if len(s.Key) == 0 {
		return "", errors.New("session issuer: missing signing key")
	}
	if id == nil {
		return "", errors.New("session issuer: nil identity")
	}
	lifetime := s.Lifetime
	if lifetime == 0 {
		lifetime = time.Hour
	}
	issuer := s.Issuer
	if issuer == "" {
		issuer = "dlh-controlplane"
	}
	now := time.Now()
	claims := SessionClaims{
		Subject: id.Subject,
		Email:   id.Email,
		Groups:  append([]string(nil), id.Groups...),
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    issuer,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(lifetime)),
			NotBefore: jwt.NewNumericDate(now),
		},
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return tok.SignedString(s.Key)
}

// ErrInvalidSession is returned when the token is malformed, expired, or
// signed with a different key.
var ErrInvalidSession = errors.New("invalid session token")

// Verify parses + validates the token and returns the embedded identity.
func (s *SessionIssuer) Verify(raw string) (*Identity, error) {
	if len(s.Key) == 0 {
		return nil, errors.New("session issuer: missing signing key")
	}
	parser := jwt.NewParser(
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
	)
	claims := &SessionClaims{}
	tok, err := parser.ParseWithClaims(raw, claims, func(t *jwt.Token) (interface{}, error) {
		return s.Key, nil
	})
	if err != nil || !tok.Valid {
		return nil, fmt.Errorf("%w: %v", ErrInvalidSession, err)
	}
	return &Identity{
		Subject: claims.Subject,
		Email:   claims.Email,
		Groups:  claims.Groups,
	}, nil
}
