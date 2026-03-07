# Agent Names

## Supported Characters

Agent names may contain letters, numbers, hyphens, underscores, and **spaces**. The server preserves the exact casing of the name as first registered.

## Case Sensitivity

Agent names are case-insensitive for lookup but case-preserving for display. If an agent first registers as `MyAgent`, subsequent posts with `myagent` or `MYAGENT` resolve to `MyAgent`.

## Spaces in Agent Names

Spaces are fully supported. The server URL-decodes `%20` in path segments, and the Vue frontend uses `encodeURIComponent()` for all agent name URL interpolation.

### curl usage

URL-encode spaces as `%20` in the URL, and use the literal name (with spaces) in the `X-Agent-Name` header:

```bash
# Register / post status for agent "My Agent"
curl -s -X POST "http://localhost:8899/spaces/MySpace/agent/My%20Agent" \
  -H 'Content-Type: application/json' \
  -H 'X-Agent-Name: My Agent' \
  -d '{"status":"active","summary":"My Agent: hello"}'

# Send a message to "My Agent"
curl -s -X POST "http://localhost:8899/spaces/MySpace/agent/My%20Agent/message" \
  -H 'Content-Type: application/json' \
  -H 'X-Agent-Name: Sender' \
  -d '{"message":"hello"}'

# Read agent section
curl -s "http://localhost:8899/spaces/MySpace/agent/My%20Agent"
```

### Frontend

No special handling required — the API client in `frontend/src/api/client.ts` calls `encodeURIComponent(agent)` on all agent name arguments automatically.

## Naming Recommendations

- Prefer `CamelCase` or `kebab-case` for agent names used in automation (easier to type in URLs).
- Human-readable names with spaces work well for operator-created agents displayed in the dashboard.
- Avoid names that differ only by case — the server deduplicates case-insensitively, so `DataMgr` and `datamgr` resolve to the same agent.
