# resolveAgentName Lock Audit

**Author:** SMEAgent
**Date:** 2026-03-07
**File:** `internal/coordinator/server.go`

## Background

`resolveAgentName(ks *KnowledgeSpace, raw string)` iterates `ks.Agents` to find a
case-insensitive match. The comment at line 2036 says:

> resolveAgentName iterates ks.Agents — must hold s.mu to avoid data race.

Any call site that invokes `resolveAgentName` without holding `s.mu` is a data race
under Go's memory model.

---

## Call Site Inventory

### UNSAFE — called without lock

| Line | Handler | Context | Risk |
|------|---------|---------|------|
| 787 | `handleSpaceAgent` GET | `ks` from `s.getSpace()` (RLock released), then `resolveAgentName`, then `s.mu.RLock()` | ks.Agents read while another goroutine may be writing |
| 811 | `handleSpaceAgent` POST | `ks` from `s.getOrCreateSpace()` (Lock released), then `resolveAgentName`, then `s.mu.Lock()` at line 845 | ks.Agents read while another goroutine may be writing |
| 893 | `handleSpaceAgent` DELETE | `ks` from `s.getSpace()` (RLock released), then `resolveAgentName`, then `s.mu.Lock()` immediately after at line 894 | ks.Agents read while another goroutine may be writing |
| 972 | `handleAgentMessage` | After conditional Lock/Unlock block (lines 967-969), then `resolveAgentName`, then `s.mu.Lock()` at line 974 | ks.Agents read while another goroutine may be writing |
| 1125 | `handleAgentDocument` POST/PUT | `ks` from `s.getOrCreateSpace()` (Lock released), then `resolveAgentName`, then `s.mu.Lock()` at line 1127 | ks.Agents read while another goroutine may be writing |
| 1182 | `handleAgentDocument` DELETE | `ks` from `s.getSpace()` (RLock released), then `resolveAgentName`, then `s.mu.Lock()` at line 1183 | ks.Agents read while another goroutine may be writing |

### SAFE — called inside lock

| Line | Handler | Lock held |
|------|---------|-----------|
| 1223 | `handleIgnition` POST | `s.mu.Lock()` at line 1222 |
| 1294 | `handleIgnition` (read section) | `s.mu.RLock()` at line 1240 (deferred) |
| 1516 | `handleApproveAgent` | `s.mu.RLock()` at line 1515 |
| 1568 | `handleReplyAgent` | `s.mu.RLock()` at line 1567 |
| 1637 | `handleDismissQuestion` | `s.mu.Lock()` at line 1636 |
| 2037 | `handleMessageAck` | `s.mu.Lock()` at line ~2030 (per inline comment) |

---

## Fix Pattern

For every unsafe site, move the `resolveAgentName` call to after the lock is acquired:

**Before (unsafe):**
```go
ks, ok := s.getSpace(spaceName)  // acquires and releases lock internally
canonical := resolveAgentName(ks, agentName)  // ks.Agents unprotected
s.mu.Lock()
// ... use canonical ...
s.mu.Unlock()
```

**After (safe):**
```go
ks, ok := s.getSpace(spaceName)
s.mu.Lock()
canonical := resolveAgentName(ks, agentName)  // ks.Agents protected
// ... use canonical ...
s.mu.Unlock()
```

For GET paths that use `RLock`:
```go
ks, ok := s.getSpace(spaceName)
s.mu.RLock()
canonical := resolveAgentName(ks, agentName)
agent, exists := ks.Agents[canonical]
s.mu.RUnlock()
```

### Line 811 (POST handleSpaceAgent) — special case

The POST path calls `s.getOrCreateSpace()` (which uses Lock), then calls
`resolveAgentName` without a lock, then much later acquires Lock at line 845. The
`resolveAgentName` call should move inside the `s.mu.Lock()` block at line 845. This is safe
because `canonical` is only used inside that block.

### Line 893 (DELETE handleSpaceAgent)

`resolveAgentName` at line 893 is called one line before `s.mu.Lock()` at line 894.
Moving it one line down (inside the lock) is a one-line fix.

---

## Race Detector Confirmation

Running `go test -race ./internal/coordinator/` under concurrent load will surface these
races as:

```
DATA RACE
Write at 0x... by goroutine ...:
  runtime.mapassign_faststr(...)
      internal/coordinator/server.go:XXX (inside s.mu.Lock block)

Previous read at 0x... by goroutine ...:
  coordinator.resolveAgentName(...)
      internal/coordinator/server.go:787 (without lock)
```

These are real races, not theoretical — `ks.Agents` is a `map[string]*AgentUpdate` and
concurrent map read+write is undefined behavior in Go.

---

## Priority

**High.** These are correctness bugs that will cause intermittent crashes (`concurrent map
read and map write` panic) under realistic multi-agent load. The fix for each site is 1-2
lines.
