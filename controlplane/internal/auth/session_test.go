package auth

import (
	"testing"
	"time"
)

func TestSessionIssuer_RoundTrip(t *testing.T) {
	s := &SessionIssuer{Key: []byte("hunter2-hunter2-hunter2-hunter2!")}
	id := &Identity{Subject: "user-1", Email: "u@example.com", Groups: []string{"dlh-runners"}}
	tok, err := s.Issue(id)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	got, err := s.Verify(tok)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if got.Subject != "user-1" || got.Email != "u@example.com" {
		t.Errorf("identity roundtrip: %+v", got)
	}
	if len(got.Groups) != 1 || got.Groups[0] != "dlh-runners" {
		t.Errorf("groups: %v", got.Groups)
	}
}

func TestSessionIssuer_RejectsWrongSignature(t *testing.T) {
	a := &SessionIssuer{Key: []byte("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")}
	b := &SessionIssuer{Key: []byte("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")}
	tok, _ := a.Issue(&Identity{Subject: "x"})
	if _, err := b.Verify(tok); err == nil {
		t.Fatal("expected verify to fail with different key")
	}
}

func TestSessionIssuer_RejectsExpired(t *testing.T) {
	s := &SessionIssuer{Key: []byte("kkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkk"), Lifetime: -1 * time.Minute}
	tok, _ := s.Issue(&Identity{Subject: "x"})
	if _, err := s.Verify(tok); err == nil {
		t.Fatal("expected verify to fail for expired token")
	}
}

func TestSessionIssuer_MissingKey(t *testing.T) {
	s := &SessionIssuer{}
	if _, err := s.Issue(&Identity{Subject: "x"}); err == nil {
		t.Fatal("expected Issue to fail without key")
	}
	if _, err := s.Verify("anything"); err == nil {
		t.Fatal("expected Verify to fail without key")
	}
}
