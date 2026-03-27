# Open-Dispatch Ambient Workflows

This directory contains Ambient workflows intended to be used in the development and operation of Open-Dispatch.

See [https://github.com/ambient-code/workflows](https://github.com/ambient-code/workflows) for more information on what
workflows are and how they are developed.

## Workflows

### [odis-pr-review](odis-pr-review/)

Automated code review for OpenDispatch pull requests. Given a PR number or GitHub URL, spawns four specialized reviewers in parallel (general, tmux backend, ambient backend, quality), aggregates findings by severity (Critical, Important, Suggestion, Informational), and posts the results as a comment on the PR.
