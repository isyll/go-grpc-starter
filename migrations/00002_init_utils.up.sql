-- Shared trigger functions and validators used across the schema.
-- These are the building blocks every other migration attaches to its
-- own tables: timestamp maintenance, immutability guards, account-id
-- generation, and JSONB date / weekday validation helpers.

-- update_updated_at_column is attached as a BEFORE UPDATE trigger on
-- every mutable table. It is the single implementation of the
-- updated_at invariant - no table manages this timestamp manually.
CREATE
OR REPLACE FUNCTION public.update_updated_at_column() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    NEW.updated_at := NOW();
    RETURN NEW;
END;
$$;

-- prevent_update_created_at guards the immutability of created_at.
-- Attached as a BEFORE UPDATE trigger so any attempt to overwrite the
-- column raises an exception before the row is written.
CREATE
OR REPLACE FUNCTION public.prevent_update_created_at() RETURNS TRIGGER AS $$
BEGIN
    IF NEW.created_at <> OLD.created_at THEN
        RAISE EXCEPTION 'created_at cannot be updated';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- generate_account_id is a BEFORE INSERT trigger that assigns a
-- human-readable 12-digit account_id (100000000000-999999999999).
-- It rejects patterned numbers to prevent IDs that look like phone
-- numbers or that are trivially guessable, retrying up to 20 times.
CREATE
OR REPLACE FUNCTION public.generate_account_id() RETURNS TRIGGER AS $$
DECLARE
    new_account_id BIGINT;
    id_text TEXT;
    max_attempts INT := 20;
    attempt INT := 0;
    digit_counts INT[];
    max_freq INT;
    i INT;
    seq_asc INT;
    seq_desc INT;
    prev_digit INT;
    curr_digit INT;
BEGIN
    -- Respect caller-supplied account_id (e.g. seed data, backfill).
    IF NEW.account_id IS NOT NULL AND NEW.account_id != 0 THEN
        RETURN NEW;
    END IF;

    LOOP
        attempt := attempt + 1;
        IF attempt > max_attempts THEN
            RAISE EXCEPTION
                'Failed to generate unique account_id after % attempts',
                max_attempts;
        END IF;

        new_account_id :=
            100000000000
            + floor(random() * 900000000000)::BIGINT;
        id_text := new_account_id::TEXT;

        -- Reject if any single digit appears 5+ times across the
        -- 12-character id (e.g. 113114115199).
        digit_counts := ARRAY[0,0,0,0,0,0,0,0,0,0];
        FOR i IN 1..12 LOOP
            curr_digit :=
                substring(id_text FROM i FOR 1)::INT;
            digit_counts[curr_digit + 1] :=
                digit_counts[curr_digit + 1] + 1;
        END LOOP;
        max_freq := 0;
        FOR i IN 1..10 LOOP
            IF digit_counts[i] > max_freq THEN
                max_freq := digit_counts[i];
            END IF;
        END LOOP;
        IF max_freq >= 5 THEN
            CONTINUE;
        END IF;

        -- Reject 5+ consecutive identical digits (e.g. 111115678901).
        seq_asc := 1;
        FOR i IN 2..12 LOOP
            IF substring(id_text FROM i FOR 1)
               = substring(id_text FROM i - 1 FOR 1)
            THEN
                seq_asc := seq_asc + 1;
                IF seq_asc >= 5 THEN
                    EXIT;
                END IF;
            ELSE
                seq_asc := 1;
            END IF;
        END LOOP;
        IF seq_asc >= 5 THEN
            CONTINUE;
        END IF;

        -- Reject ascending or descending runs of 6+ digits
        -- (e.g. 123456xxx, 987654xxx).
        seq_asc := 1;
        seq_desc := 1;
        prev_digit :=
            substring(id_text FROM 1 FOR 1)::INT;
        FOR i IN 2..12 LOOP
            curr_digit :=
                substring(id_text FROM i FOR 1)::INT;
            IF curr_digit = prev_digit + 1 THEN
                seq_asc := seq_asc + 1;
            ELSE
                seq_asc := 1;
            END IF;
            IF curr_digit = prev_digit - 1 THEN
                seq_desc := seq_desc + 1;
            ELSE
                seq_desc := 1;
            END IF;
            IF seq_asc >= 6 OR seq_desc >= 6 THEN
                EXIT;
            END IF;
            prev_digit := curr_digit;
        END LOOP;
        IF seq_asc >= 6 OR seq_desc >= 6 THEN
            CONTINUE;
        END IF;

        -- Reject a repeating 2-digit pattern spanning the whole id
        -- (e.g. 343434343434).
        IF substring(id_text FROM 1 FOR 2)
           = substring(id_text FROM 3 FOR 2)
           AND substring(id_text FROM 1 FOR 2)
           = substring(id_text FROM 5 FOR 2)
           AND substring(id_text FROM 1 FOR 2)
           = substring(id_text FROM 7 FOR 2)
           AND substring(id_text FROM 1 FOR 2)
           = substring(id_text FROM 9 FOR 2)
           AND substring(id_text FROM 1 FOR 2)
           = substring(id_text FROM 11 FOR 2)
        THEN
            CONTINUE;
        END IF;

        -- All quality checks passed; race-safe insert under the
        -- existing UNIQUE(account_id) constraint.
        BEGIN
            IF NOT EXISTS (
                SELECT 1 FROM auth.users
                WHERE account_id = new_account_id
            ) THEN
                NEW.account_id := new_account_id;
                RETURN NEW;
            END IF;
        EXCEPTION
            WHEN unique_violation THEN
                -- Concurrent insert hit the same id; retry.
                NULL;
        END;
    END LOOP;
