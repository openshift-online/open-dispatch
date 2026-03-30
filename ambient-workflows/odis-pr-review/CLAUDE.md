# OpenDispatch PR Review Workflow

## Repository

All PRs are against `openshift-online/open-dispatch` on GitHub.

## Skill Usage

This workflow uses a single skill:

- `/odis-review <PR>` — runs four specialized reviewers in parallel and returns aggregated findings

Invoke the skill with the PR number or URL. The skill handles resolving the diff, spawning sub-agents, and formatting the output.

## Conventions

- Always use `file:line` notation when referencing code (e.g., `server.go:245`)
- Write the review to `artifacts/review/review.md` before posting as a PR comment
- Post the review as a single PR comment — do not split across multiple comments
- Use `gh` CLI for all GitHub operations
