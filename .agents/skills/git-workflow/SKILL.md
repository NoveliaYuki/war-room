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

For each change, start a topic branch from the latest `main`.
Before opening a pull request, verify its base is `main`, its branch is not
`main`, and the working tree contains no local data, build artifacts, secrets,
or unrelated changes.

Search open and closed issues for an existing issue that the pull request fully
resolves. Always add one `Closes` line to the pull request body: use
`Closes #<number>` when an issue will be fixed, or `Closes NO_ISSUE` when no
matching issue exists. Do not close an issue that is only related to the change.

## 6. Step 2 — Choose Type and Version

Use this order when intent is mixed:

| Question | Type or action |
| --- | --- |
| Does it repair a defect/security issue? | `fix` |
| Does it add user behavior? | `feat` |
| Is the main outcome performance? | `perf` |
| Is behavior unchanged? | Choose `refactor`, `test`, `docs`, `build`, `ci`, or `style` by scope |
| No category fits? | `chore` |

Update root `VERSION` once per PR with functional app changes. Use the resulting
version in every commit subject in that PR. Do not duplicate the app version in
package metadata or image labels.

| Change | `VERSION` update | Publish a release? |
| --- | --- | --- |
| Breaking change | Major; `1.2.3` → `2.0.0` | Yes, after merge and passing CI |
| Backward-compatible feature | Minor; `1.1.3` → `1.2.0` | Yes, after merge and passing CI |
| Compatible fix or maintenance | Patch; `1.1.0` → `1.1.1` | No; include it in the next minor or major release |
| Documentation or workflow only | No bump | No |

Include all fixes since the last published release in the next minor or major
release. Never publish a patch release.

## 7. Step 3 — Write the Commit Subject

Use lowercase type, `[vX.Y.Z]`, ` - `, and an imperative, concrete summary.
Keep the subject specific and at most 72 characters where practical.
Do not use vague summaries such as “fix issues”, “update UI”, or “apply feedback”.

| Avoid | Prefer |
| --- | --- |
| `fix: [v0.0.1] - fix issues` | `fix: [v0.0.1] - ignore local application data` |
| `feat: [v0.0.1] - update UI` | `feat: [v0.0.1] - add meeting filters` |

## 8. Step 4 — Verify, Merge, and Release

Before committing, inspect staged files and run all pre-commit checks. Do not
commit or request merge if a check fails. Report checks accurately in the PR.

After the release PR merges and CI passes, tag and publish its minor or major
version. Use these release-note sections; keep each bullet to at most two
sentences:

```markdown
## Highlights
- List new features and major improvements.

## What changed
- List other updates and fixes since the previous release.

```

Add an **Upgrade notes** section only when users must take a manual action.

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
- Always bump `VERSION` for functional app changes; leave it unchanged for docs-only changes.
- Always verify staged files and report only completed Git actions.

## 10. Worked Example

Authorized compatible fix that bumps `VERSION` from `1.1.0` to `1.1.1`:
`fix: [v1.1.1] - preserve search focus in compact toolbar`.
Publish no patch release for `1.1.1`; include it in the next minor or major
release, such as `1.2.0`, after that release PR merges and CI passes.
