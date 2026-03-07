# Event Journal Performance Spec

**Author:** SMEAgent
**Date:** 2026-03-07
**Status:** Ready for implementation

## Overview

Three targeted performance improvements to `internal/coordinator/journal.go`, ordered by impact.

---

## 1. File Handle Pooling

### Problem

`EventJournal.write()` currently opens and closes a file per `Append()` call:

```go
f, err := os.OpenFile(j.journalPath(ev.Space), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
defer f.Close()
```

Under high write frequency this creates:
- Syscall overhead: open + write + close per event
- File descriptor churn
- Potential EMFILE under load

### Design

Maintain one open `*os.File` per space, opened in append mode. The `EventJournal` struct gains a handle map protected by its existing mutex.

```go
type EventJournal struct {
    dataDir string
    mu      sync.Mutex
    seq     atomic.Int64
    handles map[string]*os.File  // space -> open file handle
}
```

**Initialization:** lazy-open on first write per space. If the file cannot be opened, fall back to the current open-per-call behavior (best-effort).

**Write path:**

```go
func (j *EventJournal) fileFor(space string) (*os.File, error) {
    // caller holds j.mu
    if f, ok := j.handles[space]; ok {
        return f, nil
    }
    f, err := os.OpenFile(j.journalPath(space), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
    if err != nil {
        return nil, err
    }
    j.handles[space] = f
    return f, nil
}
```

**Close policy:** handles are closed when `Compact()` rewrites the journal file (the old file is no longer valid) and on server shutdown. Add a `Close()` method to `EventJournal` called from `Server.Stop()`.

**Compaction:** must close and reopen the handle after `os.Rename(tmp, path)` to avoid writing to a stale file descriptor.

### Testing

- Verify writes are visible after handle is reused (no buffering at Go layer — `os.File` is unbuffered by default for O_WRONLY).
- Race detector test: concurrent `Append` calls across spaces must not deadlock or corrupt.
- After `Compact`, subsequent appends must land in the correct (new) file.

---

## 2. RLock for LoadSince

### Problem

`LoadSince` holds an exclusive `j.mu.Lock()` for the full scan:

```go
func (j *EventJournal) LoadSince(...) {
    j.mu.Lock()
    defer j.mu.Unlock()
    ...
}
```

This blocks all `Append` calls for the duration of a potentially long file scan. Reads do not need exclusive access — the file is append-only and the OS guarantees that a concurrent append does not corrupt an in-progress sequential read.

### Design

Replace `j.mu.Lock()` with `j.mu.RLock()` in `LoadSince`. The `EventJournal.mu` must become a `sync.RWMutex`.

```go
type EventJournal struct {
    ...
    mu      sync.RWMutex
    handles map[string]*os.File
}
```

**Append:** continues to hold `j.mu.Lock()` (write lock) because it mutates the handle map and writes to the file.

**LoadSince:** uses `j.mu.RLock()`. The file opened for reading is a separate `os.Open` call (read-only), so it does not conflict with the write handle.

**Compact:** keeps `j.mu.Lock()` because it mutates the handle map and replaces the file.

**Correctness:** A read that starts before a concurrent append may not see the new event — this is acceptable; the reader will see it on the next call. There is no torn-read risk because JSONL is line-delimited and the OS guarantees atomic line appends on local filesystems for writes <= PIPE_BUF (typically 4KB). For larger payloads, callers already accept best-effort ordering.

### Testing

- `go test -race`: concurrent `Append` + `LoadSince` calls must pass without data races.
- Verify that `LoadSince` returns all events written before it was called.

---

## 3. Count-Based Compaction Trigger

### Problem

Compaction currently runs on a fixed 30-minute timer (`livenessLoop`). This means:
- A quiet space is compacted unnecessarily.
- A high-traffic space may accumulate thousands of events between compactions, making replay expensive on restart.

### Design

Add an event counter per space to `EventJournal`. When the count crosses a threshold, trigger compaction automatically on the next `Append` call.

```go
type EventJournal struct {
    ...
    counts map[string]*atomic.Int64  // space -> events since last compact
}

const compactThreshold = 1000
```

**Append path** (pseudocode):

```go
func (j *EventJournal) Append(space string, ...) *SpaceEvent {
    ev := j.buildEvent(...)
    j.write(ev)  // holds j.mu write lock internally
    if j.counts[space].Add(1) >= compactThreshold {
        // compact inline — write lock already held inside Compact
        // pass current KnowledgeSpace snapshot from caller, or skip if unavailable
    }
}
```

**Problem:** `Append` does not have access to the `KnowledgeSpace` snapshot. Two options:

**Option A** — Separate goroutine trigger: `Append` signals a channel; a background goroutine reads the live `KnowledgeSpace` under `s.mu` and calls `Compact`. This decouples the append hot path from compaction.

**Option B** — Caller-supplied snapshot: add an optional `CompactFunc func() *KnowledgeSpace` field on `EventJournal`. When the count threshold is reached, call it to obtain the snapshot and compact inline.

**Recommendation:** Option A. It keeps `Append` non-blocking and ensures compaction always has access to the authoritative in-memory state. The background goroutine runs at most one compaction at a time per space (use a `sync.Mutex` per space or a single-item channel as a semaphore).

**Count reset:** reset the counter to 0 after each successful `Compact`.

**Interaction with time-based trigger:** keep the 30-minute timer as a safety net for spaces with low write volume. The count-based trigger handles high-traffic spaces.

### Testing

- Write 1001 events and verify that compaction fires automatically.
- After compaction, `LoadSince(space, time.Time{})` should return exactly 1 event (the snapshot).
- Verify count resets and subsequent events append correctly.

---

## Implementation Order

1. RLock for `LoadSince` — smallest change, highest lock-contention relief, no new data structures.
2. File handle pooling — moderate complexity, eliminates syscall overhead.
3. Count-based compaction — most complex, requires background goroutine wiring.

Each should be committed atomically with its own test coverage.
