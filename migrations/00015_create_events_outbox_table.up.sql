-- At-least-once delivery buffer for async event handlers.
-- A row is written before each Asynq enqueue attempt and stamped with
-- processed_at when all enqueues succeed. The drain goroutine retries rows
-- where processed_at IS NULL and retry_count < 10.
CREATE TABLE IF NOT EXISTS events.outbox (
  id BIGSERIAL PRIMARY KEY,
  event_type TEXT NOT NULL CHECK (event_type <> ''),
  payload JSONB NOT NULL,
  -- Caller-supplied deduplication key. Producers that can collapse
  -- duplicate publishes (e.g. an idempotent service that may re-run
  -- under retry) populate this; the partial unique index below
  -- prevents two pending rows with the same (event_type, dedupe_key)
  -- from existing simultaneously. NULL = opt-out, no dedup.
  dedupe_key TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  processed_at TIMESTAMPTZ,
  retry_count INT NOT NULL DEFAULT 0 CHECK (retry_count >= 0),
  last_error TEXT,
  last_attempted_at TIMESTAMPTZ,
  CONSTRAINT chk_outbox_processed_at_order CHECK (
    processed_at IS NULL
    OR processed_at >= created_at
  )
);

-- Drain goroutine hot path: pick the oldest rows whose backoff window has
-- elapsed. last_attempted_at NULLS FIRST ensures freshly-written rows
-- (last_attempted_at IS NULL) sort ahead of retried ones.
CREATE INDEX IF NOT EXISTS idx_outbox_unprocessed
ON events.outbox (last_attempted_at NULLS FIRST, created_at ASC)
WHERE processed_at IS NULL;

-- Backoff schedule, in code (events.outboxBackoffFor):
--   retry_count = 0  -> 0 s    (drain ASAP after publish)
--   retry_count = 1  -> 30 s
--   retry_count = 2  -> 2 min
--   retry_count = 3  -> 10 min
--   retry_count = 4  -> 30 min
--   retry_count = 5+ -> 1 h    (capped)
-- Total exposure with 10 retries: ~6 hours before dead-letter.

CREATE INDEX IF NOT EXISTS idx_outbox_payload ON events.outbox USING GIN (payload);

-- Deduplication guard: collapse double-publishes from buggy services
-- before they reach the drain. A pending row (processed_at IS NULL)
-- with the same (event_type, dedupe_key) cannot be inserted twice.
-- Once the original is processed, a fresh publish is allowed - the
-- caller decides whether to reuse the same key.
CREATE UNIQUE INDEX IF NOT EXISTS ux_outbox_pending_dedupe
ON events.outbox (event_type, dedupe_key)
WHERE dedupe_key IS NOT NULL AND processed_at IS NULL;

-- Monitoring: rows the drain gave up on after 10 retries.
CREATE INDEX IF NOT EXISTS idx_outbox_exhausted ON events.outbox (created_at DESC)
WHERE processed_at IS NULL
AND retry_count >= 10;

DROP TRIGGER IF EXISTS prevent_outbox_created_at_update ON events.outbox;

CREATE TRIGGER prevent_outbox_created_at_update
BEFORE UPDATE ON events.outbox FOR EACH ROW EXECUTE FUNCTION prevent_update_created_at();

-- event_type and payload are write-once after insert.
CREATE OR REPLACE FUNCTION events.protect_outbox_immutable_fields() RETURNS TRIGGER AS $$
BEGIN
    IF NEW.event_type <> OLD.event_type THEN
        RAISE EXCEPTION 'outbox.event_type is immutable after insert';
    END IF;
    IF NEW.payload <> OLD.payload THEN
        RAISE EXCEPTION 'outbox.payload is immutable after insert';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS protect_outbox_immutable ON events.outbox;

CREATE TRIGGER protect_outbox_immutable
BEFORE UPDATE ON events.outbox FOR EACH ROW EXECUTE FUNCTION events.protect_outbox_immutable_fields();

