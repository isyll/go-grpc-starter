DROP INDEX IF EXISTS idx_outbox_exhausted;
DROP INDEX IF EXISTS idx_outbox_payload;
DROP INDEX IF EXISTS idx_outbox_unprocessed;

-- Reverses 000050_create_events_outbox_table.up.sql by dropping RLS
-- policies, the immutability and once-stamped-only triggers and their
-- functions, the lifecycle trigger, and the outbox table.

-- Drop policies before disabling RLS.
DROP POLICY IF EXISTS outbox_app_insert ON events.outbox;
DROP POLICY IF EXISTS outbox_admin_all ON events.outbox;
DROP POLICY IF EXISTS outbox_ro_select ON events.outbox;
ALTER TABLE events.outbox DISABLE ROW LEVEL SECURITY;

DROP TRIGGER IF EXISTS prevent_outbox_processed_at_clear ON events.outbox;
DROP FUNCTION IF EXISTS events.prevent_outbox_processed_at_clear();
DROP TRIGGER IF EXISTS protect_outbox_immutable ON events.outbox;
DROP FUNCTION IF EXISTS events.protect_outbox_immutable_fields();
DROP TRIGGER IF EXISTS prevent_outbox_created_at_update ON events.outbox;
DROP TABLE IF EXISTS events.outbox;
