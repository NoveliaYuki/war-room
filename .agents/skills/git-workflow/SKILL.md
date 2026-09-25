---
name: git-workflow
description: Apply War Room commit, version, and push conventions when preparing an explicitly requested Git operation.
---

## 1. Overview

Use only when the user asks to prepare a commit, release, or pull request.
Complete [pre-commit](../pre-commit/SKILL.md) before any authorized commit.
Editing files or approving tests does not authorize Git operations.

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

## 3. Lifecycle

```text
authorization → inspect changes → choose type → decide version
              → pass pre-commit → verify files → authorized Git operation
```

Stop at the last step the user authorized.

## 4. Step 1 — Check Authorization and Scope

Run `git status --short`, `git diff --check`, and inspect staged, modified, and untracked files.
Check local data and build artifacts before selecting files.
Never stage `data/`, `backend/data/`, databases, company logos, attachments, or secrets.

| Operation | Authorization required |
| --- | --- |
| Stage or commit | User explicitly asks for a commit |
| Amend or rewrite history | User explicitly asks for that operation |
| Push | User explicitly requests a push |
| Create a branch, issue, or PR | User explicitly asks |

## 5. Step 2 — Choose Type and Version

Use this order when intent is mixed:

| Question | Type or action |
| --- | --- |
| Does it repair a defect/security issue? | `fix` |
| Does it add user behavior? | `feat` |
| Is the main outcome performance? | `perf` |
| Is behavior unchanged? | Choose `refactor`, `test`, `docs`, `build`, `ci`, or `style` by scope |
| No category fits? | `chore` |

Use Semantic Versioning only when a release bump is authorized.

| Bump | Meaning |
| --- | --- |
| Major | Breaking change |
| Minor | Backward-compatible feature |
| Patch | Compatible fix or maintenance release |

Keep the initial version `0.0.1` unless the user authorizes a later release.
Bump once per PR, then reuse that version on every commit in the PR.
Synchronize `VERSION`, package metadata, and image labels.

## 6. Step 3 — Write the Commit Subject

Use lowercase type, `[vX.Y.Z]`, ` - `, and an imperative, concrete summary.
Keep the subject specific and at most 72 characters where practical.
Do not use vague summaries such as “fix issues”, “update UI”, or “apply feedback”.

| Avoid | Prefer |
| --- | --- |
| `fix: [v0.0.1] - fix issues` | `fix: [v0.0.1] - ignore local application data` |
| `feat: [v0.0.1] - update UI` | `feat: [v0.0.1] - add meeting filters` |

## 7. Step 4 — Verify and Report

Before an authorized commit, inspect `git diff --cached --check` and the full staged file list.
Keep commits focused; do not bump the version more than once in one PR.
Prepare PR text only when requested; state actual changes and checks accurately.
Report the commit hash or push destination only if that operation occurred.

## 8. Red Flags — Never / Always

### Never

- Never stage or commit without an explicit request.
- Never push without an explicit user request.
- Never include private data, local state, or generated artifacts.
- Never bypass pre-commit checks or rewrite published history.
- Never make multiple version bumps in one PR.

### Always

- Always run pre-commit before an authorized commit.
- Always choose the type by intent before file scope.
- Always follow the exact subject format in §2.
- Always preserve `0.0.1` until a later release is authorized.
- Always verify staged files and report only completed Git actions.

## 9. Worked Example

Authorized fix, no release bump: `fix: [v0.0.1] - ignore local application data`.
If several commits belong to that PR, every one keeps `v0.0.1`.
Do not stage or commit unless the user has explicitly asked for it.
