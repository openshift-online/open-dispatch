-- Rollback Migration: Remove CHECK constraints and FK constraints from check-in tables
-- Purpose: Rollback script for check_in_constraints_migration.sql
-- Related: TASK-002 Critical Issue #3

-- Remove FK constraints first (order matters for dependencies)
ALTER TABLE check_in_events
DROP CONSTRAINT IF EXISTS fk_checkin_event_config;

ALTER TABLE agent_check_in_configs
DROP CONSTRAINT IF EXISTS fk_agent_checkin_config_agent;

-- Remove CHECK constraints from check_in_events table
ALTER TABLE check_in_events
DROP CONSTRAINT IF EXISTS chk_response_consistency;

ALTER TABLE check_in_events
DROP CONSTRAINT IF EXISTS chk_triggered_after_scheduled;

ALTER TABLE check_in_events
DROP CONSTRAINT IF EXISTS chk_latency_non_negative;

ALTER TABLE check_in_events
DROP CONSTRAINT IF EXISTS chk_retry_count_non_negative;

-- Remove CHECK constraints from agent_check_in_configs table
ALTER TABLE agent_check_in_configs
DROP CONSTRAINT IF EXISTS chk_retry_delay_seconds_positive;

ALTER TABLE agent_check_in_configs
DROP CONSTRAINT IF EXISTS chk_retry_attempts_non_negative;

ALTER TABLE agent_check_in_configs
DROP CONSTRAINT IF EXISTS chk_timeout_seconds_positive;

-- Note: This rollback does NOT remove indexes or tables, only the CHECK constraints
-- To fully rollback the check-in feature, use the full rollback migration below

-- FULL ROLLBACK (USE WITH CAUTION - DESTRUCTIVE):
-- DROP TABLE IF EXISTS check_in_events CASCADE;
-- DROP TABLE IF EXISTS agent_check_in_configs CASCADE;
-- DROP TABLE IF EXISTS check_in_scheduler_locks CASCADE;
