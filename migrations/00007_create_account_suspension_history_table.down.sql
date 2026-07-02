DROP TRIGGER IF EXISTS trg_log_account_suspension_change
ON auth.account_suspensions;
DROP FUNCTION IF EXISTS log_account_suspension_change();

DROP TRIGGER IF EXISTS deny_account_suspension_history_delete
ON auth.account_suspension_history;
DROP FUNCTION IF EXISTS prevent_account_suspension_history_delete();

DROP TRIGGER IF EXISTS deny_account_suspension_history_update
ON auth.account_suspension_history;

DROP TABLE IF EXISTS auth.account_suspension_history;
