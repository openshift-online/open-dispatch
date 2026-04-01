-- Verification Script: Check-in System Database Indexes
-- Purpose: Verify all required indexes exist for the check-in feature
-- Related: TASK-002 Critical Issue #3

-- List all indexes on check-in tables (PostgreSQL)
SELECT
    tablename,
    indexname,
    indexdef
FROM pg_indexes
WHERE tablename IN ('agent_check_in_configs', 'check_in_events', 'check_in_scheduler_locks')
ORDER BY tablename, indexname;

-- Expected indexes (13 total):

-- agent_check_in_configs (4 indexes):
-- 1. agent_check_in_configs_pkey (PRIMARY KEY on id)
-- 2. idx_checkin_space_agent (UNIQUE on space_name, agent_name)
-- 3. idx_checkin_enabled (PARTIAL INDEX WHERE check_in_enabled = true)
-- 4. idx_agent_check_in_configs_space_name (on space_name)

-- check_in_events (7 indexes):
-- 5. check_in_events_pkey (PRIMARY KEY on id)
-- 6. idx_event_space_agent (on space_name, agent_name)
-- 7. idx_event_agent_time (on agent_name, triggered_at DESC)
-- 8. idx_pending_checkins (PARTIAL INDEX WHERE response_received = false AND message_sent = true)
-- 9. idx_check_in_events_space_name (on space_name)
-- 10. idx_check_in_events_agent_name (on agent_name)
-- 11. idx_check_in_events_message_id (on message_id)

-- check_in_scheduler_locks (3 indexes):
-- 12. check_in_scheduler_locks_pkey (PRIMARY KEY on id)
-- 13. idx_check_in_scheduler_locks_locked_at (on locked_at)
-- 14. idx_check_in_scheduler_locks_expires_at (on expires_at)

-- Total: 14 indexes (including 3 primary keys)
-- Partial indexes: 2 (idx_checkin_enabled, idx_pending_checkins)
-- Unique indexes: 1 (idx_checkin_space_agent)

-- Verify partial indexes specifically:
SELECT
    schemaname,
    tablename,
    indexname,
    indexdef
FROM pg_indexes
WHERE tablename IN ('agent_check_in_configs', 'check_in_events')
  AND indexdef LIKE '%WHERE%'
ORDER BY tablename, indexname;

-- Expected output:
-- agent_check_in_configs | idx_checkin_enabled | ... WHERE check_in_enabled = true
-- check_in_events | idx_pending_checkins | ... WHERE response_received = false AND message_sent = true

-- Verify CHECK constraints:
SELECT
    conrelid::regclass AS table_name,
    conname AS constraint_name,
    pg_get_constraintdef(oid) AS constraint_definition
FROM pg_constraint
WHERE conrelid IN (
    'agent_check_in_configs'::regclass,
    'check_in_events'::regclass
)
AND contype = 'c'
ORDER BY conrelid::regclass::text, conname;

-- Expected CHECK constraints (7 total):
-- agent_check_in_configs | chk_timeout_seconds_positive | CHECK (timeout_seconds >= 0)
-- agent_check_in_configs | chk_retry_attempts_non_negative | CHECK (retry_attempts >= 0)
-- agent_check_in_configs | chk_retry_delay_seconds_positive | CHECK (retry_delay_seconds >= 0)
-- check_in_events | chk_retry_count_non_negative | CHECK (retry_count >= 0)
-- check_in_events | chk_latency_non_negative | CHECK (response_latency_ms IS NULL OR response_latency_ms >= 0)
-- check_in_events | chk_triggered_after_scheduled | CHECK (triggered_at >= scheduled_at OR triggered_at = '0001-01-01 00:00:00')
-- check_in_events | chk_response_consistency | CHECK ((response_received = FALSE AND response_at IS NULL) OR (response_received = TRUE AND response_at IS NOT NULL))

-- Note: Cron schedule regex validation is enforced at API layer (handlers_checkin.go:104-109)
-- using robfig/cron parser. Database CHECK constraints with regex cause performance issues.

-- Verify foreign key constraints:
SELECT
    conrelid::regclass AS table_name,
    conname AS constraint_name,
    pg_get_constraintdef(oid) AS constraint_definition
FROM pg_constraint
WHERE conrelid IN (
    'agent_check_in_configs'::regclass,
    'check_in_events'::regclass
)
AND contype = 'f'
ORDER BY conrelid::regclass::text, conname;

-- Expected FK constraints (2 total):
-- agent_check_in_configs | fk_agent_checkin_config_agent | FOREIGN KEY (space_name, agent_name) REFERENCES agents(space_name, agent_name) ON DELETE CASCADE
-- check_in_events | fk_checkin_event_config | FOREIGN KEY (space_name, agent_name) REFERENCES agent_check_in_configs(space_name, agent_name) ON DELETE CASCADE

-- For SQLite (used in development):
-- SQLite stores index info differently, use:
-- SELECT name, sql FROM sqlite_master WHERE type='index' AND tbl_name IN ('agent_check_in_configs', 'check_in_events', 'check_in_scheduler_locks');
-- For FK constraints in SQLite:
-- PRAGMA foreign_key_list('agent_check_in_configs');
-- PRAGMA foreign_key_list('check_in_events');
