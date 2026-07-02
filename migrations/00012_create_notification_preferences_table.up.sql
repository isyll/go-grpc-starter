-- Per-user notification preferences. One row per user, seeded lazily
-- by the notifications service on first read.
CREATE TABLE IF NOT EXISTS notifications.notification_preferences (
  user_id BIGINT PRIMARY KEY REFERENCES auth.users (id) ON DELETE CASCADE,
  push BOOLEAN NOT NULL DEFAULT TRUE,
  email BOOLEAN NOT NULL DEFAULT TRUE,
  marketing BOOLEAN NOT NULL DEFAULT FALSE,
  quiet_hours_enabled BOOLEAN NOT NULL DEFAULT FALSE,
  quiet_hours_start TIME,
  quiet_hours_end TIME,
  timezone VARCHAR(50) NOT NULL DEFAULT 'UTC',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

DROP TRIGGER IF EXISTS update_notification_preferences_updated_at ON notifications.notification_preferences;
CREATE TRIGGER update_notification_preferences_updated_at BEFORE UPDATE
ON notifications.notification_preferences FOR EACH ROW
EXECUTE FUNCTION public.update_updated_at_column();

-- Quiet hours require both a start and an end when enabled.
CREATE OR REPLACE FUNCTION notifications.validate_quiet_hours() RETURNS TRIGGER AS $$
BEGIN
    IF NEW.quiet_hours_enabled
       AND (NEW.quiet_hours_start IS NULL OR NEW.quiet_hours_end IS NULL) THEN
        RAISE EXCEPTION 'quiet_hours_start and quiet_hours_end are required when quiet hours are enabled';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS validate_notification_quiet_hours ON notifications.notification_preferences;
CREATE TRIGGER validate_notification_quiet_hours BEFORE INSERT OR UPDATE
ON notifications.notification_preferences FOR EACH ROW
EXECUTE FUNCTION notifications.validate_quiet_hours();

ALTER TABLE notifications.notification_preferences ENABLE ROW LEVEL SECURITY;
ALTER TABLE notifications.notification_preferences FORCE ROW LEVEL SECURITY;

CREATE POLICY notif_prefs_app_all ON notifications.notification_preferences
FOR ALL TO app_api USING (TRUE) WITH CHECK (TRUE);

CREATE POLICY notif_prefs_worker_all ON notifications.notification_preferences
FOR ALL TO app_worker USING (TRUE) WITH CHECK (TRUE);

CREATE POLICY notif_prefs_ro_select ON notifications.notification_preferences
FOR SELECT TO app_readonly USING (TRUE);
