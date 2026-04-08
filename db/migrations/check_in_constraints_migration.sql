-- Migration: Add CHECK constraints to check-in tables
-- Purpose: Enforce data integrity constraints identified in code review
-- Related: TASK-002 Critical Issue #3

-- Add CHECK constraints to agent_check_in_configs table
ALTER TABLE agent_check_in_configs
ADD CONSTRAINT chk_timeout_seconds_positive
CHECK (timeout_seconds >= 0);

ALTER TABLE agent_check_in_configs
ADD CONSTRAINT chk_retry_attempts_non_negative
CHECK (retry_attempts >= 0);

ALTER TABLE agent_check_in_configs
ADD CONSTRAINT chk_retry_delay_seconds_positive
CHECK (retry_delay_seconds >= 0);

-- Add CHECK constraints to check_in_events table
ALTER TABLE check_in_events
ADD CONSTRAINT chk_retry_count_non_negative
CHECK (retry_count >= 0);

ALTER TABLE check_in_events
ADD CONSTRAINT chk_latency_non_negative
CHECK (response_latency_ms IS NULL OR response_latency_ms >= 0);

ALTER TABLE check_in_events
ADD CONSTRAINT chk_triggered_after_scheduled
CHECK (triggered_at >= scheduled_at OR triggered_at = '0001-01-01 00:00:00');

-- Add response consistency constraint (ensure response_received matches response_at state)
ALTER TABLE check_in_events
ADD CONSTRAINT chk_response_consistency
CHECK (
    (response_received = FALSE AND response_at IS NULL) OR
    (response_received = TRUE AND response_at IS NOT NULL)
);

-- Note: Cron schedule regex validation is enforced at API layer using robfig/cron parser
-- PostgreSQL CHECK constraints with regex are limited and can cause performance issues
-- See handlers_checkin.go lines 104-109 for cron validation via cron.NewParser()

-- Add foreign key constraints for referential integrity and CASCADE cleanup
ALTER TABLE agent_check_in_configs
ADD CONSTRAINT fk_agent_checkin_config_agent
FOREIGN KEY (space_name, agent_name)
REFERENCES agents(space_name, agent_name)
ON DELETE CASCADE;

ALTER TABLE check_in_events
ADD CONSTRAINT fk_checkin_event_config
FOREIGN KEY (space_name, agent_name)
REFERENCES agent_check_in_configs(space_name, agent_name)
ON DELETE CASCADE;

-- Verify indexes exist (these should be auto-created by GORM, but we verify)
-- Partial indexes:
-- 1. idx_checkin_enabled on agent_check_in_configs WHERE check_in_enabled = true
-- 2. idx_pending_checkins on check_in_events WHERE response_received = false AND message_sent = true

-- Composite indexes:
-- 3. idx_checkin_space_agent on agent_check_in_configs (space_name, agent_name) - unique
-- 4. idx_event_space_agent on check_in_events (space_name, agent_name)
-- 5. idx_event_agent_time on check_in_events (agent_name, triggered_at DESC)

-- Regular indexes:
-- 6. agent_check_in_configs.space_name
-- 7. check_in_events.space_name
-- 8. check_in_events.agent_name
-- 9. check_in_events.message_id
-- 10. check_in_scheduler_locks.locked_at
-- 11. check_in_scheduler_locks.expires_at

-- Query to verify all indexes are created:
-- SELECT tablename, indexname, indexdef FROM pg_indexes
-- WHERE tablename IN ('agent_check_in_configs', 'check_in_events', 'check_in_scheduler_locks')
-- ORDER BY tablename, indexname;

-- Query to verify CHECK constraints:
-- SELECT conname, contype, pg_get_constraintdef(oid)
-- FROM pg_constraint
-- WHERE conrelid IN (
--   'agent_check_in_configs'::regclass,
--   'check_in_events'::regclass
-- ) AND contype = 'c'
-- ORDER BY conrelid::regclass::text, conname;
