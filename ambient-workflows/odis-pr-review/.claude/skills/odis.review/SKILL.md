---
name: odis-review
description: Reviews code changes in the OpenDispatch project by spawning specialized sub-agents (general, tmux backend, ambient backend, quality) in parallel. Use when reviewing a PR, commit, set of commits, files, or any described change. Triggers on review requests, PR links, or "review this change."
argument-hint: <PR-link | commit-SHA | file-path | description>
---

# OpenDispatch Code Review

Review a change by spawning four specialized reviewers in parallel, then aggregating their findings.

## Reviewers

| Reviewer | Agent File | Scope |
|----------|-----------|-------|
| General | [odis.general.md](agents/odis.general.md) | API contracts, data integrity, cross-system coherence, branding, config |
| Tmux Backend | [odis.tmux.md](agents/odis.tmux.md) | Tmux session lifecycle, idle/approval detection, terminal interaction |
| Ambient Backend | [odis.ambient.md](agents/odis.ambient.md) | Ambient API integration, cloud sessions, AG-UI protocol |
| Quality | [odis.quality.md](agents/odis.quality.md) | Linting rules, testing, security, code conventions, architecture |

## Output format

Follow the template in [resources/output-template.md](resources/output-template.md) exactly.

## Workflow

### Step 1: Resolve the change

Determine what kind of input `$ARGUMENTS` is and obtain the diff:

- **GitHub PR URL** — run `gh pr diff <number>` and `gh pr view <number>` to get diff and description
- **Commit SHA** — run `git show <sha>` to get the diff
- **Commit range** (e.g. `abc123..def456`) — run `git diff <range>`
- **Branch name** — run `git diff main...<branch>`
- **File path(s)** — run `git diff` on each file, or if unstaged, read the files and use `git diff HEAD -- <file>`
- **Description** — search the codebase for relevant recent changes via `git log` and `git diff`

Capture the full diff text and a short summary of what changed (files touched, nature of the change).

### Step 2: Determine relevant reviewers

Based on the files and nature of the change, decide which reviewers are relevant:

- **General** — always relevant
- **Tmux Backend** — relevant if the change touches `tmux.go`, `session_backend_tmux.go`, `session_backend.go`, `lifecycle.go`, spawn/stop/restart flows, or frontend session UI
- **Ambient Backend** — relevant if the change touches `session_backend_ambient*.go`, `session_backend.go`, `lifecycle.go`, spawn/stop/restart flows, or frontend session UI
- **Quality** — always relevant

Skip reviewers whose scope has zero overlap with the change. Note skipped reviewers as "not applicable" in the final output.

### Step 3: Spawn reviewers in parallel

For each relevant reviewer, spawn a sub-agent using the Agent tool. All relevant reviewers run in parallel in a single message.

Each sub-agent prompt must include:
1. The instruction to read the reviewer's agent file from `${CLAUDE_SKILL_DIR}/agents/` for review guidance
2. The instruction to read the output template from `${CLAUDE_SKILL_DIR}/resources/output-template.md` for formatting
3. The full diff text
4. The summary of what changed
5. Clear instruction to review the change and return findings using the per-reviewer section format from the output template

### Step 4: Aggregate and present

Once all sub-agents return:
1. Read the output template from `${CLAUDE_SKILL_DIR}/resources/output-template.md`
2. Combine findings into the aggregated output format
3. Tally severity counts across all reviewers: Critical, Important, Suggestion, Informational
4. Set the overall verdict to the most severe verdict from any reviewer (CHANGES REQUESTED > CONCERNS > APPROVE)
5. Write a synthesis paragraph highlighting the most important findings across all reviewers
6. Include each reviewer's section in order: General, Tmux Backend, Ambient Backend, Quality
7. For skipped reviewers, add a single line: `Skipped — change does not touch this reviewer's scope.`
