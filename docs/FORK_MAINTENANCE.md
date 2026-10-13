---
owner: @SivanCola
backup: @esengine
status: active
reviewed: 2026-10-13
---

# Maintaining the Reasonix fork

## Purpose

Keep a functioning Reasonix agent, preserve the fork's privacy changes, and incorporate useful upstream fixes with a small maintenance burden. Use one shared integration branch, focused patches, and reviewed upstream merges.

The standing agent instructions remain in [REASONIX.md](../REASONIX.md). This guide describes maintainer procedures; it does not configure GitHub permissions, workflows, or release infrastructure.

## Repository and branch setup

| Name | Role | Current target |
| --- | --- | --- |
| `origin` | The maintained fork; push changes here | `https://github.com/limijd/DeepSeek-Reasonix` |
| `upstream` | Fetch the original project | `https://github.com/esengine/DeepSeek-Reasonix` |
| `studio` | Shared fork integration branch | `origin/studio` |
| `upstream/studio` | Source for routine upstream updates | Verify this branch before each update |
| `fix/*`, `feature/*`, `sync/*` | Short branches for reviewed work | Start from `origin/studio` |

1. Add `upstream` only when that remote is absent. If it already exists, verify its URL. Run these setup commands deliberately; they change local Git configuration.

```bash
git remote -v
git remote add upstream https://github.com/esengine/DeepSeek-Reasonix.git
git config remote.pushDefault origin
```

2. MUST push fork work to `origin`. Use SSH or a trusted credential manager; never embed tokens in remote URLs or commit credentials.

3. SHOULD protect `studio` with pull requests and required checks. Disable force pushes and deletion. Do not require linear history if upstream merge commits are retained. Require independent approval when another maintainer is available.

