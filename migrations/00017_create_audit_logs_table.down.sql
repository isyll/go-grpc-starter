DROP POLICY IF EXISTS audit_logs_worker_all ON audit.audit_logs;
DROP POLICY IF EXISTS audit_logs_app_select ON audit.audit_logs;
DROP POLICY IF EXISTS audit_logs_ro_select ON audit.audit_logs;
DROP TABLE IF EXISTS audit.audit_logs;
DROP FUNCTION IF EXISTS audit.prevent_audit_logs_change();
