# Review Output Template

Each reviewer produces a section following this structure. The aggregator combines all sections into the final output.

## Severity Levels

Every finding must be tagged with exactly one severity:

| Severity | Tag | Meaning | Merge impact |
|----------|-----|---------|-------------|
| Critical | `[CRITICAL]` | Must be fixed before merge. Bugs, security issues, data loss, broken contracts. | Blocks merge |
| Important | `[IMPORTANT]` | Suggested change, but author's discretion on implementing. Significant improvements to correctness, maintainability, or consistency. | Author decides |
| Suggestion | `[SUGGESTION]` | Could be better, but ok to proceed as-is. Style, naming, minor refactors. | Non-blocking |
| Informational | `[INFO]` | Things to be aware of only. Context, trade-offs, gotchas, follow-up reminders. No action required. | Non-blocking |

## Per-Reviewer Section Format

```
### {Reviewer Name}

**Verdict:** {APPROVE | CONCERNS | CHANGES REQUESTED}

#### Findings

- `[CRITICAL]` **file.go:42** — Description of the issue and why it blocks merge.
- `[IMPORTANT]` **file.go:78-85** — Description of the concern and why it matters. Author can decide whether to address now or defer.
- `[SUGGESTION]` **file.go:120** — Description of what could be improved and why.
- `[INFO]` Note about a trade-off or something the author should be aware of.

{Omit this section entirely if there are no findings.}

#### Positive

- Brief callout of something done well.

{Omit this section entirely if there is nothing notable to highlight.}

**Summary:** {One or two sentence summary of the review.}
```

## Verdict Criteria

- **APPROVE** — No critical or important findings. Change is ready to merge.
- **CONCERNS** — No critical findings, but one or more important findings that should be addressed or explicitly acknowledged before merge.
- **CHANGES REQUESTED** — One or more critical findings. Change should not merge until critical items are resolved.

## Aggregated Output Format

The final output presented to the user combines all reviewer sections under a top-level summary:

```
## Review Summary

**Overall:** {APPROVE | CONCERNS | CHANGES REQUESTED}
{The overall verdict is the most severe verdict from any reviewer.}

| Severity | Count |
|----------|-------|
| Critical | {N} |
| Important | {N} |
| Suggestion | {N} |
| Informational | {N} |

{One paragraph synthesizing the key takeaways across all reviewers. Highlight the most important findings.}

---

### General Review
{Per-reviewer section}

---

### Tmux Backend Review
{Per-reviewer section}

---

### Ambient Backend Review
{Per-reviewer section}

---

### Quality Review
{Per-reviewer section}
```

## Rules

- Include a reviewer section ONLY if the change is relevant to that reviewer's scope. If a change touches only frontend styling with no backend or quality concerns, skip backend reviewers and note they were not applicable.
- Every critical and important finding must reference a specific file and line number (or line range).
- Findings should explain WHY, not just WHAT.
- Positive findings reinforce good patterns the team should continue.
- Keep each section concise. Reviewers should not repeat each other's findings.
- If a reviewer has no findings at all, the verdict is APPROVE and the summary should say "No concerns within this reviewer's scope."
- The severity counts in the aggregated summary are totals across ALL reviewers.
