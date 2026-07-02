DROP POLICY IF EXISTS notif_prefs_app_all ON notifications.notification_preferences;
DROP POLICY IF EXISTS notif_prefs_worker_all ON notifications.notification_preferences;
DROP POLICY IF EXISTS notif_prefs_ro_select ON notifications.notification_preferences;
DROP TABLE IF EXISTS notifications.notification_preferences;
DROP FUNCTION IF EXISTS notifications.validate_quiet_hours();
