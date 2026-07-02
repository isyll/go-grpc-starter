-- Per-attempt log of every push notification dispatched. Written by
-- the push-notification worker (app_worker); end users read only
-- their own delivery log. Soft chronology constraints guarantee that
-- click / dismiss timestamps cannot precede sent_at.
--
-- Range-partitioned monthly by sent_at and managed by pg_partman.
-- The partition key (sent_at) is part of the composite primary key
-- because PostgreSQL requires the partition key to appear in every
-- unique or primary key constraint on a partitioned table. Future
-- partitions are pre-created by the pg-partman-daily-maintenance
-- cron job registered in infra/postgres/init-schema.sh.
DO $$ BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_type t
            JOIN pg_namespace n ON t.typnamespace = n.oid
        WHERE t.typname = 'notification_status'
            AND n.nspname = 'notifications'
    ) THEN
        CREATE TYPE notifications.notification_status AS ENUM('sent', 'failed', 'clicked', 'dismissed');
    END IF;
END $$;

COMMENT ON TYPE notifications.notification_status IS
'Lifecycle states for a notification attempt. failed records dispatch errors; clicked/dismissed record user interaction.';

CREATE TABLE IF NOT EXISTS notifications.notification_logs (
  id BIGSERIAL NOT NULL,
  user_id BIGINT REFERENCES auth.users (id) ON DELETE SET NULL,
  event_type VARCHAR(50) NOT NULL,
  event_id VARCHAR(100),
  fcm_token_id BIGINT REFERENCES auth.fcm_tokens (id) ON DELETE SET NULL,
  status notifications.notification_status NOT NULL,
  error_code VARCHAR(50),
  error_message TEXT,
  payload JSONB,
  sent_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  clicked_at TIMESTAMPTZ,
  dismissed_at TIMESTAMPTZ,
  CONSTRAINT chk_notification_logs_clicked_at_order CHECK (
    clicked_at IS NULL
    OR clicked_at >= sent_at
  ),
  CONSTRAINT chk_notification_logs_dismissed_at_order CHECK (
    dismissed_at IS NULL
    OR dismissed_at >= sent_at
  ),
  CONSTRAINT chk_notification_logs_clicked_status_timestamp CHECK (
    status <> 'clicked'
    OR clicked_at IS NOT NULL
  ),
  PRIMARY KEY (id, sent_at)
) PARTITION BY RANGE (sent_at);

-- Register the table with pg_partman. p_premake=3 pre-creates the
-- current month plus the next 3 months as concrete child partitions.
-- A DEFAULT partition is created automatically to catch rows whose
-- sent_at falls outside the managed range.
SELECT public.create_parent(
  p_parent_table => 'notifications.notification_logs',
  p_control => 'sent_at',
  p_interval => '1 month',
  p_premake => 3
);

-- Indexes propagate from the partitioned parent to every existing
-- and future child partition (PostgreSQL 13+).

CREATE INDEX IF NOT EXISTS idx_notification_logs_user_id ON notifications.notification_logs (user_id);

-- Per-event-type dashboards order by sent_at DESC.
CREATE INDEX IF NOT EXISTS idx_notification_logs_event_type ON notifications.notification_logs (event_type, sent_at DESC);

-- Status / sent_at composite powers backoffice queues.
CREATE INDEX IF NOT EXISTS idx_notification_logs_status ON notifications.notification_logs (status, sent_at DESC);

CREATE INDEX IF NOT EXISTS idx_notification_logs_fcm_token_id ON notifications.notification_logs (fcm_token_id);

CREATE INDEX IF NOT EXISTS idx_notification_logs_sent_at ON notifications.notification_logs (sent_at DESC);

CREATE INDEX IF NOT EXISTS idx_notification_logs_event_id ON notifications.notification_logs (event_id);

CREATE INDEX IF NOT EXISTS idx_notification_logs_user_status ON notifications.notification_logs (user_id, status, sent_at DESC);

-- Failure investigation hot path: failures only.
CREATE INDEX IF NOT EXISTS idx_notification_logs_failed ON notifications.notification_logs (error_code, sent_at DESC)
WHERE
status = 'failed';

CREATE INDEX IF NOT EXISTS idx_notification_logs_payload ON notifications.notification_logs USING GIN (payload);