[GitHub branch protection](https://docs.github.com/en/repositories/configuring-branches-and-merges-in-your-repository/managing-protected-branches/about-protected-branches) supports review, status checks, and restrictions on branch updates.

4. MUST migrate inherited `.github/CODEOWNERS` and document owner headers together to actual fork collaborators before relying on review routing. Upstream maintainers are not automatically maintainers of this fork.

5. MUST verify the upstream release line. `main-v2` and `studio` are different lines; do not substitute one for the other during routine synchronization.

## Preserve the fork contract

Treat these behaviors as requirements when reviewing upstream updates, dependency changes, or refactoring.

| Requirement | Review boundary |
| --- | --- |
| Built-in telemetry, accounts, cloud relay, updates, feedback/crash uploads, and balance polling stay disabled | Service owners and startup paths, including old settings and endpoint overrides |
| `reasonix.io`, its subdomains, and `api.typesafe.ai` stay blocked in shared network paths | `internal/base/netpolicy`, netclient, redirects, proxies, and Electron sessions |
| Local model SDKs do not automatically download models or send telemetry | `internal/model/laya` and subprocess environment |
| Ordinary models, tools, proxies, Git, and authorized extensions keep working | Model discovery, tool execution, MCP/LSP, hooks, and desktop navigation |
| Existing security fixes remain effective | Local path grants, HTTPS proxy authentication, install-source SSRF, egress configuration, and consent scope |

An upstream merge MUST NOT silently restore a background service. Any proposed exception requires the fork owner's explicit approval, identifying its trigger, destination, payload, credentials, and user control.

Permission to run an extension does not prove its code is safe. Keep the documented distinction between disabled built-in background services and arbitrary third-party processes, which can still make network requests.

The current implementation and evidence are in [the remediation record](audits/security-remediation-2026-10-12.md). Preserve behavioral tests when upstream adopts an equivalent fix; remove duplicate implementation only after checking the same boundary.

## Keep patches small

1. SHOULD give each patch one purpose. Separate fixes, dependency upgrades, branding, and feature changes so each can be reviewed and reverted.

2. SHOULD reuse upstream interfaces and keep policy enforcement at its owning layer. Avoid widespread rewrites or another global restriction that breaks ordinary agent functionality.

3. MUST record why a fork-only patch exists and the regression test that protects it. For backports, record the original upstream commit, such as with `git cherry-pick -x` after review.

4. MAY contribute generic fixes upstream when authorized. Keep fork privacy decisions local when the projects intentionally have different policies.

5. MUST preserve published `studio` history. Rebase unpublished feature work if useful; do not rebase and force-push the shared fork branch.

## Review and integrate upstream changes

SHOULD review upstream releases regularly, for example weekly, and triage security fixes promptly. Fetching is separate from accepting an update. Use [GitHub's fork synchronization model](https://docs.github.com/en/pull-requests/how-tos/work-with-forks/syncing-a-fork) through a review branch.

1. Start with a clean worktree. Commit or save your own unfinished work before proceeding. Verify that `git status --short` is empty.

```bash
git status --short
git fetch origin studio
git switch studio
git merge --ff-only origin/studio
git fetch --no-tags upstream studio
```

2. Pin the candidate to a commit so the review does not change when upstream advances. Replace `YYYY-MM-DD` in the branch name with the update date.

```bash
upstream_sha=$(git rev-parse upstream/studio)
base_sha=$(git merge-base HEAD "$upstream_sha")
git log --oneline "$base_sha..$upstream_sha"
git diff --stat "$base_sha" "$upstream_sha"
```

If `git merge-base --is-ancestor "$upstream_sha" HEAD` succeeds, that history is already integrated; stop instead of creating an empty sync PR. Otherwise, continue:

```bash
git switch -c sync/upstream-YYYY-MM-DD
git merge --no-ff --no-commit "$upstream_sha"
```

`--no-ff --no-commit` stops before creating a merge commit even when a fast-forward would otherwise be possible. [Git merge documentation](https://git-scm.com/docs/git-merge).

3. Review the incoming commits and resulting tree, including files that merge cleanly. Pay particular attention to startup, network clients, service owners, configuration migration, extension launchers, dependencies, packaging, and `.github/workflows`.

4. Resolve conflicts by preserving intended behavior. MUST NOT accept all upstream or all fork versions blindly. Check any reused conflict resolution before staging it. If the update is unsuitable, use `git merge --abort`.

Stage each reviewed resolution with `git add -- path/to/file`, replacing the path with the actual file. Do not stage unrelated changes.

5. Run the applicable checks below. Inspect the staged result with `git diff --cached`, then commit the merge and push the review branch:

```bash
git diff --cached --check
git diff --cached --stat
git commit
git push -u origin HEAD
```

6. Open a PR targeting `studio`. Record the upstream SHA, fork patches affected, dependency/workflow changes, test results, and remaining failures. Review the exact commit that will be merged.

If `studio` advances, update the candidate and rerun affected checks against the proposed merge result. Passing checks on an older branch state do not establish that the new combination works.

7. MUST preserve upstream ancestry when accepting synchronization PRs: use a merge commit, not squash or rebase merging. Squashing an upstream synchronization makes later updates harder to reconcile.

For an urgent isolated fix, MAY cherry-pick the reviewed commit on a separate branch. Track the backport and reconcile it during the next full upstream merge.

## Verification before merging

Use the toolchain and lint version declared in `go.mod` and `.golangci-version`. Format only changed Go files. Keep the existing lint budgets and sensitive-path coverage floors.

Changes under sensitive paths listed in `REASONIX.md` MUST receive correctness and security review. Apply the same review to fork network policy and background service owners.

For Go or upstream synchronization changes, run:

```bash
make check
go vet ./...
make lint
make test
make coverage-gate
make build
```

`make test` covers the root, `desktop`, and `sdk/go` modules. Root `go test ./...` alone does not cover those nested modules. Run the relevant race tests and supported OS checks when concurrency or platform code changes.

Run the fork privacy regressions explicitly on Linux; the current integration helper uses a Unix shell:

```bash
go test ./tools/securitycheck ./internal/base/netpolicy ./internal/safety/egress
go test ./internal/model/laya ./internal/platform/environment
node --test desktop/electron/test/unit.mjs
```

For Studio or frontend changes, also run the relevant frontend tests and build:

```bash
(cd desktop/frontend-next && pnpm test && pnpm build)
```

Install/build steps can download dependencies or execute scripts. Run them in a prepared development or CI environment with reviewed dependencies; runtime privacy restrictions are not build-system isolation.

Network changes SHOULD include a fresh-state startup observation with dummy credentials, plus tests for old settings, endpoint overrides, redirects, and subprocess behavior. A short idle trace cannot prove that all future or third-party traffic is absent.

MUST record failures accurately. The [existing validation record](audits/security-audit-2026-10-12/clean-fork-validation.json) contains known failures and scope limits. Compare both revisions in the same environment before classifying a failure as inherited; do not skip tests or weaken gates to get green results.

## Dependencies and CI

1. MUST review new or upgraded dependencies at their locked versions. Check direct and transitive packages, source ownership, install scripts, `init` behavior, network activity, credential access, subprocess execution, and native/unsafe code.

2. MUST preserve Go checksums and frontend lockfiles. Also review Python SDKs and system helpers outside those lockfiles. A checksum proves consistency with a recorded artifact, not that its content is safe.

3. SHOULD use vulnerability scanning and release advisories as inputs to review. Popularity and the absence of reported CVEs are not a substitute for checking what an executable component can access.

4. MUST review inherited workflows before enabling them or adding secrets. Retain useful CI checks; disable or adapt jobs whose services, publishing destinations, or credentials belong to upstream.

| Existing workflow | Fork decision needed |
| --- | --- |
| `.github/workflows/ci.yml`, `studio.yml` | Keep useful test gates; verify fork branch filters, required job names, and privacy regression coverage |
| `.github/workflows/deploy-web-studio.yml` | Cloud deployment and upstream remote-home configuration; disable unless intentionally building a hosted fork service |
| `.github/workflows/issue-auto-label.yml` | Sends issue content to DeepSeek with a configured key; use only after explicit review |
| `.github/workflows/release-studio.yml` | Review signing, storage, update catalogs, tag triggers, and distribution destinations before use |
| Scheduled maintenance, feedback sync, and contributor-update jobs | Review outbound services, write permissions, target branches, and upstream-only guards |

Runtime service shutdown does not disable GitHub Actions. MUST keep deployment/signing secrets out of ordinary test jobs and untrusted PR builds, and use read-only workflow permissions unless a job needs more.

Keep Actions pinned to reviewed full commit SHAs and review their own downloaded dependencies. MUST NOT run untrusted PR code in privileged `pull_request_target` or `workflow_run` jobs. [GitHub secure workflow guidance](https://docs.github.com/en/actions/reference/security/secure-use).

Use hosted or disposable runners for untrusted contributions; keep release credentials in a separate, protected release environment. Audit artifacts and logs for secrets before publication.

## Releases and recovery

1. MUST release an exact reviewed fork commit and record the incorporated upstream SHA, dependency locks, toolchains, checks, and known limitations.

2. SHOULD use a distinct fork version/tag namespace, such as `fork-vX.Y.Z`, and adapt packaging before using it. Do not trigger inherited upstream release tags or publish to upstream registries and update catalogs by accident.

3. MUST preserve the [MIT license and copyright notice](../LICENSE), distinguish modified builds from upstream releases, and document the fork's behavior and private vulnerability-reporting contact.

4. MUST test packaged artifacts, not only source builds. Verify the embedded host/frontend, startup behavior, and supported platforms. Publish checksums and, when available, signatures with a trusted verification key; unsigned hashes alone do not authenticate a publisher.

5. SHOULD retain the previous verified artifact and back up local state before testing migrations. Check downgrade compatibility before using an older binary against migrated data.

6. MUST recover shared source history with a reviewed revert or fix on a new branch. For a bad upstream merge, verify its first parent before considering `git revert -m 1`; test the result before merging the recovery PR.

Reverting a merge also affects which changes future merges bring back. Plan an explicit fix or reapplication rather than assuming another upstream merge will restore them. [Git revert documentation](https://git-scm.com/docs/git-revert).

## Review checklist

- [ ] The upstream revision and fork changes are identified; ordinary agent workflows still work.
- [ ] Built-in background services remain disabled and existing security boundaries have regression coverage.
- [ ] Dependency, workflow, packaging, and configuration changes have been reviewed.
- [ ] Required checks cover the candidate commit; failures and platform limits are reported accurately.
- [ ] The change has a maintainer, a rollback path, and any needed release notes.
