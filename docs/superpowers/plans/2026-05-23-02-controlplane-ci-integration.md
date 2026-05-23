# dlh-controlplane Phase E (CI Integration + Cleanup) — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Close out the controlplane migration: add OIDC token exchange so CI (GitHub Actions OIDC) can submit scenarios; add `dlh login` device-code flow for engineers; convert kafka + doris scenarios to chart-managed `WorkflowTemplate`s with `target_id` propagation; delete the now-redundant shell scripts.

**Architecture:** A new `POST /api/oidc/exchange` endpoint validates an external OIDC token (issuer + audience + subject claims) and returns a short-lived (1h) controlplane-issued JWT. The dlh CLI gains a `login` subcommand that completes a standard OIDC device-code flow against the configured issuer and persists the resulting id_token to `~/.config/dlh/token`. A reusable composite GitHub Action (`.github/actions/dlh-run/action.yml`) requests an OIDC token from the GH Actions JWT issuer, exchanges it via `/api/oidc/exchange`, and calls `dlh run --wait`. The two remaining standalone scenario `Workflow` YAMLs (kafka, doris) get promoted into chart-managed `WorkflowTemplate`s under `helm/dlh-test-fw/files/workflowtemplates/scenario/` with target_id parameter propagation matching the Plan 17 fix. Six shell scripts (run-scenario.sh + 4 platform-*.sh + verify-templates.sh) get deleted; `minikube-up.sh` is kept for local-dev.

**Tech Stack:** Go 1.26 (existing module); `github.com/golang-jwt/jwt/v5` for the short-lived session JWT signing (new dep); existing `go-oidc/v3` for token verification; GitHub Actions `id-token: write` permission for OIDC; standard OAuth 2.0 device-code flow (RFC 8628) for `dlh login`.

