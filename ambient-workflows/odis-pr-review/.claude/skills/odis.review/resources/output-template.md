# Review Output Template

Each reviewer produces a section following this structure. The aggregator combines all sections into the final output.

## Severity Levels

Every finding must be tagged with exactly one severity:

| Severity | Tag | Meaning | Merge impact |
|----------|-----|---------|-------------|
| Critical | `[CRITICAL]` | Must be fixed before merge. Bugs, security issues, data loss, broken contracts. | Blocks merge |
| Concern | `[CONCERN]` | Worth addressing but author's discretion. Correctness, maintainability, consistency, style, naming, minor refactors. | Author decides |
| Informational | `[INFO]` | Context, trade-offs, gotchas, follow-up reminders. No action required. | Non-blocking |

## Per-Reviewer Section Format

```
### {Reviewer Name}

**Verdict:** {APPROVE | CONCERNS | CHANGES REQUESTED} — {One sentence summary of this reviewer's assessment.}

<details>
<summary>Details</summary>

#### Findings

- `[CRITICAL]` **file.go:42** — Description of the issue and why it blocks merge.
- `[CONCERN]` **file.go:78-85** — Description of the concern and why it matters. Author can decide whether to address now or defer.
- `[INFO]` Note about a trade-off or something the author should be aware of.

{Omit this section entirely if there are no findings.}

#### Positive

- Brief callout of something done well.

{Omit this section entirely if there is nothing notable to highlight.}

</details>
```

## Verdict Criteria

- **APPROVE** — No critical or concern findings. Change is ready to merge.
- **CONCERNS** — No critical findings, but one or more concern findings that should be addressed or explicitly acknowledged before merge.
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
| Concern | {N} |
| Informational | {N} |

{One paragraph synthesizing the key takeaways across all reviewers. Highlight the most important findings.}

---

{Per-reviewer sections in order: General, Tmux Backend, Ambient Backend, Quality.
Each section uses the per-reviewer format above — verdict + summary visible,
findings and positives collapsed inside <details>.}
```

## Rules

- Include a reviewer section ONLY if the change is relevant to that reviewer's scope. If a change touches only frontend styling with no backend or quality concerns, skip backend reviewers and note they were not applicable.
- Every critical and concern finding must reference a specific file and line number (or line range).
- Findings should explain WHY, not just WHAT.
- Positive findings reinforce good patterns the team should continue.
- Keep each section concise. Reviewers should not repeat each other's findings.
- If a reviewer has no findings at all, the verdict is APPROVE and the summary should say "No concerns within this reviewer's scope."
- The severity counts in the aggregated summary are totals across ALL reviewers.
