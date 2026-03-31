# OpenDispatch PR Review Workflow

Automated code review workflow for OpenDispatch pull requests on the Ambient Code Platform.

## What it does

Given a PR number or GitHub URL, this workflow:

1. Fetches the PR diff and metadata from `openshift-online/open-dispatch`
2. Spawns four specialized reviewers in parallel:
   - **General** — API contracts, data integrity, cross-system coherence, branding
   - **Tmux Backend** — tmux session lifecycle, idle/approval detection, terminal interaction
   - **Ambient Backend** — Ambient API integration, cloud sessions, AG-UI protocol
   - **Quality** — linting rules, testing standards, security, code conventions
3. Aggregates findings with severity levels (Critical, Concern, Informational)
4. Posts the review as a comment on the PR

## Automatic Trigger

This workflow is triggered automatically via GitHub Actions when a PR is opened or updated against `main`. See `.github/workflows/pr-review.yml`.

## Manual Usage

Provide a PR number or URL when prompted:

```
123
```

```
https://github.com/openshift-online/open-dispatch/pull/123
```

## Severity Levels

| Severity | Meaning | Merge Impact |
|----------|---------|-------------|
| Critical | Must fix before merge | Blocks merge |
| Concern | Worth addressing, author's discretion | Author decides |
| Informational | Awareness only | Non-blocking |

## Verdicts

- **APPROVE** — No critical findings. Concerns may exist but are at the author's discretion.
- **CONCERNS** — Concerns significant enough to warrant discussion before merge.
- **CHANGES REQUESTED** — Critical findings that block merge.

## Structure

```
odis-pr-review/
├── .ambient/
│   └── ambient.json              # ACP workflow configuration
├── .claude/
│   └── skills/
│       └── odis.review/          # Review skill
│           ├── SKILL.md          # Skill entrypoint
│           ├── agents/           # Reviewer agent definitions
│           │   ├── odis.general.md
│           │   ├── odis.tmux.md
│           │   ├── odis.ambient.md
│           │   └── odis.quality.md
│           └── resources/
│               └── output-template.md
├── CLAUDE.md                     # Persistent context
└── README.md                     # This file
```

## Testing with Custom Workflow

Push to a branch and use ACP's "Custom Workflow" feature:

- **Git URL:** `https://github.com/openshift-online/open-dispatch.git`
- **Branch:** your branch name
- **Path:** `ambient-workflows/odis-pr-review`
