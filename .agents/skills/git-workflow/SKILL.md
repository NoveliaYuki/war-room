---
name: git-workflow
description: Apply War Room commit, version, and push conventions when preparing an explicitly requested Git operation.
---

## 1. Overview

Use only when the user asks to prepare a commit, release, or pull request.
Complete [pre-commit](../pre-commit/SKILL.md) before any authorized commit.
Editing files or approving tests does not authorize Git operations.
All repository changes go through a topic branch and a pull request to `main`;
never commit or push directly to `main`.

## 2. Quick Reference

Choose the type by intent, not by files changed.

| Type | Use for |
| --- | --- |
| `fix` | Repairing broken or unsafe behavior; this takes priority over file scope |
| `feat` | Adding user-facing behavior |
| `perf` | Improving speed or resource use |
| `refactor` | Internal changes with no behavior change |
| `test` / `docs` | Tests-only / documentation-only changes |
| `build` / `ci` | Docker, dependencies, packaging / CI-only changes |
| `style` | Formatting-only changes |
| `chore` | Other maintenance or version-only changes |

Commit subject: `<type>: [vX.Y.Z] - <imperative summary>`.

## 3. Branch and Pull Request Workflow

Start each change from an up-to-date `main` in a topic branch. Use a descriptive
prefix such as `feature/`, `fix/`, `docs/`, or `chore/` to make the purpose clear.
Push the topic branch and open a pull request targeting `main`; do not push a
change directly to `main`, even for a release or a small fix. Merge only through
the pull request after the required checks pass. Keep GitHub branch protection on
`main` enabled so direct pushes, force pushes, and deletion of `main` are blocked.
Enable automatic deletion of head branches after their pull requests are merged.

```text
main → feature/<short-description> → pull request → main
```

## 4. Lifecycle

```text
authorization → feature branch → inspect changes → choose type/version
              → pass pre-commit → verify files → pull request → merge
```

Stop at the last step the user authorized. Creating a branch, pushing it, opening
a pull request, merging, and publishing a release each require explicit user
authorization.

## 5. Step 1 — Check Authorization and Scope

Run `git status --short`, `git diff --check`, and inspect staged, modified, and untracked files.
Check local data and build artifacts before selecting files.
Never stage `data/`, `backend/data/`, databases, company logos, attachments, or secrets.

| Operation | Authorization required |
| --- | --- |
| Stage or commit | User explicitly asks for a commit |
| Amend or rewrite history | User explicitly asks for that operation |
| Push | User explicitly requests a push |
| Create a branch, issue, or PR | User explicitly asks |

For a requested change, create a topic branch rather than working on `main`.
Before opening a pull request, verify its base is `main`, its branch is not
`main`, and the working tree contains no local data, build artifacts, secrets,
or unrelated changes.

## 6. Step 2 — Choose Type and Version

Use this order when intent is mixed:

| Question | Type or action |
| --- | --- |
| Does it repair a defect/security issue? | `fix` |
| Does it add user behavior? | `feat` |
| Is the main outcome performance? | `perf` |
| Is behavior unchanged? | Choose `refactor`, `test`, `docs`, `build`, `ci`, or `style` by scope |
| No category fits? | `chore` |

Update `VERSION` in the same pull request as every functional app change, choosing
the SemVer bump by impact: breaking changes bump major, backward-compatible
features bump minor, and compatible fixes or maintenance bump patch. Purely
documentation or repository-workflow changes do not bump the app version. Use
the root `VERSION` file as the sole application version for frontend and backend;
do not duplicate it in package metadata or image labels. Bump it at most once per
PR, then use that resulting version in each commit subject in the PR.

Publish application releases only for minor or major versions. Patch version
bumps still merge through normal PRs, but do not create patch release tags or
GitHub releases. Include all compatible fixes accumulated since the prior
published release in the next minor or major release's `What changed` notes. A
minor release increments the minor component and resets patch to zero (for
example, `1.1.3` → `1.2.0`); a major release increments major and resets minor
and patch to zero (for example, `1.2.3` → `2.0.0`).

| Bump | Meaning |
| --- | --- |
| Major | Breaking change |
| Minor | Backward-compatible feature |
| Patch | Compatible fixes or maintenance; update `VERSION`, but do not publish a release |

## 7. Step 3 — Write the Commit Subject

Use lowercase type, `[vX.Y.Z]`, ` - `, and an imperative, concrete summary.
Keep the subject specific and at most 72 characters where practical.
Do not use vague summaries such as “fix issues”, “update UI”, or “apply feedback”.

| Avoid | Prefer |
| --- | --- |
| `fix: [v0.0.1] - fix issues` | `fix: [v0.0.1] - ignore local application data` |
| `feat: [v0.0.1] - update UI` | `feat: [v0.0.1] - add meeting filters` |

## 8. Step 4 — Verify, Merge, and Release

Before an authorized commit, inspect `git diff --cached --check` and the full
staged file list. Run the complete pre-commit workflow before committing or
requesting merge; keep commits focused and do not bypass failing checks. Prepare
pull request text only when requested, and accurately report checks and results.

After an authorized minor or major release pull request is merged and CI passes,
create and push the matching `vX.Y.0` or `vX.0.0` tag, then publish a GitHub
release for that tag. Do not publish a release from an unmerged branch or before
CI passes. Release notes use this template; keep each bullet to no more than two
sentences:

```markdown
## Highlights
- Summarize the release's most useful new capability or broad improvement.

## What changed
- List other shipped changes, including fixes accumulated since the prior release.

```

Use **Highlights** rather than **New features** so the heading also fits major
improvements and releases whose main value is broader than a single new feature.
Use **What changed** for the concise change list, including accumulated bug
fixes. Omit upgrade notes by default for this local app; add an **Upgrade notes**
section only when a release requires user action or a special manual data or
compatibility step. Report the release URL, tag, commit, and verified CI result
when the user authorized those operations.

## 9. Red Flags — Never / Always

### Never

- Never stage or commit without an explicit request.
- Never commit or push directly to `main`; never bypass the pull request.
- Never publish a patch release.
- Never include private data, local state, or generated artifacts.
- Never bypass pre-commit checks or rewrite published history.
- Never make multiple version bumps in one PR.

### Always

- Always run pre-commit before an authorized commit.
- Always use a topic branch and a pull request targeting `main`.
- Always wait for passing CI before merging or publishing a release.
- Always choose the type by intent before file scope.
- Always follow the exact subject format in §2.
- Always keep `VERSION` unchanged for routine non-release pull requests.
- Always verify staged files and report only completed Git actions.

## 10. Worked Example

Authorized compatible fix that bumps `VERSION` from `1.1.0` to `1.1.1`:
`fix: [v1.1.1] - preserve search focus in compact toolbar`.
Publish no patch release for `1.1.1`; include it in the next minor or major
release, such as `1.2.0`, after that release PR merges and CI passes.
