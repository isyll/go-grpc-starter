-- Push notification templates, one row per event type, with
-- per-language title and body copy.
CREATE TABLE IF NOT EXISTS notifications.notification_templates (
  id SERIAL PRIMARY KEY,
  event_type VARCHAR(100) NOT NULL UNIQUE,
  icon VARCHAR(100),
  sound VARCHAR(50) NOT NULL DEFAULT 'default',
  priority VARCHAR(20) NOT NULL DEFAULT 'normal',
  android_channel_id VARCHAR(100),
  action VARCHAR(100),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS notifications.notification_template_translations (
  id SERIAL PRIMARY KEY,
  template_id INTEGER NOT NULL
    REFERENCES notifications.notification_templates (id) ON DELETE CASCADE,
  language VARCHAR(10) NOT NULL,
  title VARCHAR(200) NOT NULL,
  body VARCHAR(500) NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (template_id, language)
);

DROP TRIGGER IF EXISTS update_notification_templates_updated_at ON notifications.notification_templates;
CREATE TRIGGER update_notification_templates_updated_at BEFORE UPDATE
ON notifications.notification_templates FOR EACH ROW
EXECUTE FUNCTION public.update_updated_at_column();

-- One generic example template. Replace with your own event types.
INSERT INTO notifications.notification_templates (event_type, action)
VALUES ('user.welcome', 'open_app')
ON CONFLICT (event_type) DO NOTHING;

INSERT INTO notifications.notification_template_translations (template_id, language, title, body)
SELECT id, 'en', 'Welcome', 'Welcome to {app_name}!'
FROM notifications.notification_templates WHERE event_type = 'user.welcome'
ON CONFLICT (template_id, language) DO NOTHING;

ALTER TABLE notifications.notification_templates ENABLE ROW LEVEL SECURITY;
ALTER TABLE notifications.notification_templates FORCE ROW LEVEL SECURITY;
ALTER TABLE notifications.notification_template_translations ENABLE ROW LEVEL SECURITY;
ALTER TABLE notifications.notification_template_translations FORCE ROW LEVEL SECURITY;

CREATE POLICY notif_templates_app_read ON notifications.notification_templates
FOR SELECT TO app_api, app_worker, app_readonly USING (TRUE);
CREATE POLICY notif_templates_worker_write ON notifications.notification_templates
FOR ALL TO app_worker USING (TRUE) WITH CHECK (TRUE);

CREATE POLICY notif_tpl_tr_app_read ON notifications.notification_template_translations
FOR SELECT TO app_api, app_worker, app_readonly USING (TRUE);
CREATE POLICY notif_tpl_tr_worker_write ON notifications.notification_template_translations
FOR ALL TO app_worker USING (TRUE) WITH CHECK (TRUE);
