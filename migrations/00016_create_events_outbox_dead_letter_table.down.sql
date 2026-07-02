-- Reverses 000051_create_events_outbox_dead_letter_table.up.sql by
-- dropping the admin-only RLS policies and the dead-letter table.

-- Drop policies before disabling RLS.
DROP POLICY IF EXISTS outbox_dl_admin_all ON events.outbox_dead_letter;
DROP POLICY IF EXISTS outbox_dl_ro_select ON events.outbox_dead_letter;
ALTER TABLE events.outbox_dead_letter DISABLE ROW LEVEL SECURITY;

DROP INDEX IF EXISTS idx_outbox_dead_letter_reason_failed;

DROP TABLE IF EXISTS events.outbox_dead_letter;
