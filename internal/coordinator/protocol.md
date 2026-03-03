## Communication Protocol

### Coordinator (8899)

All agents use `localhost:8899` exclusively.

Space: `{SPACE}`

### Endpoints

| Action | Command |
|--------|---------|
| Post (JSON) | `curl -s -X POST http://localhost:8899/spaces/{SPACE}/agent/{name} -H 'Content-Type: application/json' -d '{"status":"...","summary":"...","items":[...]}'` |
| Post (text) | `curl -s -X POST http://localhost:8899/spaces/{SPACE}/agent/{name} -H 'Content-Type: text/plain' --data-binary @/tmp/my_update.md` |
| Read section | `curl -s http://localhost:8899/spaces/{SPACE}/agent/{name}` |
| Read full doc | `curl -s http://localhost:8899/spaces/{SPACE}/raw` |
| Browser | `http://localhost:8899/spaces/{SPACE}/` (polls every 3s) |

### Rules

1. **Read before you write.** Always `GET /raw` first.
2. **Post to your endpoint only.** Use `POST /spaces/{SPACE}/agent/{name}`.
3. **Tag questions with `[?BOSS]`** — they render highlighted in the dashboard.
4. **Concise summaries.** Always Use "{name}: {summary}" (required!).
5. **Safe writes.** Write to a temp file first, then POST with `--data-binary @/tmp/file.md`.

### JSON Format Reference

```json
{
  "status": "active|done|blocked|idle|error",
  "summary": "One-line summary (required)",
  "phase": "current phase",
  "test_count": 0,
  "items": ["bullet point 1", "bullet point 2"],
  "sections": [{"title": "Section Name", "items": ["detail"]}],
  "questions": ["tagged [?BOSS] automatically"],
  "blockers": ["highlighted automatically"],
  "next_steps": "What you're doing next"
}
```