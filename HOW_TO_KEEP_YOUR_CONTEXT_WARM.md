****# How To Keep Your Context Warm

Multi-agent AI development has a fundamental problem: agents forget. Every time a session compacts, resumes, or starts fresh, the model re-reads its context and reconstructs its understanding from scratch. The bigger the project, the more it forgets, the more time it wastes relearning, and the more likely it is to make decisions that contradict earlier ones.

The Boss coordinator solves this by giving agents a shared, persistent, structured document that acts as collective memory. Instead of each agent maintaining its own private understanding that evaporates at compaction, they all read from and write to a single source of truth that outlives any individual session.

This document explains the approach, why it works, and how to use it.

## The Problem: Cold Context

When an AI agent starts a new session or recovers from a compaction, it has no memory of what happened before. It must reconstruct everything from:

- The codebase itself (thousands of files, no obvious starting point)
- A summary of what happened (lossy, misses nuance)
- Whatever the user remembers to tell it (inconsistent, incomplete)

This leads to predictable failures:

1. **Contradictory decisions.** Agent A decides on approach X. Agent A compacts. New Agent A picks approach Y because it doesn't remember the reasoning behind X.
2. **Duplicated work.** Agent B solves a problem that Agent C already solved three hours ago.
3. **Lost tribal knowledge.** The team discovered that KSUIDs contain uppercase letters and K8s rejects them. Nobody wrote it down. The next agent hits the same bug.
4. **Coordination breakdown.** Five agents working on the same system, each with a different understanding of the API contract.

## The Solution: Boss Coordinator + Shared Working Document

The Boss coordinator (`components/boss/`) is a lightweight Go HTTP server that manages a shared markdown document (`working.md`). Every agent reads from it, writes to it, and treats it as the canonical state of the project.

### Architecture

```
Agent (API)  ──POST /agent/api──→  Queue ──→  Background goroutine
Agent (CP)   ──POST /agent/cp──→   Queue ──→  reads each queue,
Agent (SDK)  ──POST /agent/sdk──→  Queue ──→  splices content between
Agent (BE)   ──POST /agent/be──→   Queue ──→  BEGIN/END markers,
Agent (FE)   ──POST /agent/fe──→   Queue ──→  writes to working.md
Overlord     ──POST /agent/overlord→ Queue ──→  (single writer, no conflicts)
```

Each agent owns a section of the document, delimited by `<!-- BEGIN:{NAME} -->` and `<!-- END:{NAME} -->` HTML comment markers. When an agent posts an update, the coordinator splices it into the document without touching any other agent's section. A single background goroutine serializes all writes, so there are no conflicts.

### What Makes This Work

**1. Structured shared state, not chat history.**

The document is not a conversation log. It is organized into specific sections with specific purposes:

- **Session Dashboard:** One-line status per agent. Any agent (or human) can glance at this and know who is doing what.
- **Shared Contracts:** Agreed API surfaces, pagination rules, authentication patterns, state machines. These are the "hard truths" that no agent is allowed to contradict.
- **Agent Sections:** Each agent's current status, recent decisions, open questions, and technical details.
- **Archive:** Resolved items that no longer need to be in active context but should be recoverable.

This structure means a fresh agent can read the document top-down and reconstruct its understanding in a single pass. The Dashboard tells it "where are we." The Contracts tell it "what are the rules." Its own section tells it "what was I doing." The Archive tells it "what already happened."

**2. Write serialization eliminates conflicts.**

The coordinator uses per-agent buffered channels (64 deep) and a single drain goroutine. Agents post whenever they want. The goroutine processes updates sequentially, one agent at a time. No locks between agents. No merge conflicts. No lost updates.

This is critical because AI agents cannot coordinate their write timing. They don't know when another agent is about to write. The queue-and-splice model makes this irrelevant.

**3. Compaction-resistant context.**

When an agent session compacts, the model receives a summary of what happened. But the working document persists on disk. The new session reads the document and is immediately "warm" — it knows the current state, the standing orders, the contracts, and what its peers are doing.

In practice, this means an agent recovers to approximately 95% effectiveness after compaction. The 5% loss is procedural memory (the "feel" of debugging a specific problem), not factual knowledge.

**4. The Overlord pattern.**

One agent (the Overlord) acts as the coordinator. It reads every other agent's section, evaluates progress, identifies blockers, issues standing orders, and grades performance. The Overlord doesn't write code. It writes strategy.

This creates a hierarchy: agents write to their sections, the Overlord reads all sections and posts directives, the Boss (human) reads the Overlord's analysis and makes final calls. Questions flow up (tagged with `[?BOSS]`), decisions flow down (via standing orders).

**5. Progressive compaction keeps context lean.**

Rule 9 of the protocol: "When your section exceeds ~20 items, move resolved items to the Archive." This prevents the document from growing unbounded. Active context stays small and relevant. Historical context moves to the Archive where it can be retrieved if needed but doesn't consume attention.

Agents mark compacted entries with `(compacted)` — a single-line summary of what was a multi-paragraph update. This preserves the timeline without the bulk.

## How Context Warmth Is Measured

Context warmth has four dimensions:


| Dimension                 | Cold                   | Warm                                                                | How the coordinator helps                                    |
| ------------------------- | ---------------------- | ------------------------------------------------------------------- | ------------------------------------------------------------ |
| **Situational awareness** | "What's happening?"    | Agent knows every peer's status                                     | Dashboard table, read on every cycle                         |
| **Technical fidelity**    | "What are the rules?"  | Agent knows exact API contracts, state machines, naming conventions | Shared Contracts section, immutable without mutual agreement |
| **Task continuity**       | "What was I doing?"    | Agent can resume mid-task after compaction                          | Agent's own section with timestamped entries                 |
| **Decision history**      | "Why did we choose X?" | Agent knows past decisions and reasoning                            | Archive section with Key Decisions log                       |

A "fully warm" agent scores high on all four. The working document provides all four in a single read.

## How To Use the Boss Coordinator

### Starting the coordinator

```bash
cd components
WORKING_DIR=../ go run boss/cmd/boss/main.go
```

The coordinator starts on port 4345 by default (`COORDINATOR_PORT` env var to override). It reads and writes `working.md` in the working directory.

### Agent operations

```bash
# Read the full document
curl -s http://localhost:4345/raw

# Read your section only
curl -s http://localhost:4345/agent/api

# Post your update (write to temp file first to avoid shell mangling)
curl -s -X POST http://localhost:4345/agent/api \
  -H 'Content-Type: text/plain' \
  --data-binary @/tmp/my_update.md

# View rendered markdown in a browser (polls every 3s, highlights [?BOSS] tags)
open http://localhost:4345
```

### Setting up a new working document

The document needs `<!-- BEGIN:{NAME} -->` / `<!-- END:{NAME} -->` markers for each agent. The coordinator auto-registers new agents on first POST, but the markers must exist in the document for splicing to work.

Minimal template:

```markdown
# Working Document

## Session Dashboard

| **Session** | **Status** |
| ----------- | ---------- |
| Agent1      | starting   |

## Shared — Contracts

_Put agreed rules here._

## Agent Sections

### Agent1

<!-- BEGIN:Agent1 -->
Initial state.
<!-- END:Agent1 -->

## Archive

_Resolved items go here._
```

### Protocol rules for agents

1. **Read before you write.** Always `GET /raw` first.
2. **Write to your section only.** Use `POST /agent/{name}`.
3. **Timestamp everything.** ISO-8601 (`YYYY-MM-DD HH:MM`).
4. **Tag your entries.** `[API]`, `[CP]`, etc.
5. **Ask up, don't guess.** Tag questions with `[?BOSS]` so the human can spot them.
6. **Compact regularly.** Keep your section under ~20 active items.
7. **Never contradict Shared Contracts.** If you think a contract is wrong, ask — don't unilaterally change it.

## Why This Works: Lessons From Production

We used this system to coordinate 6 agents (API, Control Plane, SDK, Backend Expert, Frontend, Overlord) through a full platform refactoring: replacing a monolithic Go backend (75 routes, K8s CRDs/etcd) with three focused components (API server + PostgreSQL, control plane, multi-language SDK). The results:

**370 tests written across 5 components in a single day.** No test contradicted another component's contract. The Shared Contracts section was the single source of truth for API shapes, pagination rules, and state machines.

**Zero coordination conflicts.** Five agents writing to the same document concurrently, never once clobbering each other's content. The queue-and-splice model handled it.

**Recovery from compaction in one prompt.** When an agent compacted, the new session read working.md and was immediately productive. The Dashboard told it the current state. The Contracts told it the rules. Its own section told it what to do next.

**Bugs caught before they spread.** The Backend Expert agent identified 6 behavioral gaps between the old backend and the new API server. These were logged in the shared document with severity ratings. The Overlord tracked them. The Boss made priority calls. Every agent saw the same gap list — no one accidentally shipped a known bug.

**Progressive decision-making.** Ambiguous questions (Should restart-from-Failed be allowed? Should the API return 200 or 202?) were tagged `[?BOSS]`, bubbled up through the Overlord, decided by the human, and recorded in Shared Contracts. Once decided, every agent respected the ruling because it was in the document they all read.

## The Anti-Patterns

Things that degrade context warmth:

1. **Skipping the read.** An agent that posts without reading first will contradict something that was decided while it was compacted.
2. **Hoarding context.** An agent that keeps important information in its own section instead of promoting it to Shared Contracts. Other agents can't benefit from knowledge they can't see.
3. **Stale standing orders.** Orders that were completed but never archived. Agents waste time trying to figure out if "SMOKE TEST IS GO" is still the current directive or a historical artifact.
4. **Unbounded sections.** An agent that never compacts. Its section grows until it dominates the document, pushing other agents' context out of the effective window.
5. **Silent agents.** An agent that doesn't post updates. The Overlord can't evaluate what it can't see. Other agents can't coordinate with a ghost.

## Summary

Context warmth is not about giving agents more tokens. It is about giving them the right tokens, in the right structure, at the right time.

The Boss coordinator provides:

- **Persistent shared state** that outlives any individual session
- **Structured sections** that make reconstruction fast and reliable
- **Write serialization** that eliminates conflicts between concurrent agents
- **Progressive compaction** that keeps active context lean
- **Hierarchical coordination** (Agent -> Overlord -> Boss) that scales decision-making

The result: agents that remember, coordinate, and build on each other's work instead of starting over every time.