END;
$$ LANGUAGE plpgsql;

-- prevent_account_id_update enforces immutability of account_id after
-- creation. account_id is the user-facing identifier shared with
-- support and used in referral flows - it must never change.
CREATE
OR REPLACE FUNCTION public.prevent_account_id_update() RETURNS TRIGGER AS $$
BEGIN
    IF NEW.account_id <> OLD.account_id THEN
        RAISE EXCEPTION 'account_id is immutable and cannot be updated';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- validate_weekdays checks that a JSONB value is an array of integers
-- in [1,7] where 1=Monday and 7=Sunday (ISO 8601 convention). Used in
-- CHECK constraints on trip_patterns.weekdays.
CREATE
OR REPLACE FUNCTION public.validate_weekdays(weekdays JSONB) RETURNS BOOLEAN AS $$
DECLARE
    day INTEGER;
    array_length INTEGER;
BEGIN
    -- Reject anything that is not a JSON array up front.
    IF jsonb_typeof(weekdays) != 'array' THEN
        RETURN FALSE;
    END IF;

    array_length := jsonb_array_length(weekdays);

    -- 0 to 7 elements; "empty" is meaningful (no recurrence applied).
    IF array_length < 0 OR array_length > 7 THEN
        RETURN FALSE;
    END IF;

    IF array_length = 0 THEN
        RETURN TRUE;
    END IF;

    -- Every element must be in the ISO 8601 day range.
    FOR day IN SELECT jsonb_array_elements_text(weekdays)::INTEGER
    LOOP
        IF day < 1 OR day > 7 THEN
            RETURN FALSE;
        END IF;
    END LOOP;

    RETURN TRUE;
EXCEPTION
    WHEN OTHERS THEN
        RETURN FALSE;
END;
$$ LANGUAGE plpgsql IMMUTABLE;

-- validate_custom_dates checks that a JSONB value is an array of
-- ISO 8601 date strings (YYYY-MM-DD), each not in the past and not
-- more than 2 years ahead, with no duplicates. Used in CHECK
-- constraints on trip_patterns.custom_dates and excluded_dates.
CREATE
OR REPLACE FUNCTION public.validate_custom_dates(custom_dates JSONB) RETURNS BOOLEAN AS $$
DECLARE
    date_str TEXT;
    date_value DATE;
    date_count INTEGER;
    max_length INTEGER := 365;
    max_date DATE := CURRENT_DATE + INTERVAL '2 years';
