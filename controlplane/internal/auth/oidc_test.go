package auth

import (
	"context"
	"testing"
)

func TestFakeVerifier_Roundtrip(t *testing.T) {
	v := FakeVerifier{}
	id, err := v.Verify(context.Background(), "fake:user-1:user@example.com:dlh-admins,dlh-runners")
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if id.Subject != "user-1" {
		t.Errorf("subject: %q", id.Subject)
	}
	if id.Email != "user@example.com" {
		t.Errorf("email: %q", id.Email)
	}
	if len(id.Groups) != 2 || id.Groups[0] != "dlh-admins" {
		t.Errorf("groups: %v", id.Groups)
	}
}

func TestFakeVerifier_Malformed(t *testing.T) {
	v := FakeVerifier{}
	if _, err := v.Verify(context.Background(), "not-a-fake"); err == nil {
		t.Fatal("expected error")
	}
}