-- Once stamped, processed_at cannot be cleared; prevents a buggy drain
-- from resurrecting already-delivered events.
CREATE OR REPLACE FUNCTION events.prevent_outbox_processed_at_clear() RETURNS TRIGGER AS $$
BEGIN
    IF OLD.processed_at IS NOT NULL AND NEW.processed_at IS NULL THEN
        RAISE EXCEPTION 'outbox.processed_at cannot be cleared once set (id=%)', OLD.id;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS prevent_outbox_processed_at_clear ON events.outbox;

CREATE TRIGGER prevent_outbox_processed_at_clear
BEFORE UPDATE ON events.outbox FOR EACH ROW EXECUTE FUNCTION events.prevent_outbox_processed_at_clear();

COMMENT ON TABLE events.outbox IS
'Transactional outbox for async event delivery. Written before each Asynq '
'enqueue and marked processed on success. Unprocessed rows are retried up to 10 times.';

COMMENT ON COLUMN events.outbox.event_type IS
'Fully-qualified event type (e.g. user.registered). Immutable after insert.';

COMMENT ON COLUMN events.outbox.payload IS
'JSON-encoded event struct matching the registered type. Immutable after insert.';

COMMENT ON COLUMN events.outbox.dedupe_key IS
'Optional caller-supplied deduplication key. When non-NULL, the '
'partial unique index ux_outbox_pending_dedupe blocks a second '
'pending row from being inserted with the same (event_type, '
'dedupe_key) tuple. NULL opts out of dedup.';

COMMENT ON COLUMN events.outbox.created_at IS 'Row creation timestamp. Immutable after insert.';

COMMENT ON COLUMN events.outbox.processed_at IS
'Stamped when all async subscriptions were successfully enqueued. '
'NULL means pending or all retry attempts exhausted.';

COMMENT ON COLUMN events.outbox.retry_count IS
'Number of failed drain attempts. Rows with retry_count >= 10 are abandoned.';

COMMENT ON COLUMN events.outbox.last_error IS
'Error message from the most recent failed drain attempt.';

COMMENT ON COLUMN events.outbox.last_attempted_at IS
'Timestamp of the most recent drain attempt. Combined with retry_count it '
'gates the next attempt via exponential backoff. NULL means the row has '
'never been attempted.';

COMMENT ON FUNCTION events.protect_outbox_immutable_fields() IS 'BEFORE UPDATE trigger on events.outbox. Raises an exception if any immutable field (event_type, payload, created_at) is modified after insert. The outbox is append-only except for the drain fields.';

COMMENT ON FUNCTION events.prevent_outbox_processed_at_clear() IS 'BEFORE UPDATE trigger on events.outbox that raises an exception if processed_at is set back to NULL after being stamped. A processed outbox row must remain immutable to prevent double-delivery.';

-- Row-level security. The outbox is infrastructure with split
-- privileges:
--   - app_api may INSERT only. Domain services call
--     OutboxRepository.Publish inside a service-level WithTx so
--     the outbox row commits atomically with the domain mutation.
--     SELECT/UPDATE/DELETE are deliberately denied to keep the
--     queue tamper-resistant from the API binary.
--   - app_worker (event-dispatcher worker) has full access: it
--     SELECTs pending rows with FOR UPDATE SKIP LOCKED, marks them
--     processed/failed, and moves exhausted rows to the dead-letter.
--   - app_readonly reads for Grafana / outbox-health dashboards.
ALTER TABLE events.outbox ENABLE ROW LEVEL SECURITY;
ALTER TABLE events.outbox FORCE ROW LEVEL SECURITY;

CREATE POLICY outbox_app_insert ON events.outbox
FOR INSERT TO app_api WITH CHECK (TRUE);

CREATE POLICY outbox_admin_all ON events.outbox
FOR ALL TO app_worker USING (TRUE) WITH CHECK (TRUE);

CREATE POLICY outbox_ro_select ON events.outbox
FOR SELECT TO app_readonly USING (TRUE);

COMMENT ON POLICY outbox_app_insert ON events.outbox IS
'app_api may INSERT outbox rows only (called inside service-level WithTx by OutboxRepository.Publish). SELECT / UPDATE / DELETE are intentionally denied so the API binary cannot tamper with the queue once committed.';

COMMENT ON POLICY outbox_admin_all ON events.outbox IS
'Only the event-dispatcher worker (app_worker) reads, marks processed/failed, or dead-letters rows.';
