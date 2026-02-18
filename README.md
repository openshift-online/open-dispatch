# Boss Coordinator

A lightweight shared memory bus for multi-agent coordination. Agents post structured status updates to a central HTTP server, which persists state as JSON and renders human-readable markdown.

## Architecture: Shared Memory Bus (Blackboard Pattern)

```
Agent A ──POST JSON──┐
Agent B ──POST JSON──┤
Agent C ──POST JSON──┼──▶ Boss Server ──▶ KnowledgeSpace (in-memory)
Agent D ──POST JSON──┤         │                  │
Agent E ──POST JSON──┘         ▼                  ▼
                          feature.json       feature.md
                         (structured)     (human-readable)
```

Agents read and write to a central state instead of message-passing. Each agent owns its section. The server serializes all writes and guarantees well-formed output.

### Why structured data over raw markdown?

Raw markdown coordination documents corrupt easily when multiple agents splice text concurrently. Structured JSON input with server-side markdown rendering eliminates:

- Broken table formatting from malformed pipes
- Dashboard corruption from conflicting writes
- Lost sections from overlapping BEGIN/END markers

Agents POST structured data. The server assembles guaranteed well-formed markdown.

## Quick Start

```bash
# Build
export GOROOT="/path/to/go1.24"
go build -o boss ./cmd/boss/

# Run
PORT=8899 DATA_DIR=./data ./boss

# Open dashboard
open http://localhost:8899
```

## Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `PORT` | `8899` | Server listen port |
| `DATA_DIR` | `./data` | Directory for JSON + markdown persistence |

## API Reference

### Spaces

Spaces are independent coordination contexts. Each space is a KnowledgeSpace with its own agents, contracts, and archive.

| Method | Endpoint | Description |
|--------|----------|-------------|
| `GET` | `/` | HTML dashboard listing all spaces |
| `GET` | `/spaces` | JSON array of space summaries |
| `GET` | `/spaces/{space}/` | HTML viewer for a space (auto-polls every 3s) |

### Agents

| Method | Endpoint | Description |
|--------|----------|-------------|
| `GET` | `/spaces/{space}/agent/{name}` | Get agent state as JSON |
| `POST` | `/spaces/{space}/agent/{name}` | Update agent (JSON or text/plain) |
| `DELETE` | `/spaces/{space}/agent/{name}` | Remove agent from space |
| `GET` | `/spaces/{space}/api/agents` | All agents as JSON map |

### Rendered Output

| Method | Endpoint | Description |
|--------|----------|-------------|
| `GET` | `/spaces/{space}/raw` | Full space rendered as markdown |

### Shared Data

| Method | Endpoint | Description |
|--------|----------|-------------|
| `GET/POST` | `/spaces/{space}/contracts` | Shared contracts (text) |
| `GET/POST` | `/spaces/{space}/archive` | Archive of resolved items (text) |

### Backward Compatibility

Routes without `/spaces/` prefix operate on the `"default"` space:

| Endpoint | Equivalent |
|----------|------------|
| `/raw` | `/spaces/default/raw` |
| `/agent/{name}` | `/spaces/default/agent/{name}` |
| `/api/agents` | `/spaces/default/api/agents` |

## Agent Update Format

```json
{
  "status": "active",
  "summary": "One-line summary (required)",
  "phase": "2.5b",
  "test_count": 88,
  "items": ["bullet point 1", "bullet point 2"],
  "sections": [
    {
      "title": "Section Name",
      "items": ["detail 1", "detail 2"],
      "table": {
        "headers": ["Col A", "Col B"],
        "rows": [["val1", "val2"]]
      }
    }
  ],
  "questions": ["auto-tagged with [?BOSS] in rendered output"],
  "blockers": ["rendered with red indicator"],
  "next_steps": "What you plan to do next"
}
```

### Status Values

| Status | Emoji | Meaning |
|--------|-------|---------|
| `active` | green | Currently working |
| `done` | checkmark | Work complete |
| `blocked` | red | Waiting on dependency |
| `idle` | pause | Standing by |
| `error` | X | Something failed |

### Plain Text Fallback

If you POST with `Content-Type: text/plain`, the body is wrapped into an `AgentUpdate` with `status: active` and the first line as `summary`. This supports legacy agents and quick updates:

```bash
curl -s -X POST http://localhost:8899/spaces/my-feature/agent/api \
  -H 'Content-Type: text/plain' \
  --data-binary @/tmp/my_update.md
```

## Persistence

On every mutation the server writes two files to `DATA_DIR`:

| File | Format | Purpose |
|------|--------|---------|
| `{space}.json` | Structured JSON | Source of truth, loaded on startup |
| `{space}.md` | Rendered markdown | Human-readable snapshot, verifies renderer |

The `.md` file is regenerated from the `.json` on every write. It is not read back by the server -- the JSON is canonical.

## Project Structure

```
components/boss/
  cmd/boss/main.go                      # Entrypoint
  internal/coordinator/
    types.go                            # AgentUpdate, KnowledgeSpace, markdown renderer
    server.go                           # HTTP server, persistence, routing
    client.go                           # Go client for programmatic access
    server_test.go                      # 17 tests with -race
  data/
    {space}.json                        # Persisted KnowledgeSpaces
    {space}.md                          # Rendered markdown snapshots
  go.mod
```

## Distributed Agent Architecture

The bus is agent-location-agnostic. Any process that can HTTP POST can participate, regardless of where it runs:

| Use Case | Agent Location | Bus Sees |
|----------|---------------|----------|
| Security isolation | Air-gapped pod with IAM role | `{"status":"done","summary":"Rotation successful"}` |
| GPU workloads | Node with H100 + vector DB | `{"status":"done","summary":"Analysis complete","sections":[...]}` |
| Data sovereignty | Regional VPC (Frankfurt, Virginia) | `{"status":"done","summary":"EU error rate: 0.3%"}` |

The `AgentUpdate` schema is the declassification boundary. Agents distill privileged access into structured results. The bus never sees raw credentials, embeddings, or PII.

## Testing

```bash
go test -race -v ./internal/coordinator/
```
