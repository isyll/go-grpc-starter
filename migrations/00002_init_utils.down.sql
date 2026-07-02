-- Reverses 000001_init_utils.up.sql. Drops every shared helper
-- function in reverse-creation order. Any table still depending on
-- these functions would have been dropped first by its own down
-- migration.
DROP FUNCTION IF EXISTS public.prevent_history_delete();

DROP FUNCTION IF EXISTS public.prevent_status_history_update();

DROP FUNCTION IF EXISTS public.update_updated_at_column();

DROP FUNCTION IF EXISTS public.prevent_update_created_at();

DROP FUNCTION IF EXISTS public.prevent_account_id_update();

DROP FUNCTION IF EXISTS public.generate_account_id();

DROP FUNCTION IF EXISTS public.validate_weekdays(JSONB);

DROP FUNCTION IF EXISTS public.validate_custom_dates(JSONB);