**Reference spec:** `docs/superpowers/specs/2026-05-21-dlh-controlplane-design.md` (§9.4 CLI / CI tokens, §12 Phase E, §14 open question #1 IdP).

**Branch & worktree:** Per `CLAUDE.md`, work on `feat/plan18-controlplane-ci-integration` in worktree `/Users/allen/repo/dlh-test-fw-plan18`. Task 1 creates it.

**Plan-time decisions / deviations from spec:**

1. **Session JWT, not opaque token.** Spec §9.4 says "short-lived controlplane session token". We use a self-signed HS256 JWT with a 1h `exp`, signed using a new `DLH_SESSION_SIGNING_KEY` env (auto-generated Secret like `dlh-internal-token`). This avoids needing server-side session storage and lets the existing OIDC verifier path inspect the same `Bearer` header — middleware tries OIDC first, falls back to controlplane-JWT.
2. **No persistent revocation list.** A 1h session can't be revoked mid-life. Acceptable for v1; if revocation becomes important, add a Redis-backed revocation list later.
3. **GitHub Actions only** for the reusable action. Other CI providers (GitLab, CircleCI) work too but the example workflow + composite action ship for GH Actions only. The exchange endpoint is provider-neutral — it just validates issuer + audience.
4. **Audience validation is configurable per environment.** Allow `DLH_CI_AUDIENCE` env (default `dlh-controlplane`); operators set the matching audience in their GH Actions OIDC token request.
5. **Issuer allowlist** ships with GitHub Actions' issuer (`https://token.actions.githubusercontent.com`) by default. Additional issuers via `DLH_CI_TRUSTED_ISSUERS` (comma-separated). The exchange endpoint rejects tokens from any issuer not in the allowlist.
6. **`dlh login` defaults to using DLH_OIDC_ISSUER_URL** discovered from the controlplane's `/healthz`-adjacent metadata endpoint. To avoid creating a new "settings" endpoint, expose this via a new `GET /api/auth/info` endpoint that returns the issuer + client id + audience (all already in config; safe to expose).
7. **Scope of scenario sweep:** convert kafka + doris from standalone `Workflow` YAMLs in `scenarios/` to chart-managed `WorkflowTemplate`s in `helm/dlh-test-fw/files/workflowtemplates/scenario/`. After Plan E completes, the `scenarios/` directory only retains the chart-managed sources (mysql is already in the chart). The standalone Workflow YAMLs at `scenarios/*.yaml` are deleted since `run-scenario.sh` is being removed.
8. **`scripts/minikube-up.sh` is kept.** Local-dev convenience — operators still need a way to spin up a clean minikube for testing the chart.
9. **Session JWT secret distribution.** Like Plan 16's `dlh-internal-token` Secret, the chart ships a `dlh-session-signing-key` Secret with a helm-lookup-stable random key. Operators don't manage the value directly; it survives helm upgrades.
10. **Natural pause points:** after Task 8 (exchange endpoint + JWT + auth/info — backend ready), Task 13 (CLI `login` works; CI action shells out to it), Task 17 (scenarios swept), Task 21 (everything except smoke + merge).

---

## File Structure

**New files (Go backend):**
- `controlplane/internal/auth/session.go` — `SessionIssuer` mints + verifies short-lived HS256 JWTs.
- `controlplane/internal/auth/session_test.go`
- `controlplane/internal/auth/exchange.go` — `Exchanger` validates external OIDC tokens against issuer allowlist + audience.
- `controlplane/internal/auth/exchange_test.go`
- `controlplane/internal/api/oidc_exchange.go` — `POST /api/oidc/exchange` handler.
- `controlplane/internal/api/auth_info.go` — `GET /api/auth/info` handler.

**Modified files (Go backend):**
- `controlplane/api/openapi.yaml` — add `/api/oidc/exchange` + `/api/auth/info` paths + schemas; regenerate.
- `controlplane/internal/api/gen/*.gen.go` — regenerated.
- `controlplane/internal/api/handlers.go` — wire new handlers + populate stubs.
- `controlplane/internal/auth/middleware.go` — accept controlplane session JWTs in addition to OIDC id_tokens.
- `controlplane/internal/config/config.go` — add `SessionSigningKey` + `CITrustedIssuers` + `CIAudience`.
- `controlplane/internal/api/server.go` — Deps gains `*auth.SessionIssuer` + `*auth.Exchanger`.
- `controlplane/cmd/dlh-controlplane/main.go` — construct session issuer + exchanger; wire into Deps.
- `controlplane/deploy/deployment.yaml` — add `DLH_SESSION_SIGNING_KEY` env from new Secret.
- `controlplane/deploy/role.yaml` — already permits Secret reads (Plan 17).

**New files (Helm chart):**
- `helm/dlh-test-fw/templates/dlh-session-signing-secret.yaml` — Argo-CD-stable signing key Secret.
- `helm/dlh-test-fw/files/workflowtemplates/scenario/kafka-broker-partition.yaml` — chart-managed WT.
- `helm/dlh-test-fw/files/workflowtemplates/scenario/doris-be-network-loss.yaml` — chart-managed WT.

**New files (CLI):**
- `controlplane/cmd/dlh/login.go` — `dlh login` device-code OIDC flow.

**Modified files (CLI):**
- `controlplane/cmd/dlh/root.go` — register `loginCmd()`; load `~/.config/dlh/token` as default token if `--token`/`DLH_TOKEN` unset.

**New files (CI):**
- `.github/actions/dlh-run/action.yml` — reusable composite action.
- `.github/workflows/example-release-gate.yml` — sample workflow demonstrating use.

**Deleted files:**
- `scripts/run-scenario.sh`
- `scripts/platform-up.sh`
- `scripts/platform-down.sh`
- `scripts/platform-verify.sh`
- `scripts/verify-templates.sh`
- `scenarios/mysql-pod-delete.yaml` (chart-managed equivalent already exists; standalone Workflow no longer needed)
- `scenarios/kafka-broker-partition.yaml` (replaced by chart-managed WT in Task 16)
- `scenarios/doris-be-network-loss.yaml` (replaced by chart-managed WT in Task 17)

**Documentation updates:**
- `docs/FINDINGS.md` — Plan 18 section: post-controlplane operational model + Phase E pitfalls.
- `CLAUDE.md` — drop the obsolete script references; add `dlh login` + CI integration notes.
- `README.md` — Plan 18 row; remove instructions that referenced `run-scenario.sh`.
- `docs/operations/ci-integration.md` (new) — runbook for wiring a GH Actions workflow to the controlplane.

**Unchanged:** verdict-job, k6 image, dashboards, Argo CD manifests, all controlplane code from Plans 15-17 except the listed modifications.

---

## Task 1: Baseline + worktree

No commits.

- [ ] **Step 1: Verify clean main + CI green + Phase D present.**

```bash
cd /Users/allen/repo/dlh-test-fw
git status
git log --first-parent --oneline -5
gh run list --branch main --limit 1
ls controlplane/internal/targets/
ls controlplane/internal/chaos/
```

Expected: clean tree on `main`; HEAD includes `753324b` (Plan 17 README backfill) and `e9d73b6` (Plan 17 merge); CI `success`; targets + chaos packages present.

- [ ] **Step 2: Create the feature worktree using ABSOLUTE path** (Plan 15 cwd issue avoidance).

```bash
cd /Users/allen/repo/dlh-test-fw
git worktree add /Users/allen/repo/dlh-test-fw-plan18 -b feat/plan18-controlplane-ci-integration main
cd /Users/allen/repo/dlh-test-fw-plan18
git worktree list
git status
```

Expected: clean tree on `feat/plan18-controlplane-ci-integration`; worktree at the sibling path.

- [ ] **Step 3: Verify Phase D baseline passes:**

```bash
cd /Users/allen/repo/dlh-test-fw-plan18/controlplane
make ui-build 2>&1 | tail -3
go build ./...
go test ./...
```

Expected: clean ui-build + go build; all Phase D tests pass.

All remaining tasks run from `/Users/allen/repo/dlh-test-fw-plan18`.

---

# Section A — OIDC exchange + session JWT + auth-info (Tasks 2-8)

## Task 2: Config + session signing key

**Files:**
- Modify: `controlplane/internal/config/config.go`
- Modify: `controlplane/internal/config/config_test.go`

- [ ] **Step 1: Inspect current Config struct.**

```bash
cd /Users/allen/repo/dlh-test-fw-plan18/controlplane
grep -A 30 "^type Config struct" internal/config/config.go
```

- [ ] **Step 2: Add `SessionSigningKey`, `CITrustedIssuers`, `CIAudience` fields.**

Use Edit. Find the existing `InternalToken string` field and append after it:

```go
	InternalToken     string
	SessionSigningKey string
	CITrustedIssuers  []string
	CIAudience        string
```

In the `Load()` function, find where InternalToken is read and append:

```go
		SessionSigningKey: os.Getenv("DLH_SESSION_SIGNING_KEY"),
		CITrustedIssuers:  parseCSV(getenv("DLH_CI_TRUSTED_ISSUERS", "https://token.actions.githubusercontent.com")),
		CIAudience:        getenv("DLH_CI_AUDIENCE", "dlh-controlplane"),
```

Append a helper at the end of the file:

```go
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
```

Add `"strings"` to the import block.

In the validation block at the end of `Load()`, add (after the existing AuthDisabled + InternalToken checks):

```go
	if !c.AuthDisabled && c.SessionSigningKey == "" {
		return nil, fmt.Errorf("DLH_SESSION_SIGNING_KEY is required when auth is enabled")
	}
```

- [ ] **Step 3: Add tests.**

Append to `internal/config/config_test.go`:

```go
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
```

- [ ] **Step 4: Build + test.**

```bash
cd /Users/allen/repo/dlh-test-fw-plan18/controlplane
go build ./...
go test ./internal/config/... -v
```

Expected: all config tests pass (existing 3 + 3 new = 6).

- [ ] **Step 5: Commit.**

```bash
cd /Users/allen/repo/dlh-test-fw-plan18
git add controlplane/internal/config/
git status
git commit -m "feat(controlplane/config): add SessionSigningKey + CITrustedIssuers + CIAudience

Required when auth is enabled. Defaults trust GH Actions' OIDC issuer
and audience 'dlh-controlplane'. CSV-parsed for multiple issuers."
```

---

## Task 3: SessionIssuer — sign + verify short-lived HS256 JWTs

**Files:**
- Create: `controlplane/internal/auth/session.go`
- Create: `controlplane/internal/auth/session_test.go`

- [ ] **Step 1: Add jwt dep.**

```bash
cd /Users/allen/repo/dlh-test-fw-plan18/controlplane
go get github.com/golang-jwt/jwt/v5@v5.2.1
```

- [ ] **Step 2: Write `internal/auth/session.go`:**

```go
package auth

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// SessionIssuer mints and verifies short-lived controlplane session
// tokens. Issued in exchange for a CI-OIDC token (POST /api/oidc/exchange)
// or — future — for a `dlh login` device-code flow. Backed by an HMAC
// secret held in the dlh-session-signing-key Secret.
type SessionIssuer struct {
	Key      []byte
	Lifetime time.Duration // default 1 hour
	Issuer   string        // exposed in `iss` claim — defaults to "dlh-controlplane"
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

// Verify parses + validates the token and returns the embedded identity.
// Returns ErrInvalidSession if the token is malformed, expired, or
// signed with a different key.
var ErrInvalidSession = errors.New("invalid session token")

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
```

- [ ] **Step 3: Write `internal/auth/session_test.go`:**

```go
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
```

- [ ] **Step 4: Build + test.**

```bash
go mod tidy
go build ./...
go test ./internal/auth/... -v
```

Expected: 4 new tests PASS; existing auth tests still pass.

- [ ] **Step 5: Commit.**

```bash
git add controlplane/internal/auth/session.go controlplane/internal/auth/session_test.go controlplane/go.mod controlplane/go.sum
git commit -m "feat(controlplane/auth): SessionIssuer mints + verifies short-lived HS256 JWTs

1h default lifetime, HMAC-SHA256 signing. Used by /api/oidc/exchange
(CI token swap) and dlh login (device-code flow)."
```

---

## Task 4: Exchanger — validate external OIDC tokens against allowlist

**Files:**
- Create: `controlplane/internal/auth/exchange.go`
- Create: `controlplane/internal/auth/exchange_test.go`

- [ ] **Step 1: Write `internal/auth/exchange.go`:**

```go
package auth

import (
	"context"
	"errors"
	"fmt"

	"github.com/coreos/go-oidc/v3/oidc"
)

// Exchanger validates external OIDC tokens (GH Actions JWT, GitLab JWT, etc.)
// against a configured issuer allowlist + required audience. On success
// returns an Identity built from the token's standard claims.
type Exchanger struct {
	// TrustedIssuers is the set of acceptable `iss` values.
	TrustedIssuers []string
	// RequiredAudience is checked against the token's `aud` claim.
	RequiredAudience string
	// providers caches per-issuer go-oidc providers.
	providers map[string]*oidc.Provider
}

// ErrIssuerNotTrusted indicates the token's iss doesn't match the allowlist.
var ErrIssuerNotTrusted = errors.New("token issuer not in trusted allowlist")

// Validate parses the token (no verification yet) to extract iss, looks
// up the matching provider, and runs full verification. Returns an
// Identity on success.
func (e *Exchanger) Validate(ctx context.Context, rawToken string) (*Identity, error) {
	if rawToken == "" {
		return nil, errors.New("empty token")
	}
	// Quick parse to get iss before paying for verification.
	parsed, err := parseIssuerOnly(rawToken)
	if err != nil {
		return nil, fmt.Errorf("parse iss: %w", err)
	}
	if !contains(e.TrustedIssuers, parsed) {
		return nil, fmt.Errorf("%w: %s", ErrIssuerNotTrusted, parsed)
	}
	prov, err := e.providerFor(ctx, parsed)
	if err != nil {
		return nil, fmt.Errorf("oidc provider %s: %w", parsed, err)
	}
	verifier := prov.Verifier(&oidc.Config{
		ClientID: e.RequiredAudience,
	})
	tok, err := verifier.Verify(ctx, rawToken)
	if err != nil {
		return nil, fmt.Errorf("verify: %w", err)
	}
	// CI claims are a free-for-all; build Identity from standard fields
	// plus any 'repository' / 'sub' that the action provider sets.
	claims := map[string]any{}
	_ = tok.Claims(&claims)
	subject, _ := claims["sub"].(string)
	if subject == "" {
		subject = tok.Subject
	}
	email, _ := claims["email"].(string)
	id := &Identity{Subject: subject, Email: email}
	// Map repository/workflow into groups when present (e.g. GH Actions).
	if repo, ok := claims["repository"].(string); ok && repo != "" {
		id.Groups = append(id.Groups, "ci-repo:"+repo)
	}
	if owner, ok := claims["repository_owner"].(string); ok && owner != "" {
		id.Groups = append(id.Groups, "ci-owner:"+owner)
	}
	return id, nil
}

func (e *Exchanger) providerFor(ctx context.Context, issuer string) (*oidc.Provider, error) {
	if e.providers == nil {
		e.providers = map[string]*oidc.Provider{}
	}
	if p, ok := e.providers[issuer]; ok {
		return p, nil
	}
	p, err := oidc.NewProvider(ctx, issuer)
	if err != nil {
		return nil, err
	}
	e.providers[issuer] = p
	return p, nil
}

func contains(haystack []string, needle string) bool {
	for _, h := range haystack {
		if h == needle {
			return true
		}
	}
	return false
}
```

Add a sibling helper file `internal/auth/jwt_peek.go`:

```go
package auth

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
)

// parseIssuerOnly extracts the iss claim from a JWT without verifying
// the signature. Used by Exchanger to choose the right OIDC provider
// before paying for jwks fetch + verification.
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
```

- [ ] **Step 2: Write `internal/auth/exchange_test.go`:**

The real OIDC verification path needs a JWKS endpoint — too heavy for unit tests. Test the helpers in isolation and the issuer-allowlist gate.

```go
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
```

- [ ] **Step 3: Build + test.**

```bash
go build ./...
go test ./internal/auth/... -v
```

Expected: 4 new tests pass; existing tests still pass.

- [ ] **Step 4: Commit.**

```bash
git add controlplane/internal/auth/exchange.go controlplane/internal/auth/jwt_peek.go controlplane/internal/auth/exchange_test.go
git commit -m "feat(controlplane/auth): Exchanger validates external OIDC against issuer allowlist

Peek the iss claim without verification to select the right go-oidc
provider, then run full verification with the configured audience.
GH-Actions-style 'repository' / 'repository_owner' claims map into
groups so RBAC bindings can target CI principals."
```

---

## Task 5: Auth middleware accepts session JWTs

**Files:**
- Modify: `controlplane/internal/auth/middleware.go`

- [ ] **Step 1: Inspect existing middleware.**

```bash
cd /Users/allen/repo/dlh-test-fw-plan18/controlplane
grep -A 30 "^func Middleware" internal/auth/middleware.go
```

The existing `Middleware(verifier VerifierIface, roles *Roles)` validates OIDC bearer tokens.

- [ ] **Step 2: Update to also accept controlplane session JWTs.**

Use Edit. Replace the existing `Middleware` function with a new signature + body:

old_string anchor (the function signature):
```go
func Middleware(verifier VerifierIface, roles *Roles) func(http.Handler) http.Handler {
```

new_string:
```go
func Middleware(verifier VerifierIface, roles *Roles, sessionIssuer *SessionIssuer) func(http.Handler) http.Handler {
```

Then replace the function body. Find the existing body. Replace the token verification block — instead of just calling `verifier.Verify`, try session JWT first (cheap; local-only), fall back to OIDC.

Replace the inner anonymous func body with:

```go
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			hdr := r.Header.Get("Authorization")
			if !strings.HasPrefix(hdr, "Bearer ") {
				http.Error(w, "missing bearer token", http.StatusUnauthorized)
				return
			}
			raw := strings.TrimPrefix(hdr, "Bearer ")

			var id *Identity
			var err error
			// Try controlplane session JWT first (cheap, local).
			if sessionIssuer != nil {
				id, err = sessionIssuer.Verify(raw)
			}
			// Fall back to OIDC bearer verification.
			if id == nil {
				id, err = verifier.Verify(r.Context(), raw)
			}
			if err != nil || id == nil {
				http.Error(w, "invalid token", http.StatusUnauthorized)
				return
			}
			role := roles.Resolve(id)
			ctx := context.WithValue(r.Context(), identityKey, id)
			ctx = context.WithValue(ctx, roleKey, role)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
```

- [ ] **Step 3: Update callers.** Find every place that calls `Middleware(...)`. Currently only `cmd/dlh-controlplane/main.go` and possibly `internal/api/server.go`. Search:

```bash
grep -rn "auth.Middleware" controlplane/ --include="*.go"
```

Update each call site to pass `nil` for the new `sessionIssuer` parameter (it'll be wired with a real value in Task 7).

For `cmd/dlh-controlplane/main.go`, replace:
```go
authMW := auth.Middleware(verifier, roles)
```
with:
```go
authMW := auth.Middleware(verifier, roles, nil) // session issuer wired in Task 7
```

- [ ] **Step 4: Build + tests.**

```bash
go build ./...
go test ./internal/auth/...
```

Expected: clean build; all auth tests still pass (existing middleware tests, if any, may pass nil for sessionIssuer).

If existing tests call the old signature, they need updating. Search:

```bash
grep -rn "auth.Middleware\|Middleware(" controlplane/internal/auth/ --include="*_test.go"
```

If found, add `nil` to the call.

- [ ] **Step 5: Commit.**

```bash
git add controlplane/internal/auth/middleware.go controlplane/cmd/dlh-controlplane/main.go
git commit -m "feat(controlplane/auth): middleware accepts session JWTs + OIDC tokens

Session-JWT verification runs first (cheap; local HMAC). OIDC verification
remains the fallback. Identity context attachment is unchanged."
```

---

## Task 6: OpenAPI: /api/oidc/exchange + /api/auth/info

**Files:**
- Modify: `controlplane/api/openapi.yaml`
- Regenerate: `controlplane/internal/api/gen/*.gen.go`, `controlplane/web/src/api/gen.ts`
- Modify: `controlplane/internal/api/handlers.go` (stubs)

- [ ] **Step 1: Inspect current spec + find a good insertion point.**

```bash
cd /Users/allen/repo/dlh-test-fw-plan18/controlplane
grep -n "^  /\|^components:" api/openapi.yaml | head -15
```

- [ ] **Step 2: Add new paths.**

Use Edit to insert new paths before the `components:` line. Find the last existing path block — likely `/api/targets/{id}/test`. Insert the new paths after it (anchor on `^components:`):

```
  /api/oidc/exchange:
    post:
      operationId: oidcExchange
      security: []   # public endpoint — token validated by Exchanger
      requestBody:
        required: true
        content:
          application/json:
            schema: { $ref: "#/components/schemas/ExchangeRequest" }
      responses:
        "200":
          description: short-lived session token
          content:
            application/json:
              schema: { $ref: "#/components/schemas/ExchangeResponse" }
        "400":
          description: invalid request
        "401":
          description: token rejected (untrusted issuer, bad audience, expired, etc.)
  /api/auth/info:
    get:
      operationId: getAuthInfo
      security: []   # public — clients consult this before logging in
      responses:
        "200":
          description: client-facing auth config
          content:
            application/json:
              schema: { $ref: "#/components/schemas/AuthInfo" }
components:
```

- [ ] **Step 3: Add schemas.**

Find the last existing schema in `components.schemas:` (likely `ProbeResult`). Append:

```yaml
    ExchangeRequest:
      type: object
      required: [token]
      properties:
        token:
          type: string
          description: "External OIDC token (GH Actions JWT, etc.)"
    ExchangeResponse:
      type: object
      required: [accessToken, expiresAt]
      properties:
        accessToken:
          type: string
          description: "Short-lived controlplane session JWT. Send as Bearer."
        expiresAt:
          type: string
          format: date-time
        subject:
          type: string
        groups:
          type: array
          items: { type: string }
    AuthInfo:
      type: object
      required: [oidcIssuer, oidcClientId]
      properties:
        oidcIssuer:    { type: string }
        oidcClientId:  { type: string }
        ciAudience:    { type: string, description: "Audience CI workflows should request when minting OIDC tokens" }
        authDisabled:  { type: boolean }
```

- [ ] **Step 4: Regenerate Go + TS clients.**

```bash
go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen -config api/oapi-codegen-server.yaml api/openapi.yaml
go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen -config api/oapi-codegen-types.yaml api/openapi.yaml
cd web && pnpm openapi-typescript ../api/openapi.yaml -o src/api/gen.ts && cd ..
```

- [ ] **Step 5: Look up new response types.**

```bash
grep -nE "^type (OidcExchange|GetAuthInfo)" internal/api/gen/server.gen.go
```

Note the exact response-type names. Add stubs to `internal/api/handlers.go`. Append:

```go
// Phase E stubs. Real implementations land in Task 7.
func (h *Handlers) OidcExchange(_ context.Context, _ gen.OidcExchangeRequestObject) (gen.OidcExchangeResponseObject, error) {
	return gen.OidcExchange401Response{}, nil
}
func (h *Handlers) GetAuthInfo(_ context.Context, _ gen.GetAuthInfoRequestObject) (gen.GetAuthInfoResponseObject, error) {
	return gen.GetAuthInfo200JSONResponse{}, nil
}
```

(Substitute the codegen-emitted names if they differ.)

- [ ] **Step 6: Build + test.**

```bash
go build ./...
go test ./...
```

Expected: clean build; all tests still pass.

- [ ] **Step 7: Commit.**

```bash
git add controlplane/api/openapi.yaml controlplane/internal/api/gen/ controlplane/internal/api/handlers.go controlplane/web/src/api/gen.ts
git commit -m "feat(controlplane): OpenAPI for /api/oidc/exchange + /api/auth/info

Public endpoints (no Bearer required). Exchange returns a short-lived
controlplane session JWT in exchange for an external OIDC token (GH
Actions JWT, etc.). AuthInfo returns the client-facing config so dlh
login can discover the IdP without hardcoding."
```

---

## Task 7: Wire SessionIssuer + Exchanger handlers

**Files:**
- Modify: `controlplane/internal/api/server.go` (Deps gains the two new fields)
- Create: `controlplane/internal/api/oidc_exchange.go`
- Create: `controlplane/internal/api/auth_info.go`
- Modify: `controlplane/internal/api/handlers.go` (real impls)
- Modify: `controlplane/cmd/dlh-controlplane/main.go` (construct + wire)

- [ ] **Step 1: Extend Deps.**

In `internal/api/server.go`, add to the struct:

```go
SessionIssuer *auth.SessionIssuer
Exchanger     *auth.Exchanger
AuthInfo      AuthInfoConfig
```

Define `AuthInfoConfig` near Deps:

```go
type AuthInfoConfig struct {
	OIDCIssuer   string
	OIDCClientID string
	CIAudience   string
	AuthDisabled bool
}
```

Add the import `"github.com/dlh/dlh-test-fw/controlplane/internal/auth"` if not already present.

- [ ] **Step 2: Write `internal/api/oidc_exchange.go`:**

```go
package api

import (
	"context"

	"github.com/dlh/dlh-test-fw/controlplane/internal/api/gen"
)

func (h *Handlers) handleOidcExchange(ctx context.Context, body *gen.ExchangeRequest) (gen.OidcExchangeResponseObject, error) {
	if body == nil || body.Token == "" {
		return gen.OidcExchange400Response{}, nil
	}
	if h.deps.Exchanger == nil || h.deps.SessionIssuer == nil {
		return gen.OidcExchange401Response{}, nil
	}
	id, err := h.deps.Exchanger.Validate(ctx, body.Token)
	if err != nil {
		return gen.OidcExchange401Response{}, nil
	}
	tok, err := h.deps.SessionIssuer.Issue(id)
	if err != nil {
		return gen.OidcExchange401Response{}, nil
	}
	expires := nowPlusLifetime(h.deps.SessionIssuer.Lifetime)
	groups := append([]string(nil), id.Groups...)
	return gen.OidcExchange200JSONResponse{
		AccessToken: tok,
		ExpiresAt:   expires,
		Subject:     &id.Subject,
		Groups:      &groups,
	}, nil
}
```

Add a tiny helper at the bottom:

```go
func nowPlusLifetime(d time.Duration) time.Time {
	if d == 0 {
		d = time.Hour
	}
	return time.Now().UTC().Add(d)
}
```

Add `"time"` to the import block.

- [ ] **Step 3: Write `internal/api/auth_info.go`:**

```go
package api

import (
	"context"

	"github.com/dlh/dlh-test-fw/controlplane/internal/api/gen"
)

func (h *Handlers) handleAuthInfo(_ context.Context) gen.GetAuthInfoResponseObject {
	cfg := h.deps.AuthInfo
	ciAud := cfg.CIAudience
	disabled := cfg.AuthDisabled
	return gen.GetAuthInfo200JSONResponse{
		OidcIssuer:   cfg.OIDCIssuer,
		OidcClientId: cfg.OIDCClientID,
		CiAudience:   &ciAud,
		AuthDisabled: &disabled,
	}
}
```

- [ ] **Step 4: Replace the stub `OidcExchange` + `GetAuthInfo`** in `internal/api/handlers.go` with calls to the new helpers:

```go
func (h *Handlers) OidcExchange(ctx context.Context, req gen.OidcExchangeRequestObject) (gen.OidcExchangeResponseObject, error) {
	return h.handleOidcExchange(ctx, req.Body)
}
func (h *Handlers) GetAuthInfo(ctx context.Context, _ gen.GetAuthInfoRequestObject) (gen.GetAuthInfoResponseObject, error) {
	return h.handleAuthInfo(ctx), nil
}
```

(Replace the Task 6 stubs.)

- [ ] **Step 5: Wire into `cmd/dlh-controlplane/main.go`.**

After the existing auth verifier construction (where `verifier auth.VerifierIface` is built), add:

```go
sessionIssuer := &auth.SessionIssuer{
	Key:      []byte(cfg.SessionSigningKey),
	Lifetime: time.Hour,
}
exchanger := &auth.Exchanger{
	TrustedIssuers:   cfg.CITrustedIssuers,
	RequiredAudience: cfg.CIAudience,
}
```

In the Deps literal add:
```go
SessionIssuer: sessionIssuer,
Exchanger:     exchanger,
AuthInfo: api.AuthInfoConfig{
	OIDCIssuer:   cfg.OIDCIssuerURL,
	OIDCClientID: cfg.OIDCClientID,
	CIAudience:   cfg.CIAudience,
	AuthDisabled: cfg.AuthDisabled,
},
```

Update the auth middleware call to pass the session issuer:

```go
authMW := auth.Middleware(verifier, roles, sessionIssuer)
```

- [ ] **Step 6: Build + test.**

```bash
go build ./...
go test ./...
```

Expected: clean build; all tests pass.

If `gen.ExchangeRequest.Token` field shape is `*string` not `string`, adjust accordingly (handler nil-check the pointer first).

- [ ] **Step 7: Commit.**

```bash
git add controlplane/internal/api/server.go controlplane/internal/api/oidc_exchange.go \
        controlplane/internal/api/auth_info.go controlplane/internal/api/handlers.go \
        controlplane/cmd/dlh-controlplane/main.go
git commit -m "feat(controlplane): wire OidcExchange + GetAuthInfo handlers

Exchanger validates external OIDC; SessionIssuer mints a 1h JWT.
GetAuthInfo exposes the IdP config so 'dlh login' doesn't need a
flag for issuer URL."
```

---

## Task 8: Helm chart Secret + Deployment env

**Files:**
- Create: `helm/dlh-test-fw/templates/dlh-session-signing-secret.yaml`
- Modify: `controlplane/deploy/deployment.yaml`

- [ ] **Step 1: Write the secret template** (lookup-stable pattern mirrors `dlh-internal-token-secret.yaml`):

```yaml
{{- $existing := lookup "v1" "Secret" .Values.namespace "dlh-session-signing-key" -}}
{{- $key := "" -}}
{{- if $existing -}}
  {{- $key = index $existing.data "key" | default (randAlphaNum 64 | b64enc) -}}
{{- else -}}
  {{- $key = randAlphaNum 64 | b64enc -}}
{{- end -}}
apiVersion: v1
kind: Secret
metadata:
  name: dlh-session-signing-key
  namespace: {{ .Values.namespace }}
  labels:
    app.kubernetes.io/part-of: dlh-test-fw
type: Opaque
data:
  key: {{ $key | quote }}
```

- [ ] **Step 2: Add a `DLH_SESSION_SIGNING_KEY` env to the controlplane Deployment.**

Open `controlplane/deploy/deployment.yaml`. Find the existing `env:` block (after Plan 16 it includes DLH_INTERNAL_TOKEN). Append a new entry sourced from the new Secret:

```yaml
            - name: DLH_SESSION_SIGNING_KEY
              valueFrom:
                secretKeyRef:
                  name: dlh-session-signing-key
                  key: key
```

Use Edit. Find the existing `DLH_INTERNAL_TOKEN` env block (3 lines) and add the new block immediately after.

- [ ] **Step 3: Render + validate.**

```bash
cd /Users/allen/repo/dlh-test-fw-plan18
helm dependency update helm/dlh-test-fw 2>&1 | tail -3
helm template dlh helm/dlh-test-fw > /tmp/rendered-plan18.yaml
grep -A 5 'name: dlh-session-signing-key' /tmp/rendered-plan18.yaml | head -10
grep -c 'DLH_SESSION_SIGNING_KEY' controlplane/deploy/deployment.yaml
kubeconform -skip CustomResourceDefinition -strict -summary \
  -schema-location default \
  -schema-location 'https://raw.githubusercontent.com/datreeio/CRDs-catalog/main/{{.Group}}/{{.ResourceKind}}_{{.ResourceAPIVersion}}.json' \
  /tmp/rendered-plan18.yaml controlplane/deploy/*.yaml 2>&1 | tail -3
```

Expected: Secret rendered; DLH_SESSION_SIGNING_KEY appears in deployment.yaml; kubeconform Invalid=0.

- [ ] **Step 4: Commit.**

```bash
git add helm/dlh-test-fw/templates/dlh-session-signing-secret.yaml controlplane/deploy/deployment.yaml
git commit -m "feat(chart): dlh-session-signing-key Secret + DLH_SESSION_SIGNING_KEY env

Random 64-char alphanumeric on first install; lookup-stable on upgrade.
Mirrors the Plan 16 dlh-internal-token pattern."
```

**Section A complete.** Backend can mint + verify session tokens; CI exchange path is live; auth info is queryable.

---

# Section B — dlh login + CI action + CLI token cache (Tasks 9-13)

## Task 9: dlh login — device-code OIDC flow

**Files:**
- Create: `controlplane/cmd/dlh/login.go`

- [ ] **Step 1: Write `cmd/dlh/login.go`:**

```go
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

func loginCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "login",
		Short: "Authenticate via OIDC device-code flow + cache the token at ~/.config/dlh/token",
		RunE: func(_ *cobra.Command, _ []string) error {
			return runLogin()
		},
	}
	return c
}

func runLogin() error {
	// Step 1: consult the controlplane for IdP config.
	infoURL := strings.TrimRight(flagEndpoint, "/") + "/api/auth/info"
	resp, err := http.Get(infoURL)
	if err != nil {
		return fmt.Errorf("query auth info: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return fmt.Errorf("auth info HTTP %d: %s", resp.StatusCode, body)
	}
	var info struct {
		OidcIssuer   string `json:"oidcIssuer"`
		OidcClientId string `json:"oidcClientId"`
		AuthDisabled bool   `json:"authDisabled"`
	}
	if err := json.Unmarshal(body, &info); err != nil {
		return fmt.Errorf("parse auth info: %w", err)
	}
	if info.AuthDisabled {
		return fmt.Errorf("controlplane has DLH_AUTH_DISABLED=true; no login needed")
	}
	if info.OidcIssuer == "" || info.OidcClientId == "" {
		return fmt.Errorf("controlplane auth info missing issuer or client id")
	}

	// Step 2: discover the device authorization endpoint.
	disco, err := discoverOIDC(info.OidcIssuer)
	if err != nil {
		return fmt.Errorf("oidc discovery: %w", err)
	}
	if disco.DeviceAuthorizationEndpoint == "" || disco.TokenEndpoint == "" {
		return fmt.Errorf("issuer does not expose device-code endpoints")
	}

	// Step 3: initiate device code grant.
	dc, err := requestDeviceCode(disco.DeviceAuthorizationEndpoint, info.OidcClientId)
	if err != nil {
		return fmt.Errorf("device code: %w", err)
	}
	fmt.Printf("\nVisit: %s\nEnter code: %s\n\n", dc.VerificationURI, dc.UserCode)
	if dc.VerificationURIComplete != "" {
		fmt.Printf("(Or open: %s)\n\n", dc.VerificationURIComplete)
	}

	// Step 4: poll for the id_token.
	idToken, err := pollForToken(disco.TokenEndpoint, info.OidcClientId, dc)
	if err != nil {
		return fmt.Errorf("poll: %w", err)
	}

	// Step 5: persist to ~/.config/dlh/token.
	if err := saveToken(idToken); err != nil {
		return fmt.Errorf("save token: %w", err)
	}
	fmt.Println("Login successful. Token cached at ~/.config/dlh/token.")
	return nil
}

type oidcDiscovery struct {
	DeviceAuthorizationEndpoint string `json:"device_authorization_endpoint"`
	TokenEndpoint               string `json:"token_endpoint"`
}

func discoverOIDC(issuer string) (*oidcDiscovery, error) {
	u := strings.TrimRight(issuer, "/") + "/.well-known/openid-configuration"
	resp, err := http.Get(u)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("discovery HTTP %d: %s", resp.StatusCode, body)
	}
	var d oidcDiscovery
	if err := json.NewDecoder(resp.Body).Decode(&d); err != nil {
		return nil, err
	}
	return &d, nil
}

type deviceCode struct {
	DeviceCode              string `json:"device_code"`
	UserCode                string `json:"user_code"`
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete"`
	ExpiresIn               int    `json:"expires_in"`
	Interval                int    `json:"interval"`
}

func requestDeviceCode(endpoint, clientID string) (*deviceCode, error) {
	form := url.Values{}
	form.Set("client_id", clientID)
	form.Set("scope", "openid profile email")
	resp, err := http.PostForm(endpoint, form)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("device code HTTP %d: %s", resp.StatusCode, body)
	}
	var d deviceCode
	if err := json.Unmarshal(body, &d); err != nil {
		return nil, err
	}
	if d.Interval <= 0 {
		d.Interval = 5
	}
	if d.ExpiresIn <= 0 {
		d.ExpiresIn = 600
	}
	return &d, nil
}

