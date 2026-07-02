-- Reverses 000046_create_notification_logs_table.up.sql. Drops the
-- pg_partman registration, RLS policies, validation trigger and its
-- function, the partitioned table (CASCADE removes child partitions
-- and the template table), and the notification status enum type.

-- Remove the pg_partman management entry so the daily maintenance job
-- no longer tries to create partitions for a table being dismantled.
DELETE FROM public.part_config
WHERE parent_table = 'notifications.notification_logs';
DELETE FROM public.part_config_sub
WHERE sub_parent = 'notifications.notification_logs';

-- Drop policies before disabling RLS.
DROP POLICY IF EXISTS notification_logs_owner_select ON notifications.notification_logs;
DROP POLICY IF EXISTS notification_logs_admin_all ON notifications.notification_logs;
DROP POLICY IF EXISTS notification_logs_ro_select ON notifications.notification_logs;
ALTER TABLE notifications.notification_logs DISABLE ROW LEVEL SECURITY;

-- CASCADE removes every child partition and the pg_partman template table.
DROP TABLE IF EXISTS notifications.notification_logs CASCADE;

DROP FUNCTION IF EXISTS validate_notification_log();

DROP TYPE IF EXISTS notifications.notification_status;
