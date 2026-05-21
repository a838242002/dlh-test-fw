package auth

import (
	"context"
	"errors"
	"strings"
)

// FakeVerifier accepts tokens of the form `fake:<sub>:<email>:<group1,group2>`.
// Only used in tests and when DLH_AUTH_DISABLED=true.
type FakeVerifier struct{}

func (FakeVerifier) Verify(_ context.Context, rawToken string) (*Identity, error) {
	if !strings.HasPrefix(rawToken, "fake:") {
		return nil, errors.New("not a fake token")
	}
	parts := strings.SplitN(strings.TrimPrefix(rawToken, "fake:"), ":", 3)
	if len(parts) < 2 {
		return nil, errors.New("malformed fake token")
	}
	id := &Identity{Subject: parts[0], Email: parts[1]}
	if len(parts) == 3 && parts[2] != "" {
		id.Groups = strings.Split(parts[2], ",")
	}
	return id, nil
}

// VerifierIface is the interface both Verifier and FakeVerifier satisfy.
type VerifierIface interface {
	Verify(ctx context.Context, rawToken string) (*Identity, error)
}

// Compile-time assertions that both types satisfy the interface.
var _ VerifierIface = (*Verifier)(nil)
var _ VerifierIface = FakeVerifier{}
