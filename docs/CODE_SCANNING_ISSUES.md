# Code Scanning Issues (GitHub Security tab)

This document summarizes the issues reported under **Security → Code scanning** and their root causes or remediation.

## Summary

| Category | Severity | Count | Root cause / remediation |
|----------|----------|-------|---------------------------|
| **Vulnerabilities** | High | 1 alert (9 CVEs) | Go dependency `github.com/ollama/ollama@v0.13.0` (see below) |
| **Pinned-Dependencies** | Medium | Multiple | Workflows/actions or container image not pinned by hash/digest |
| **Token-Permissions** | High | 1 | Releaser job needs `contents: write` for releases (intentional) |
| **Branch-Protection** | High | 1 | Repo setting: enable branch protection for `main` in GitHub |
| **Code-Review** | High | 1 | Process: require PR reviews before merge |
| **Maintained** | High | 1 | Informational: repo created in last 90 days |
| **CII-Best-Practices** | Low | 1 | CII badge “InProgress” |

---

## 1. Vulnerabilities (VulnerabilitiesID) — High

**Message:** `9 existing vulnerabilities detected` with GO-2025-4251 (GHSA-f6mr-38g8-39rg), GO-2025-3548, GO-2025-3557, GO-2025-3558, GO-2025-3559, GO-2025-3582, GO-2025-3689, GO-2025-3695, GO-2025-3824.

**Root cause:** All are in the **Ollama** Go client dependency:

- **Module:** `github.com/ollama/ollama@v0.13.0`
- **Pulled in by:** `trpc.group/trpc-go/trpc-agent-go/model/ollama` and direct use in `pkg/expert/modelprovider/`.

**Remediation:**

- **Upgrade blocked:** Upgrading `github.com/ollama/ollama` to v0.17.x fails the build because `trpc.group/trpc-go/trpc-agent-go/model/ollama@v1.5.0` is incompatible with the ollama v0.17 API (type changes: `api.ToolCallFunctionArguments`, `*api.ToolPropertiesMap`). The vuln database reports "Fixed in: N/A" for all 9; once trpc-agent-go (or a fork) supports ollama v0.17+, upgrade and re-run `govulncheck ./...`.
- **Mitigation:** If Genie only talks to a trusted/local Ollama server, exposure may be limited; ensure the Ollama API is not exposed to untrusted networks.
- Run `govulncheck ./...` periodically and after dependency updates.

---

## 2. Pinned-Dependencies (PinnedDependenciesID) — Medium

**Messages:**

- `downloadThenRun not pinned by hash` — Releaser downloads `quill` via `curl` and runs it; no checksum verification.
- `containerImage not pinned by hash` — Releaser uses `goreleaser/goreleaser-cross:latest` (and optionally other images) without a digest.
- `GitHub-owned GitHubAction not pinned by hash` / `third-party GitHubAction not pinned by hash` — Some workflow steps use `@v3` or tag instead of a full commit SHA.

**Remediation:**

- **Container:** Use image digest, e.g.  
  `image: goreleaser/goreleaser-cross@sha256:<digest>`
- **Actions:** Pin all `uses:` to a full commit SHA (e.g. `actions/checkout@<sha> # v6.0.2`). Scorecard and StepSecurity can suggest exact pins.
- **downloadThenRun:** Pin the quill version (already done) and optionally add a SHA256 check before running the binary.

---

## 3. Token-Permissions (TokenPermissionsID) — High

**Message:** `topLevel 'contents' permission set to 'write'`.

**Root cause:** The **releaser** workflow needs to create releases and push artifacts. The job that runs GoReleaser has `permissions: contents: write` (and `id-token: write` for OIDC). Top-level permissions are already set to `contents: read`; only the release job has elevated permissions.

**Remediation:** This is **intentional** for the release job. No change required unless the release process is redesigned. Optionally add a short comment in the workflow above the job’s `permissions` explaining that `contents: write` is required for publishing releases.

---

## 4. Branch-Protection (BranchProtectionID) — High

**Message:** `branch protection not enabled for branch 'main'`.

**Root cause:** Repository settings, not code.

**Remediation:** In GitHub: **Settings → Branches → Add rule** for `main` (e.g. require PR reviews, status checks, no force push). This cannot be fixed by code changes.

---

## 5. Code-Review (CodeReviewID) — High

**Message:** `Found 0/2 approved changesets`.

**Root cause:** Process/policy: no PRs have been merged with the required number of approvals (e.g. 2).

**Remediation:** Enforce “Require pull request reviews before merging” in branch protection and merge PRs that meet the policy. Not a code change.

---

## 6. Maintained (MaintainedID) — High

**Message:** `project was created in last 90 days`.

**Root cause:** Informational; the repo is new.

**Remediation:** None required; the alert will clear with time or can be dismissed as informational.

---

## 7. CII-Best-Practices (CIIBestPracticesID) — Low

**Message:** CII badge “InProgress”.

**Root cause:** CII best practices badge is in progress, not yet passing.

**Remediation:** Work through the [CII Best Practices](https://bestpractices.coreinfrastructure.org/) questionnaire and satisfy the criteria; not a code-only change.

---

## Quick reference: what to fix in code

- **Pin container image** in `.github/workflows/releaser.yml` by digest.
- **Pin GitHub Actions** in all workflows (including `upload-sarif` in scorecard) to full commit SHAs.
- **Optional:** Add checksum verification for the quill download in the releaser.
- **Optional:** Upgrade `github.com/ollama/ollama` when fixes are available and re-run `govulncheck` and tests.

All other items are either repo settings, process, or informational.