BEGIN
    IF jsonb_typeof(custom_dates) != 'array' THEN
        RAISE EXCEPTION 'custom_dates must be a JSON array';
    END IF;

    date_count := jsonb_array_length(custom_dates);

    -- Hard cap to keep validation O(N) and CHECK evaluation cheap.
    IF date_count > max_length THEN
        RAISE EXCEPTION 'custom_dates cannot contain more than % dates', max_length;
    END IF;

    FOR date_str IN SELECT jsonb_array_elements_text(custom_dates)
    LOOP
        -- DATE cast raises invalid_datetime_format for malformed
        -- strings; the handler below converts it to a friendly error.
        date_value := date_str::DATE;

        IF date_value < CURRENT_DATE THEN
            RAISE EXCEPTION 'The date % is in the past', date_str;
        END IF;

        IF date_value > max_date THEN
            RAISE EXCEPTION 'The date % is too far in the future (max %)', date_str, max_date;
        END IF;
    END LOOP;

    -- Duplicate detection: DISTINCT count must match raw count.
    IF date_count != (SELECT COUNT(DISTINCT elem) FROM jsonb_array_elements(custom_dates) AS elem) THEN
        RAISE EXCEPTION 'custom_dates contains duplicates';
    END IF;

    RETURN TRUE;
EXCEPTION
    WHEN invalid_datetime_format THEN
        RAISE EXCEPTION 'Invalid date format. Use the format YYYY-MM-DD';
    WHEN OTHERS THEN
        RAISE;
END;
$$ LANGUAGE plpgsql IMMUTABLE;

COMMENT ON FUNCTION public.update_updated_at_column() IS 'BEFORE UPDATE trigger body that sets updated_at = NOW(). Attached to every mutable table that carries an updated_at column.';

COMMENT ON FUNCTION public.prevent_update_created_at() IS 'BEFORE UPDATE trigger body that raises an exception if created_at is changed. Enforces the immutability invariant: a row creation timestamp is a fact, not a preference.';

COMMENT ON FUNCTION public.generate_account_id() IS 'BEFORE INSERT trigger that generates a unique 12-digit account_id in [100000000000, 999999999999]. Rejects IDs with 5+ identical digits, 5+ consecutive identical digits, 6+ ascending or descending runs, or a repeating 2-digit pattern, to avoid IDs that resemble phone numbers or are trivially guessable. Retries up to 20 times.';

COMMENT ON FUNCTION public.prevent_account_id_update() IS 'BEFORE UPDATE trigger body that raises an exception if account_id is changed. account_id is the user-facing identifier - it must never change after creation.';

COMMENT ON FUNCTION public.validate_weekdays(JSONB) IS 'Returns TRUE when the input is a JSONB array of integers in [1,7] (1=Monday, 7=Sunday, ISO 8601). Used in CHECK constraints on trip_patterns.weekdays. IMMUTABLE.';

COMMENT ON FUNCTION public.validate_custom_dates(JSONB) IS 'Returns TRUE when the input is a JSONB array of YYYY-MM-DD date strings, each not in the past, not more than 2 years ahead, and with no duplicates. Raises descriptive exceptions for individual invalid values. Used in CHECK constraints on trip_patterns custom_dates and excluded_dates. IMMUTABLE.';

-- prevent_status_history_update is a BEFORE UPDATE trigger body shared
-- by every `*_status_history` and `*_role_history` audit table in the
-- schema. Status-history rows are append-only - a corrective entry is
-- INSERTed, never an UPDATE - so the trigger raises unconditionally.
-- Lives here (not next to a single history table) because eleven
-- migrations reference it.
CREATE OR REPLACE FUNCTION public.prevent_status_history_update() RETURNS TRIGGER AS $$
BEGIN
    RAISE EXCEPTION
        'Updates to status-history tables are not allowed. '
        'History rows are immutable - insert a corrective row instead.';
    RETURN NULL;
END;
$$ LANGUAGE plpgsql;

-- prevent_history_delete is a BEFORE DELETE trigger body shared by the
-- append-only status-history tables. Physical
-- deletion of history rows is rejected so the audit trail can never be
-- tampered with. Per-table history tables that need a different error
-- message (e.g. auth.user_status_history) define their own variant.
CREATE OR REPLACE FUNCTION public.prevent_history_delete() RETURNS TRIGGER AS $$
BEGIN
    RAISE EXCEPTION
        'Deletion of history records not allowed. '
        'History is an immutable audit trail.';
    RETURN NULL;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION public.prevent_status_history_update() IS 'BEFORE UPDATE trigger body shared by every *_status_history and *_role_history audit table. Raises unconditionally - history rows are append-only and corrective entries must be inserted as new rows.';

COMMENT ON FUNCTION public.prevent_history_delete() IS 'BEFORE DELETE trigger body shared by rides.trip_status_history and rides.booking_status_history. Raises unconditionally - history rows must never be physically deleted; the audit trail is permanent.';