func pollForToken(tokenEndpoint, clientID string, dc *deviceCode) (string, error) {
	deadline := time.Now().Add(time.Duration(dc.ExpiresIn) * time.Second)
	interval := time.Duration(dc.Interval) * time.Second
	ctx, cancel := context.WithDeadline(context.Background(), deadline)
	defer cancel()
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return "", fmt.Errorf("device code expired")
		case <-t.C:
			form := url.Values{}
			form.Set("client_id", clientID)
			form.Set("grant_type", "urn:ietf:params:oauth:grant-type:device_code")
			form.Set("device_code", dc.DeviceCode)
			resp, err := http.PostForm(tokenEndpoint, form)
			if err != nil {
				return "", err
			}
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			var tr struct {
				IdToken string `json:"id_token"`
				Error   string `json:"error"`
			}
			_ = json.Unmarshal(body, &tr)
			if tr.IdToken != "" {
				return tr.IdToken, nil
			}
			switch tr.Error {
			case "authorization_pending":
				continue
			case "slow_down":
				interval += 5 * time.Second
				t.Reset(interval)
			default:
				return "", fmt.Errorf("token endpoint: %s — %s", tr.Error, body)
			}
		}
	}
}

func saveToken(idToken string) error {
	dir, err := tokenDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "token"), []byte(idToken), 0o600)
}

func tokenDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "dlh"), nil
}

func loadCachedToken() string {
	dir, err := tokenDir()
	if err != nil {
		return ""
	}
	b, err := os.ReadFile(filepath.Join(dir, "token"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}
```

- [ ] **Step 2: Update `cmd/dlh/root.go`** to register `loginCmd()` + use `loadCachedToken()` as a fallback if `flagToken`/`DLH_TOKEN` is unset.

Find the existing `tokenDefault()` function:

```go
func tokenDefault() string {
	return os.Getenv("DLH_TOKEN")
}
```

Replace with:

```go
func tokenDefault() string {
	if v := os.Getenv("DLH_TOKEN"); v != "" {
		return v
	}
	return loadCachedToken()
}
```

Find the existing command registration in `rootCmd()`. It currently looks like:

```go
root.AddCommand(runCmd(), runsCmd())
```

Replace with:

```go
root.AddCommand(runCmd(), runsCmd(), loginCmd())
```

- [ ] **Step 3: Build the CLI + smoke-test --help:**

```bash
cd /Users/allen/repo/dlh-test-fw-plan18/controlplane
make cli
./bin/dlh login --help
./bin/dlh --help | grep -i login
```

Expected: `login` subcommand shows up.

- [ ] **Step 4: Commit.**

```bash
git add controlplane/cmd/dlh/login.go controlplane/cmd/dlh/root.go
git commit -m "feat(controlplane/cli): dlh login (device-code OIDC flow)

Discovers IdP via GET /api/auth/info, then walks the standard RFC 8628
device-code grant. id_token persists to ~/.config/dlh/token (mode 0600).
loadCachedToken used as fallback when DLH_TOKEN / --token unset."
```

---

## Task 10: Reusable GH Actions composite — .github/actions/dlh-run/action.yml

**Files:**
- Create: `.github/actions/dlh-run/action.yml`

- [ ] **Step 1: Write the composite action.**

```yaml
name: 'dlh-run'
description: 'Submit a dlh-test-fw scenario via the controlplane API and wait for it'
inputs:
  endpoint:
    description: 'Controlplane base URL (https://dlh.example.com)'
    required: true
  scenario:
    description: 'Scenario WorkflowTemplate name (e.g. mysql-pod-delete)'
    required: true
  target:
    description: 'Optional remote target ID; empty = framework cluster'
    required: false
    default: ''
  audience:
    description: 'OIDC audience to request (must match controlplane DLH_CI_AUDIENCE)'
    required: false
    default: 'dlh-controlplane'
  params:
    description: 'Newline-separated key=value WorkflowTemplate parameter overrides'
    required: false
    default: ''
  wait:
    description: 'Block until the run reaches a terminal phase'
    required: false
    default: 'true'
  dlh-cli-version:
    description: 'Version of the dlh CLI to install (must match controlplane)'
    required: false
    default: 'latest'

runs:
  using: composite
  steps:
    - name: Install dlh CLI
      shell: bash
      run: |
        set -euo pipefail
        # Fetch a prebuilt binary from the controlplane release artifacts.
        # If your deployment doesn't publish binaries, swap this for a
        # `go install` from a checked-out controlplane source tree.
        VERSION='${{ inputs.dlh-cli-version }}'
        ENDPOINT='${{ inputs.endpoint }}'
        # Placeholder: download URL would normally be a GitHub Release asset.
        # For internal deployments, prefer go install:
        if ! command -v dlh >/dev/null 2>&1; then
          echo "::error::dlh CLI not in PATH. See docs/operations/ci-integration.md."
          exit 127
        fi

    - name: Request GH Actions OIDC token
      id: oidc
      shell: bash
      env:
        AUDIENCE: ${{ inputs.audience }}
        ACTIONS_ID_TOKEN_REQUEST_TOKEN: ${{ env.ACTIONS_ID_TOKEN_REQUEST_TOKEN }}
        ACTIONS_ID_TOKEN_REQUEST_URL: ${{ env.ACTIONS_ID_TOKEN_REQUEST_URL }}
      run: |
        set -euo pipefail
        if [ -z "${ACTIONS_ID_TOKEN_REQUEST_TOKEN:-}" ]; then
          echo "::error::No OIDC request token. Did the calling job set permissions: id-token: write?"
          exit 1
        fi
        TOKEN=$(curl -fsSL \
          -H "Authorization: Bearer $ACTIONS_ID_TOKEN_REQUEST_TOKEN" \
          "${ACTIONS_ID_TOKEN_REQUEST_URL}&audience=${AUDIENCE}" | jq -r .value)
        echo "::add-mask::$TOKEN"
        echo "token=$TOKEN" >> "$GITHUB_OUTPUT"

    - name: Exchange OIDC for controlplane session
      id: session
      shell: bash
      env:
        ENDPOINT: ${{ inputs.endpoint }}
        OIDC_TOKEN: ${{ steps.oidc.outputs.token }}
      run: |
        set -euo pipefail
        RESP=$(curl -fsSL -X POST "$ENDPOINT/api/oidc/exchange" \
          -H 'Content-Type: application/json' \
          -d "$(jq -n --arg t "$OIDC_TOKEN" '{token:$t}')")
        ACCESS=$(echo "$RESP" | jq -r .accessToken)
        if [ -z "$ACCESS" ] || [ "$ACCESS" = "null" ]; then
          echo "::error::Exchange failed: $RESP"
          exit 1
        fi
        echo "::add-mask::$ACCESS"
        echo "token=$ACCESS" >> "$GITHUB_OUTPUT"

    - name: Submit scenario
      shell: bash
      env:
        ENDPOINT: ${{ inputs.endpoint }}
        DLH_TOKEN: ${{ steps.session.outputs.token }}
        SCENARIO: ${{ inputs.scenario }}
        TARGET: ${{ inputs.target }}
        PARAMS: ${{ inputs.params }}
        WAIT: ${{ inputs.wait }}
      run: |
        set -euo pipefail
        ARGS=()
        if [ -n "$TARGET" ]; then ARGS+=(--target "$TARGET"); fi
        if [ "$WAIT" = "true" ]; then ARGS+=(--wait); fi
        while IFS= read -r line; do
          [ -z "$line" ] && continue
          ARGS+=(--param "$line")
        done <<< "$PARAMS"
        dlh --endpoint "$ENDPOINT" run "$SCENARIO" "${ARGS[@]}"
```

- [ ] **Step 2: Validate the YAML syntax.**

```bash
cd /Users/allen/repo/dlh-test-fw-plan18
python3 -c "import yaml; list(yaml.safe_load_all(open('.github/actions/dlh-run/action.yml')))" && echo "valid YAML"
```

- [ ] **Step 3: Commit.**

```bash
git add .github/actions/dlh-run/action.yml
git commit -m "feat(ci): reusable composite action .github/actions/dlh-run

Three-step flow: request GH Actions OIDC -> exchange for controlplane
session via POST /api/oidc/exchange -> dlh run --wait. Audience defaults
to 'dlh-controlplane'. dlh CLI is assumed pre-installed by the calling
workflow (or via a previous step); the action will fail fast otherwise."
```

---

## Task 11: Example release-gate workflow

**Files:**
- Create: `.github/workflows/example-release-gate.yml`

- [ ] **Step 1: Write the example workflow.**

```yaml
# Example release-gating workflow demonstrating .github/actions/dlh-run.
#
# Adapt for your team: copy this file under .github/workflows/, set
# DLH_ENDPOINT as a repository secret, register `ci-repo:<owner>/<repo>`
# in the controlplane's dlh-roles ConfigMap as a `runner`.
#
# This file is committed as an EXAMPLE; the dlh-test-fw repo itself does
# not run release-gating against its own controlplane. Operators copy + rename.
name: example-release-gate
on:
  workflow_dispatch:
  pull_request:
    types: [opened, synchronize]
    paths:
      - 'helm/**'
      - 'controlplane/**'
      - 'scenarios/**'

permissions:
  id-token: write  # required for GH Actions OIDC
  contents: read

jobs:
  smoke:
    name: 'dlh smoke (mysql-pod-delete)'
    runs-on: ubuntu-latest
    timeout-minutes: 15
    steps:
      - uses: actions/checkout@v4

      # Build the dlh CLI from this checkout so it's in PATH for the action.
      - uses: actions/setup-go@v5
        with:
          go-version-file: controlplane/go.mod
          cache: true
          cache-dependency-path: controlplane/go.sum
      - name: Build dlh CLI
        run: |
          cd controlplane
          go build -o /usr/local/bin/dlh ./cmd/dlh

      - uses: ./.github/actions/dlh-run
        with:
          endpoint: ${{ secrets.DLH_ENDPOINT }}
          scenario: mysql-pod-delete
          target: ''  # framework cluster
          params: |
            vus=5
            load_duration=30s
            chaos_duration=15s
          wait: 'true'
```

- [ ] **Step 2: Lint:**

```bash
cd /Users/allen/repo/dlh-test-fw-plan18
python3 -c "import yaml; list(yaml.safe_load_all(open('.github/workflows/example-release-gate.yml')))" && echo "valid YAML"
```

- [ ] **Step 3: Commit.**

```bash
git add .github/workflows/example-release-gate.yml
git commit -m "feat(ci): example release-gate workflow using dlh-run composite

Demonstrates: id-token: write permission, build dlh CLI from the
controlplane source tree (no prebuilt binary distribution required),
call the composite with a multi-line params list."
```

---

## Task 12: CI integration operator doc

**Files:**
- Create: `docs/operations/ci-integration.md`

- [ ] **Step 1: Write the doc.**

```markdown
# CI integration runbook (Plan 18)

How to wire a GitHub Actions workflow to a dlh-controlplane deployment.

## One-time setup (per controlplane environment)

1. **Audience choice.** Pick a stable string (default: `dlh-controlplane`)
   and configure the controlplane via `DLH_CI_AUDIENCE`. Document this
   for your CI users.

2. **Trusted issuers.** Set `DLH_CI_TRUSTED_ISSUERS` to a CSV of OIDC
   issuers you accept. For GitHub.com:
   `https://token.actions.githubusercontent.com` (this is also the
   default; only override if you need additional CI providers).

3. **RBAC for CI principals.** GH Actions OIDC tokens carry a
   `repository` claim. The controlplane Exchanger maps that into a
   group named `ci-repo:<owner>/<repo>`. Add an entry to the dlh-roles
   ConfigMap (synced via Argo CD):

   ```yaml
   data:
     bindings.yaml: |
       runner: ["ci-repo:my-org/my-repo"]
   ```

   Without a binding, exchanged tokens become viewer-only.

## Workflow integration (per CI repository)

1. Copy `.github/actions/dlh-run/action.yml` into your repo at the same
   path (this is a composite action — it cannot be referenced as a
   third-party action across repositories without publishing first).
2. Build the dlh CLI in a setup step so it's in PATH (see the example
   workflow at `.github/workflows/example-release-gate.yml`).
3. In the job's `permissions:` block, set `id-token: write`. Without it,
   GH Actions does not mint an OIDC token and the action fails fast.
4. Set `DLH_ENDPOINT` as a repo or org secret pointing at the
   controlplane base URL (https://...).

## Troubleshooting

- **`exchange failed: token issuer not in trusted allowlist`**: the
  `DLH_CI_TRUSTED_ISSUERS` env on the controlplane doesn't list GH
  Actions. Fix on the controlplane side via PR + Argo CD sync.

- **`forbidden`** on submit: the exchanged session has viewer-only role.
  Add the repo to a runner binding in the dlh-roles ConfigMap.

- **Exchange returns 401 with no detail**: check the controlplane logs
  for the underlying go-oidc error. Most often: audience mismatch.
  Confirm the workflow's `audience:` input matches the controlplane's
  `DLH_CI_AUDIENCE`.

- **GH Actions OIDC discovery doesn't include device endpoints**: this
  is normal — GH Actions OIDC is for CI-to-service, not interactive
  login. `dlh login` targets a separate (interactive) IdP, configured
  via `DLH_OIDC_ISSUER_URL`.
```

- [ ] **Step 2: Commit.**

```bash
git add docs/operations/ci-integration.md
git commit -m "docs(operations): CI integration runbook for Plan 18

Audience + trusted issuer setup, RBAC for ci-repo:* groups, common
exchange-failure troubleshooting."
```

---

## Task 13: Smoke `dlh login --help` + composite action YAML

**Files:** None modified. Validation only.

- [ ] **Step 1: Verify the CLI is rebuildable.**

```bash
cd /Users/allen/repo/dlh-test-fw-plan18/controlplane
make cli
./bin/dlh login --help
./bin/dlh --help
```

Expected: `login` subcommand listed; help text mentions device-code flow.

- [ ] **Step 2: Confirm AuthInfo handler returns expected JSON shape.**

(No live cluster — just check the unit tests run clean.)

```bash
go test ./internal/api/... -v -run "GetAuthInfo|OidcExchange"
```

Add a tiny handler test if not present yet. Append to `internal/api/handlers_test.go`:

```go
func TestGetAuthInfo_PopulatesFromDeps(t *testing.T) {
	deps := &Deps{AuthInfo: AuthInfoConfig{
		OIDCIssuer:   "https://issuer.example.com",
		OIDCClientID: "client-x",
		CIAudience:   "aud-y",
		AuthDisabled: false,
	}}
	h := &Handlers{deps: deps}
	resp, err := h.GetAuthInfo(context.Background(), gen.GetAuthInfoRequestObject{})
	if err != nil {
		t.Fatalf("GetAuthInfo: %v", err)
	}
	out, ok := resp.(gen.GetAuthInfo200JSONResponse)
	if !ok {
		t.Fatalf("response type: %T", resp)
	}
	if out.OidcIssuer != "https://issuer.example.com" || out.OidcClientId != "client-x" {
		t.Errorf("info: %+v", out)
	}
}
```

```bash
go test ./internal/api/... -v -run "TestGetAuthInfo"
```

Expected: PASS.

- [ ] **Step 3: Commit.**

```bash
git add controlplane/internal/api/handlers_test.go
git commit -m "test(controlplane/api): TestGetAuthInfo_PopulatesFromDeps"
```

**Section B complete.** CLI + composite action + CI doc + tests are in place.

---

# Section C — Convert kafka + doris scenarios to chart-managed WTs (Tasks 14-17)

## Task 14: Inspect mysql-pod-delete WT template (reference for the conversion pattern)

**Files:** None modified.

- [ ] **Step 1: Read the existing chart-managed mysql-pod-delete WorkflowTemplate.**

```bash
cd /Users/allen/repo/dlh-test-fw-plan18
cat helm/dlh-test-fw/files/workflowtemplates/scenario/mysql-pod-delete.yaml
```

Note its structure:
- `apiVersion: argoproj.io/v1alpha1, kind: WorkflowTemplate`
- `metadata.name: mysql-pod-delete`, `metadata.namespace: {{`{{ .Values.namespace }}`}}` (or hardcoded — confirm via the actual file)
- `spec.arguments.parameters` includes `target_id` with default `""` (Plan 17 fix)
- `spec.templates[].main.steps` propagates `{{`{{workflow.parameters.target_id}}`}}` to the chaos step

This is the pattern. Tasks 15-17 convert the standalone Workflow YAMLs at `scenarios/kafka-broker-partition.yaml` and `scenarios/doris-be-network-loss.yaml` into the same WorkflowTemplate shape.

- [ ] **Step 2: Inspect the standalone Workflow sources.**

```bash
cat scenarios/kafka-broker-partition.yaml
echo "---"
cat scenarios/doris-be-network-loss.yaml
```

Note the differences from the chart-managed mysql WT:
- `kind: Workflow` (not WorkflowTemplate)
- `metadata.generateName:` (not name)
- `metadata.namespace:` is hardcoded
- All inline — no separate `arguments.parameters` block at the top; defaults live inside each step's argument lists

The conversion: lift the steps into a `main` template; promote default-bearing parameters into `spec.arguments.parameters` (so they can be overridden via `dlh run --param`); add `target_id` parameter + propagation.

---

## Task 15: Convert kafka-broker-partition to chart-managed WT

**Files:**
- Create: `helm/dlh-test-fw/files/workflowtemplates/scenario/kafka-broker-partition.yaml`

- [ ] **Step 1: Write the new WorkflowTemplate.**

Open the existing `scenarios/kafka-broker-partition.yaml` to copy field values. Then write the chart-managed equivalent:

```yaml
# Plan 18 (2026-05-23): converted from standalone Workflow to chart-managed
# WorkflowTemplate so `dlh run kafka-broker-partition --target X` works
# through the controlplane API. The standalone scenarios/*.yaml will be
# deleted in Task 19.
apiVersion: argoproj.io/v1alpha1
kind: WorkflowTemplate
metadata:
  name: kafka-broker-partition
  namespace: {{`{{ .Values.namespace }}`}}
  labels:
    app.kubernetes.io/part-of: dlh-test-fw
    dlh.category: scenario
spec:
  serviceAccountName: argo-workflow
  priority: 100
  synchronization:
    semaphores:
    - configMapKeyRef:
        name: dlh-scenario-locks
        key: kafka
  entrypoint: main
  arguments:
    parameters:
    # ===== scenario identity =====
    - { name: scenario_label,    value: kafka-broker-partition }
    # ===== SLO =====
    - { name: slo_name,          value: broker-partition }
    - name: slo_vars
      value: |
        LATENCY_METRIC=k6_dlh_kafka_produce_duration_seconds
        OPS_COUNTER=k6_dlh_kafka_messages_produced_total_total
        ERR_KIND_PATTERN=kafka.*
        P95_LT=2.0
        ERR_LT=0.50
    # ===== load shape =====
    - { name: vus,               value: "10" }
    - { name: load_duration,     value: 180s }
    # ===== chaos shape =====
    - { name: chaos_start_after, value: 30s }
    - { name: chaos_duration,    value: 60s }
    - { name: broker_id,         value: "0" }
    # ===== workload (kafka-specific) =====
    - { name: kafka_bootstrap,   value: "kafka.kafka-sys.svc.cluster.local:9092" }
    - { name: kafka_topic,       value: "dlh-load" }
    - { name: kafka_op,          value: "produce" }
    - { name: kafka_message_size, value: "256" }
    # ===== remote target (Plan 17 Phase D / Plan 18) =====
    # Empty string means "local framework cluster"; set to a registered target ID for remote chaos.
    - { name: target_id, value: "" }

  templates:
  - name: main
    steps:
    - - name: write-slo
        templateRef: { name: util-write-slo, template: main }
        arguments:
          parameters:
          - { name: slo_name, value: "{{`{{workflow.parameters.slo_name}}`}}" }
          - { name: slo_vars, value: "{{`{{workflow.parameters.slo_vars}}`}}" }
    - - name: chaos
        templateRef: { name: chaos-kafka-broker-partition, template: main }
        continueOn: { failed: true }
        arguments:
          parameters:
          - { name: kafka_namespace, value: "kafka-sys" }
          - { name: broker_id,       value: "{{`{{workflow.parameters.broker_id}}`}}" }
          - { name: duration,        value: "{{`{{workflow.parameters.chaos_duration}}`}}" }
          - { name: target_id,       value: "{{`{{workflow.parameters.target_id}}`}}" }
      - name: load
        templateRef: { name: load-k6-run, template: main }
        arguments:
          parameters:
          - { name: script_path,    value: "/scripts/runners/kafka.js" }
          - { name: vus,            value: "{{`{{workflow.parameters.vus}}`}}" }
          - { name: duration,       value: "{{`{{workflow.parameters.load_duration}}`}}" }
          - { name: scenario_label, value: "{{`{{workflow.parameters.scenario_label}}`}}" }
          - name: env_map
            value: |
              KAFKA_BOOTSTRAP={{`{{workflow.parameters.kafka_bootstrap}}`}}
              KAFKA_TOPIC={{`{{workflow.parameters.kafka_topic}}`}}
              KAFKA_OP={{`{{workflow.parameters.kafka_op}}`}}
              KAFKA_MESSAGE_SIZE={{`{{workflow.parameters.kafka_message_size}}`}}
    - - name: verdict
        templateRef: { name: verdict-slo-eval, template: main }
        arguments:
          parameters:
          - { name: scenario_label, value: "{{`{{workflow.parameters.scenario_label}}`}}" }
          - { name: slo_name,       value: "{{`{{workflow.parameters.slo_name}}`}}" }
```

**Note on Helm escaping:** the `{{`{{ ... }}`}}` pattern is needed because this file is rendered through `helm template` first; Argo's own `{{ }}` syntax must be escaped to survive Helm processing. Match the escaping pattern used in `helm/dlh-test-fw/files/workflowtemplates/scenario/mysql-pod-delete.yaml`.

If the existing mysql WT uses a different escaping pattern (e.g., the chart's `tpl` function with a different delimiter), match that exact pattern. Compare with mysql-pod-delete.yaml as you write.

- [ ] **Step 2: Render + lint.**

```bash
cd /Users/allen/repo/dlh-test-fw-plan18
helm dependency update helm/dlh-test-fw 2>&1 | tail -3
helm lint helm/dlh-test-fw 2>&1 | tail -5
helm template dlh helm/dlh-test-fw > /tmp/rendered-plan18-kafka.yaml
grep -A 5 'name: kafka-broker-partition' /tmp/rendered-plan18-kafka.yaml | head -15
grep -c 'target_id' /tmp/rendered-plan18-kafka.yaml
```

Expected: helm lint passes; the WorkflowTemplate renders; `target_id` appears multiple times in the rendered output (workflow params + chaos step arg).

- [ ] **Step 3: Commit.**

```bash
git add helm/dlh-test-fw/files/workflowtemplates/scenario/kafka-broker-partition.yaml
git commit -m "feat(workflowtemplates/scenario): chart-managed kafka-broker-partition WT

Converted from standalone scenarios/kafka-broker-partition.yaml Workflow
to a chart-managed WorkflowTemplate so dlh run can target it via
the controlplane API. target_id parameter + propagation included
per Plan 17 FINDING #10."
```

---

## Task 16: Convert doris-be-network-loss to chart-managed WT

**Files:**
- Create: `helm/dlh-test-fw/files/workflowtemplates/scenario/doris-be-network-loss.yaml`

- [ ] **Step 1: Inspect the standalone Doris scenario.**

```bash
cd /Users/allen/repo/dlh-test-fw-plan18
cat scenarios/doris-be-network-loss.yaml
```

Note its parameter set (loss_percent, target_namespace, target_pod_selector, etc.) and the fixture step that loads Doris data.

- [ ] **Step 2: Write the chart-managed WorkflowTemplate.**

```yaml
# Plan 18 (2026-05-23): converted from standalone Workflow to chart-managed
# WorkflowTemplate so `dlh run doris-be-network-loss --target X` works
# through the controlplane API.
apiVersion: argoproj.io/v1alpha1
kind: WorkflowTemplate
metadata:
  name: doris-be-network-loss
  namespace: {{`{{ .Values.namespace }}`}}
  labels:
    app.kubernetes.io/part-of: dlh-test-fw
    dlh.category: scenario
spec:
  serviceAccountName: argo-workflow
  priority: 100
  synchronization:
    semaphores:
    - configMapKeyRef:
        name: dlh-scenario-locks
        key: doris
  entrypoint: main
  arguments:
    parameters:
    # ===== scenario identity =====
    - { name: scenario_label, value: doris-be-network-loss }
    # ===== SLO =====
    - { name: slo_name,       value: be-network-loss }
    - name: slo_vars
      value: |
        LATENCY_METRIC=k6_dlh_doris_stream_load_duration_seconds
        OPS_COUNTER=k6_dlh_doris_stream_load_total_total
        ERR_KIND_PATTERN=doris.*
        P95_LT=5.0
        ERR_LT=0.50
    # ===== load shape =====
    - { name: vus,               value: "5" }
    - { name: load_duration,     value: 180s }
    # ===== chaos shape =====
    - { name: chaos_start_after, value: 30s }
    - { name: chaos_duration,    value: 60s }
    - { name: loss_percent,      value: "50" }
    # ===== workload (doris-specific) =====
    - { name: doris_fe_host, value: "doris-fe-0.doris-fe.doris-sys.svc.cluster.local:8030" }
    # ===== remote target (Plan 17 Phase D / Plan 18) =====
    - { name: target_id, value: "" }

  templates:
  - name: main
    steps:
    - - name: write-slo
        templateRef: { name: util-write-slo, template: main }
        arguments:
          parameters:
          - { name: slo_name, value: "{{`{{workflow.parameters.slo_name}}`}}" }
          - { name: slo_vars, value: "{{`{{workflow.parameters.slo_vars}}`}}" }
    - - name: fixture-load
        templateRef: { name: fixture-minio-load-doris, template: main }
        arguments:
          parameters:
          - { name: uri,                            value: "s3://fixtures/doris-rows.csv" }
          - { name: fe_host,                        value: "{{`{{workflow.parameters.doris_fe_host}}`}}" }
          - { name: stream_load_credentials_secret, value: "doris-creds" }
    - - name: chaos
        templateRef: { name: chaos-network-loss, template: main }
        continueOn: { failed: true }
        arguments:
          parameters:
          - { name: target_namespace,    value: "doris-sys" }
          - { name: target_pod_selector, value: "app=doris-be" }
          - { name: loss_percent,        value: "{{`{{workflow.parameters.loss_percent}}`}}" }
          - { name: duration,            value: "{{`{{workflow.parameters.chaos_duration}}`}}" }
          - { name: target_id,           value: "{{`{{workflow.parameters.target_id}}`}}" }
      - name: load
        templateRef: { name: load-k6-run, template: main }
        arguments:
          parameters:
          - { name: script_path,    value: "/scripts/runners/doris.js" }
          - { name: vus,            value: "{{`{{workflow.parameters.vus}}`}}" }
          - { name: duration,       value: "{{`{{workflow.parameters.load_duration}}`}}" }
          - { name: scenario_label, value: "{{`{{workflow.parameters.scenario_label}}`}}" }
          - name: env_map
            value: |
              DORIS_FE_HOST={{`{{workflow.parameters.doris_fe_host}}`}}
    - - name: verdict
        templateRef: { name: verdict-slo-eval, template: main }
        arguments:
          parameters:
          - { name: scenario_label, value: "{{`{{workflow.parameters.scenario_label}}`}}" }
          - { name: slo_name,       value: "{{`{{workflow.parameters.slo_name}}`}}" }
```

- [ ] **Step 3: Render + lint.**

```bash
helm lint helm/dlh-test-fw 2>&1 | tail -5
helm template dlh helm/dlh-test-fw > /tmp/rendered-plan18-doris.yaml
grep -A 5 'name: doris-be-network-loss' /tmp/rendered-plan18-doris.yaml | head -15
```

Expected: clean lint; the WorkflowTemplate renders.

- [ ] **Step 4: Commit.**

```bash
git add helm/dlh-test-fw/files/workflowtemplates/scenario/doris-be-network-loss.yaml
git commit -m "feat(workflowtemplates/scenario): chart-managed doris-be-network-loss WT

Converted from standalone scenarios/doris-be-network-loss.yaml Workflow.
target_id parameter + propagation per Plan 17 FINDING #10."
```

---

## Task 17: Delete the standalone Workflow YAMLs

**Files:**
- Delete: `scenarios/mysql-pod-delete.yaml`
- Delete: `scenarios/kafka-broker-partition.yaml`
- Delete: `scenarios/doris-be-network-loss.yaml`

- [ ] **Step 1: Confirm chart-managed equivalents exist.**

```bash
cd /Users/allen/repo/dlh-test-fw-plan18
ls helm/dlh-test-fw/files/workflowtemplates/scenario/
```

Expected: all three (`mysql-pod-delete.yaml`, `kafka-broker-partition.yaml`, `doris-be-network-loss.yaml`).

- [ ] **Step 2: Delete the standalone files.**

```bash
git rm scenarios/mysql-pod-delete.yaml scenarios/kafka-broker-partition.yaml scenarios/doris-be-network-loss.yaml
```

If `scenarios/README.md` exists and references these files, update it (or delete the directory if it's now empty):

```bash
ls scenarios/
```

If only `README.md` remains, update it to point at `helm/dlh-test-fw/files/workflowtemplates/scenario/` instead.

- [ ] **Step 3: Confirm CI's kubeconform job doesn't break** (it scans `scenarios/*.yaml`).

```bash
grep -A 5 "Validate scenarios/" .github/workflows/ci.yml
```

If a step references `scenarios/*.yaml`, it'll now fail because no matching files exist. Either delete the step or modify the find expression to also skip the (now-empty) scenarios/ dir.

If the existing CI step has `scenarios/*.yaml` hardcoded, change it. Use Edit:

old_string:
```yaml
            scenarios/*.yaml
```

new_string:
```yaml
            $(find scenarios -maxdepth 1 -name '*.yaml' 2>/dev/null | tr '\n' ' ')
```

Wait, kubeconform with no input files exits with error. Better: condition the step on file presence:

```yaml
      - name: Validate scenarios/*.yaml
        run: |
          if compgen -G "scenarios/*.yaml" > /dev/null; then
            kubeconform -skip CustomResourceDefinition -strict -summary \
              -schema-location default \
              -schema-location 'https://raw.githubusercontent.com/datreeio/CRDs-catalog/main/{{.Group}}/{{.ResourceKind}}_{{.ResourceAPIVersion}}.json' \
              scenarios/*.yaml
          else
            echo "No standalone scenarios/ files — skipping (Plan 18: scenarios moved into chart)"
          fi
```

Use Edit to replace the existing scenario validation step with the conditional version.

- [ ] **Step 4: Verify the render still includes all 3 scenarios.**

```bash
helm template dlh helm/dlh-test-fw | grep -E "^  name: (mysql-pod-delete|kafka-broker-partition|doris-be-network-loss)$"
```

Expected: 3 matches.

- [ ] **Step 5: Commit.**

```bash
git add -A scenarios/ .github/workflows/ci.yml
git status
git commit -m "refactor(scenarios): delete standalone Workflow YAMLs (now chart-managed)

All three scenarios live in helm/dlh-test-fw/files/workflowtemplates/scenario/
as WorkflowTemplates. CI scenario-validation step skips when scenarios/*.yaml
is empty."
```

**Section C complete.** All scenarios are chart-managed; target_id propagation is in place; scenarios/ directory is empty (or near-empty).

---

# Section D — Delete shell scripts + final docs (Tasks 18-21)

## Task 18: Delete the shell scripts

**Files:**
- Delete: `scripts/run-scenario.sh`, `scripts/platform-up.sh`, `scripts/platform-down.sh`, `scripts/platform-verify.sh`, `scripts/verify-templates.sh`

- [ ] **Step 1: Confirm what's there + that minikube-up.sh is the only keeper.**

```bash
cd /Users/allen/repo/dlh-test-fw-plan18
ls scripts/
```

Expected to delete: 5 scripts. Keep: `scripts/minikube-up.sh`.

- [ ] **Step 2: Delete.**

```bash
git rm scripts/run-scenario.sh scripts/platform-up.sh scripts/platform-down.sh scripts/platform-verify.sh scripts/verify-templates.sh
ls scripts/
```

Expected: only `minikube-up.sh` remains.

- [ ] **Step 3: Confirm shellcheck CI doesn't break.**

```bash
grep -nA 5 "shellcheck" .github/workflows/ci.yml | head -10
```

If the CI step has `scripts/` as the scandir (which catches `scripts/*.sh`), it works fine with just `minikube-up.sh`. If the step references specific deleted scripts by name, update it.

If the existing step looks like `scandir: scripts`, no change needed. shellcheck will validate the remaining `minikube-up.sh`.

- [ ] **Step 4: Helm chart check** (in case anything still references the deleted scripts via Helm — unlikely, but worth a grep).

```bash
grep -rn "run-scenario\|platform-up\|platform-down\|platform-verify\|verify-templates" helm/ controlplane/ 2>&1 | head
```

Expected: only stale doc/comment references, if any. Note them for Task 19.

- [ ] **Step 5: Commit.**

```bash
git status
git commit -m "refactor(scripts): delete platform-up/down/verify + run-scenario + verify-templates

Plan 14 marked them LOCAL-DEV ONLY and Plan 16 made run-scenario.sh a
deprecation shim. Plan 18 completes the removal — the controlplane API
+ dlh CLI cover every use case the scripts addressed. scripts/minikube-up.sh
is retained for local-dev convenience."
```

---

## Task 19: Sweep stale references + update CLAUDE.md

**Files:**
- Modify: `CLAUDE.md`
- Possibly: `README.md`, `docs/operations/bootstrap-via-argocd.md`, others surfaced by grep.

- [ ] **Step 1: Find all stale references to the deleted scripts.**

```bash
cd /Users/allen/repo/dlh-test-fw-plan18
grep -rln "run-scenario\.sh\|platform-up\.sh\|platform-down\.sh\|platform-verify\.sh\|verify-templates\.sh" . \
  --include="*.md" --include="*.yaml" --include="*.yml" --include="*.go" \
  2>&1 | grep -v "^./.git/" | head
```

Each match is a candidate for an update.

- [ ] **Step 2: Update CLAUDE.md.**

Open `CLAUDE.md`. Find the "Operational model: GitOps vs local-dev" section. It currently lists the deleted scripts as `LOCAL-DEV ONLY` paths.

Use Edit to replace the references in CLAUDE.md so the list now reads:

```markdown
### Local-dev (laptop minikube)

Use the `dlh` CLI against a local minikube + chart deployment:

- `scripts/minikube-up.sh` — destructive cluster reset (only remaining script)
- `cd controlplane && make ui-build && make build` — build the binary
- `helm upgrade --install dlh helm/dlh-test-fw -f helm/dlh-test-fw/values.yaml -f helm/dlh-test-fw/values-minikube.yaml -n dlh-test-fw --create-namespace` — install the chart (replaces `platform-up.sh`)
- `helm uninstall dlh -n dlh-test-fw` — teardown (replaces `platform-down.sh`)
- `helm test dlh -n dlh-test-fw` — verify (replaces `platform-verify.sh`)
- `kubectl -n dlh-test-fw get workflowtemplates` — list scenarios (replaces `verify-templates.sh`)
- `dlh run mysql-pod-delete --wait` — submit a scenario (replaces `run-scenario.sh`)
```

Find the exact text in the existing CLAUDE.md and adapt — keep the surrounding "When to use" guidance and Production / GitOps Argo CD section unchanged.

- [ ] **Step 3: Update other references surfaced by Step 1.**

For each file from the grep:
- If it's a markdown doc, replace the deleted-script reference with the equivalent `dlh ...` / `helm ...` command.
- If it's a YAML in `helm/` or `argocd/`, check whether the reference is stale comment — update or remove the comment.

- [ ] **Step 4: Commit.**

```bash
git add -u
git status
git commit -m "docs: remove stale references to deleted shell scripts

Updates CLAUDE.md's operational-model section + any other docs that
still pointed at run-scenario.sh / platform-*.sh / verify-templates.sh
to use 'dlh ...' or 'helm ...' equivalents."
```

---

## Task 20: Final docs — FINDINGS + README + CI integration link

**Files:**
- Modify: `docs/FINDINGS.md`
- Modify: `README.md`

- [ ] **Step 1: Append Plan 18 section to docs/FINDINGS.md.**

Use Edit. Find the last line of FINDINGS.md and append:

```markdown

---

## Plan 18 — controlplane Phase E (CI integration + cleanup) (2026-05-23)

### What landed

- `POST /api/oidc/exchange` + `GET /api/auth/info`: external OIDC token → short-lived (1h) HS256 controlplane session JWT.
- `controlplane/internal/auth/session.go` + `exchange.go` + `jwt_peek.go`.
- `dlh login` (RFC 8628 device-code flow) → `~/.config/dlh/token`.
- `.github/actions/dlh-run/action.yml` reusable composite + `example-release-gate.yml` workflow.
- `docs/operations/ci-integration.md` operator runbook.
- kafka + doris scenarios promoted from standalone Workflow YAMLs to chart-managed WorkflowTemplates under `helm/dlh-test-fw/files/workflowtemplates/scenario/`, completing the Plan 17 FINDING #10 target_id propagation sweep.
- Shell scripts deleted: `run-scenario.sh`, `platform-up.sh`, `platform-down.sh`, `platform-verify.sh`, `verify-templates.sh`. Only `scripts/minikube-up.sh` survives.
- `dlh-session-signing-key` Secret (helm-lookup-stable random key) added to the chart.

### Post-controlplane operational model

- **All scenario submission** flows through `POST /api/runs` (UI / `dlh run` / GH Actions composite). No `argo submit`, no `kubectl create -f scenarios/*.yaml`.
- **All scenario sources** live in `helm/dlh-test-fw/files/workflowtemplates/scenario/`. The standalone `scenarios/` directory is no longer used.
- **Local-dev** uses `minikube-up.sh` + `helm upgrade --install` + `dlh ...`. No more `platform-up.sh`.
- **CI** uses the composite action with `id-token: write` + an exchanged session token. No PATs.
- **Production** uses Argo CD to sync the chart + manifests + targets ConfigMap. No shell into the cluster.

### Operational pitfalls discovered

1. **Session JWT signing key must be 64+ chars when using HS256**. The chart's `randAlphaNum 64` produces a sufficient key; shorter keys still sign but reduce the brute-force margin. Don't override via values without keeping the length.

2. **`Exchanger.providers` cache is a memory-only map**. Restarting the controlplane re-fetches the JWKS for each known issuer; cold-start has a small (~100ms per issuer) latency penalty. Acceptable for v1; consider a TTL cache if issuer count grows.

3. **GH Actions OIDC `audience` is per-token-request**. Workflows MUST request the audience matching the controlplane's `DLH_CI_AUDIENCE`. If they don't, the Exchanger rejects with `verify: oidc: expected audience`. The composite action sets it explicitly via the `audience:` input.

4. **Device-code flow is IdP-specific**. `dlh login` works when the IdP exposes `device_authorization_endpoint` in its `.well-known/openid-configuration`. GitHub.com OIDC for personal accounts does NOT (it's CI-only). Most enterprise IdPs (Okta, Google Workspace, Dex with PKCE) do.

5. **GH Actions OIDC tokens last 5 minutes**. The composite action mints + exchanges + uses in one job — no caching between jobs. If a workflow has multiple `dlh-run` steps, each step mints + exchanges its own.

6. **Stale `scenarios/` directory references in docs**. After Plan 18, several docs still pointed at `scenarios/*.yaml` paths. Step-by-step sweep via `grep -rn` was needed; future plans should check `grep -rn 'scripts/' --include='*.md'` and `grep -rn 'scenarios/' --include='*.md'` before claiming cleanup is done.

### Carry-forward for future plans

- The session JWT issuer (`iss` claim) is hardcoded `dlh-controlplane`. If multiple controlplane instances coexist, add a distinguishing field (or accept any issuer matching a configured allowlist).
- Notification hooks (Slack, email) for run completion remain unimplemented (interface stub in Plan 15 design).
- The controlplane UI doesn't yet show a "logged in as ..." indicator. Easy to add: GET /api/auth/info during boot, then a small badge in the nav.
- `dlh login` doesn't refresh expired tokens — re-running `dlh login` is required after 1h. A refresh-token flow could close this.
```

- [ ] **Step 2: Append Plan 18 row to README.md.**

```markdown
| Plan 18 | `XXXXXXX` | dlh-controlplane Phase E (CI integration + cleanup) — POST /api/oidc/exchange + GET /api/auth/info; dlh login device-code; GH Actions composite + example release-gate workflow; kafka+doris promoted to chart-managed WTs; 5 shell scripts deleted |
```

Use Edit to append after the Plan 17 row.

- [ ] **Step 3: Commit.**

```bash
git add docs/FINDINGS.md README.md
git status
git commit -m "docs: Plan 18 — FINDINGS post-controlplane operational model + README row"
```

---

## Task 21: Final verification pass

**Files:** None modified.

- [ ] **Step 1: Go build + tests.**

```bash
cd /Users/allen/repo/dlh-test-fw-plan18/controlplane
go vet ./...
go test ./...
```

Expected: clean; all tests pass (Phase D total + the new auth/api tests).

- [ ] **Step 2: UI build.**

```bash
cd /Users/allen/repo/dlh-test-fw-plan18/controlplane/web
pnpm openapi-typescript ../api/openapi.yaml -o src/api/gen.ts
pnpm build 2>&1 | tail -5
```

Expected: clean build.

- [ ] **Step 3: CLI build.**

```bash
cd /Users/allen/repo/dlh-test-fw-plan18/controlplane
make cli
./bin/dlh --help | head -15
```

Expected: `dlh login`, `dlh run`, `dlh runs` all listed.

- [ ] **Step 4: Helm chart render + kubeconform.**

```bash
cd /Users/allen/repo/dlh-test-fw-plan18
helm dependency update helm/dlh-test-fw 2>&1 | tail -3
helm lint helm/dlh-test-fw 2>&1 | tail -3
helm template dlh helm/dlh-test-fw > /tmp/rendered-plan18-final.yaml
grep -c 'name: dlh-session-signing-key' /tmp/rendered-plan18-final.yaml
grep -cE "^  name: (mysql-pod-delete|kafka-broker-partition|doris-be-network-loss)$" /tmp/rendered-plan18-final.yaml
kubeconform -skip CustomResourceDefinition -strict -summary \
  -schema-location default \
  -schema-location 'https://raw.githubusercontent.com/datreeio/CRDs-catalog/main/{{.Group}}/{{.ResourceKind}}_{{.ResourceAPIVersion}}.json' \
  /tmp/rendered-plan18-final.yaml controlplane/deploy/role.yaml controlplane/deploy/service.yaml controlplane/deploy/serviceaccount.yaml controlplane/deploy/deployment.yaml controlplane/deploy/rolebinding.yaml controlplane/deploy/ingress.yaml controlplane/deploy/roles-configmap.yaml 2>&1 | tail -3
```

Expected: lint passes; session signing key Secret rendered; 3 scenario WTs present; kubeconform Invalid=0.

- [ ] **Step 5: shellcheck the remaining script.**

```bash
shellcheck -S error scripts/minikube-up.sh
```

Expected: no output (clean).

- [ ] **Step 6: Confirm script directory is exactly as expected.**

```bash
ls scripts/
```

Expected: only `minikube-up.sh`.

No commit. Gate before push.

---

## Task 22: Push, watch CI, merge to main

**Files:** Backfill README hash only.

- [ ] **Step 1: Push branch + verify CI.**

```bash
cd /Users/allen/repo/dlh-test-fw-plan18
git push -u origin feat/plan18-controlplane-ci-integration 2>&1 | tail -5
RUN_ID=$(gh run list --branch feat/plan18-controlplane-ci-integration --limit 1 --json databaseId -q '.[0].databaseId')
echo "Watching CI run $RUN_ID"
gh run watch "$RUN_ID" --interval 30 || true
gh run view "$RUN_ID" --json conclusion -q .conclusion
```

Expected: `success`.

If CI fails on the deleted-scenarios path (kubeconform job tries to validate `scenarios/*.yaml` and the glob returns nothing): the conditional from Task 17 Step 3 handles this. If it still fails, fix the CI step + commit + re-watch.

If CI fails on `shellcheck` (because the scandir is empty? unlikely — minikube-up.sh remains): inspect + fix.

- [ ] **Step 2: Merge to main with --no-ff.**

```bash
cd /Users/allen/repo/dlh-test-fw
git checkout main
git pull --ff-only origin main 2>&1 | tail -3
git merge --no-ff feat/plan18-controlplane-ci-integration -m "Merge feat/plan18-controlplane-ci-integration: Phase E (CI + cleanup)

Closes the controlplane migration: CI workflows can exchange GH Actions
OIDC for a short-lived controlplane session JWT and submit scenarios
via the reusable .github/actions/dlh-run composite. Engineers run
'dlh login' for an interactive device-code OIDC flow. kafka + doris
scenarios are promoted to chart-managed WorkflowTemplates with
target_id propagation (Plan 17 FINDING #10). Shell scripts deleted
except minikube-up.sh.

Plan 18 of dlh-test-fw. See:
- docs/superpowers/specs/2026-05-21-dlh-controlplane-design.md
- docs/superpowers/plans/2026-05-23-02-controlplane-ci-integration.md
- docs/operations/ci-integration.md"
```

Expected: clean merge.

- [ ] **Step 3: Backfill README hash.**

```bash
cd /Users/allen/repo/dlh-test-fw
MERGE_HASH=$(git log --first-parent --format=%h -1)
echo "Hash: $MERGE_HASH"
sed -i "" "s|| Plan 18 | \`XXXXXXX\`|| Plan 18 | \`$MERGE_HASH\`|" README.md
grep "^| Plan 18 " README.md
git add README.md
git commit -m "docs(readme): backfill Plan 18 merge hash"
```

- [ ] **Step 4: Push main + verify CI.**

```bash
git push origin main 2>&1 | tail -5
git status   # should report "up to date with origin/main"
sleep 10
RUN_MAIN=$(gh run list --branch main --limit 1 --json databaseId -q '.[0].databaseId')
gh run watch "$RUN_MAIN" --interval 30 || true
gh run view "$RUN_MAIN" --json conclusion -q .conclusion
```

Expected: `success`.

- [ ] **Step 5: Worktree cleanup.**

```bash
cd /Users/allen/repo/dlh-test-fw
git worktree remove /Users/allen/repo/dlh-test-fw-plan18
git branch -d feat/plan18-controlplane-ci-integration
git push origin --delete feat/plan18-controlplane-ci-integration
git worktree list
```

Expected: only the main worktree remains.

- [ ] **Step 6: Final state verification.**

```bash
git log --first-parent --oneline -5
ls scripts/
ls controlplane/cmd/dlh/
grep "^| Plan 18 " README.md
```

Expected: merge + backfill at the top of first-parent; `scripts/` contains only `minikube-up.sh`; `cmd/dlh/` contains `login.go` plus the existing files; README Plan 18 row has real hash.

---

## Done

Plan 18 closes Phase E. CI workflows can submit scenarios via OIDC exchange; engineers `dlh login` interactively; scenario sources are unified under the Helm chart; shell scripts are gone. The controlplane migration is complete — `dlh-test-fw` runs entirely through the API surface.

Phase F (CronWorkflow scheduling) remains optional per spec §12, but is out of scope for this plan.