-- Domain trigger: enforce log data consistency and auto-stamp
-- clicked_at / dismissed_at when the status transitions to those
-- values without an explicit timestamp.
CREATE
OR REPLACE FUNCTION validate_notification_log() RETURNS TRIGGER AS $$
BEGIN
    -- 'failed' requires an error_code so the failure can be classified.
    IF NEW.status = 'failed' AND NEW.error_code IS NULL THEN
        RAISE EXCEPTION 'error_code must be set when status is failed';
    END IF;

    -- Auto-stamp clicked_at when status is set to 'clicked'.
    IF NEW.status = 'clicked' AND NEW.clicked_at IS NULL THEN
        NEW.clicked_at = NOW();
    END IF;

    -- Auto-stamp dismissed_at when status is set to 'dismissed'.
    IF NEW.status = 'dismissed' AND NEW.dismissed_at IS NULL THEN
        NEW.dismissed_at = NOW();
    END IF;

    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Row-level triggers on the partitioned parent fire for operations on
-- every child partition (PostgreSQL 13+).
DROP TRIGGER IF EXISTS trigger_validate_notification_log ON notifications.notification_logs;

CREATE TRIGGER trigger_validate_notification_log BEFORE INSERT
OR
UPDATE ON notifications.notification_logs FOR EACH ROW
EXECUTE FUNCTION validate_notification_log();

COMMENT ON TABLE notifications.notification_logs IS 'Per-attempt log of every push notification dispatched. Writes flow through the push-notification worker (app_worker); end users read their own delivery log via RLS. Range-partitioned monthly by sent_at; pg_partman creates future partitions via the daily cron maintenance job.';

COMMENT ON COLUMN notifications.notification_logs.user_id IS 'Recipient user. SET NULL on user deletion to preserve historical delivery counts. NULL means a system-wide / broadcast notification.';

COMMENT ON COLUMN notifications.notification_logs.event_type IS 'Type of event that triggered the notification (e.g. user.welcome). Mirrors notification_templates.event_type.';

COMMENT ON COLUMN notifications.notification_logs.event_id IS 'Unique identifier for the originating domain event, used to deduplicate retries.';

COMMENT ON COLUMN notifications.notification_logs.fcm_token_id IS 'Reference to the FCM token used to deliver the message. SET NULL on token deletion.';

COMMENT ON COLUMN notifications.notification_logs.status IS 'Delivery outcome: sent → clicked or dismissed (UPDATE allowed by the FCM-callback handler). Failures set status=failed with an error_code.';

COMMENT ON COLUMN notifications.notification_logs.error_code IS 'Short error code when status is failed (e.g. fcm/unregistered).';

COMMENT ON COLUMN notifications.notification_logs.error_message IS 'Detailed error message when status is failed.';

COMMENT ON COLUMN notifications.notification_logs.payload IS 'Complete FCM payload that was sent. Indexed with GIN for ad-hoc queries.';

COMMENT ON COLUMN notifications.notification_logs.sent_at IS 'Timestamp of the dispatch attempt. Also the range-partition key; included in the composite primary key per PostgreSQL partitioning rules.';

COMMENT ON COLUMN notifications.notification_logs.clicked_at IS 'Timestamp the user clicked the notification. CHECK enforces >= sent_at.';

COMMENT ON COLUMN notifications.notification_logs.dismissed_at IS 'Timestamp the user dismissed the notification. CHECK enforces >= sent_at.';

COMMENT ON FUNCTION validate_notification_log() IS 'BEFORE INSERT OR UPDATE trigger on notifications.notification_logs. Enforces that sent_at, clicked_at, and dismissed_at respect chronological order and that transitions between statuses only move forward.';

-- Row-level security. user_id is nullable (system notifications
-- have no specific recipient). The owner sees only their own
-- delivery log; writes are reserved for the push-notification
-- worker (app_worker) which inserts every dispatch attempt.
-- Policies on the partitioned parent apply to all queries routed
-- through the parent table name.
ALTER TABLE notifications.notification_logs ENABLE ROW LEVEL SECURITY;
ALTER TABLE notifications.notification_logs FORCE ROW LEVEL SECURITY;

CREATE POLICY notification_logs_owner_select ON notifications.notification_logs
FOR SELECT TO app_api
USING (TRUE);

CREATE POLICY notification_logs_admin_all ON notifications.notification_logs
FOR ALL TO app_worker USING (TRUE) WITH CHECK (TRUE);

CREATE POLICY notification_logs_ro_select ON notifications.notification_logs
FOR SELECT TO app_readonly USING (TRUE);

COMMENT ON POLICY notification_logs_owner_select ON notifications.notification_logs IS
'A user reads their own delivery log. Writes flow exclusively through the push-notification worker (app_worker).';

COMMENT ON POLICY notification_logs_admin_all ON notifications.notification_logs IS
'app_worker (push worker) has full access to insert attempts and query failures for retry and monitoring.';

COMMENT ON POLICY notification_logs_ro_select ON notifications.notification_logs IS
'app_readonly reads all notification attempts for monitoring dashboards.';
