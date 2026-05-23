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

       data:
         bindings.yaml: |
           runner: ["ci-repo:my-org/my-repo"]

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
