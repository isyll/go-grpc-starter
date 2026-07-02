-- Permanent failure record for events.outbox rows that exhausted
-- retries, carry an unknown event_type, or whose payload could not
-- be unmarshalled. Admin-only: app_api has no policy at all and
-- cannot read or write this table.
CREATE TABLE IF NOT EXISTS events.outbox_dead_letter (
  id BIGSERIAL PRIMARY KEY,
  source_id BIGINT NOT NULL,
  event_type TEXT NOT NULL,
  payload JSONB NOT NULL,
  failure_reason TEXT NOT NULL CHECK (failure_reason IN (
    'unknown_event_type',
    'retry_exhausted',
    'unmarshal_failed'
  )),
  last_error TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  failed_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

COMMENT ON TABLE events.outbox_dead_letter IS
'Permanently failed outbox rows. Rows land here when the event type is '
'unknown, payload cannot be unmarshalled, or retry_count exceeded the '
'maximum. Source row is kept in events.outbox with processed_at set.';

COMMENT ON COLUMN events.outbox_dead_letter.source_id IS
'ID of the originating events.outbox row.';
COMMENT ON COLUMN events.outbox_dead_letter.event_type IS
'Fully-qualified event type of the failed row (copied from events.outbox).';
COMMENT ON COLUMN events.outbox_dead_letter.payload IS
'JSON payload of the failed row (copied from events.outbox).';
COMMENT ON COLUMN events.outbox_dead_letter.failure_reason IS
'Reason the row could not be processed: unknown_event_type, '
'retry_exhausted, or unmarshal_failed.';
COMMENT ON COLUMN events.outbox_dead_letter.last_error IS
'Error message from the last processing attempt.';
COMMENT ON COLUMN events.outbox_dead_letter.failed_at IS
'Timestamp the row was dead-lettered. Powers alerting dashboards.';

-- Alerting hot path: dashboards group by failure_reason and order
-- by failed_at DESC.
CREATE INDEX IF NOT EXISTS idx_outbox_dead_letter_reason_failed
ON events.outbox_dead_letter (failure_reason, failed_at DESC);

-- Row-level security. Dead-letter is admin-only: the
-- event-dispatcher worker writes here when a row exhausts
-- retries or its event type is unknown. app_api has no
-- policy and cannot touch this table. app_readonly reads for
-- alerting dashboards.
ALTER TABLE events.outbox_dead_letter ENABLE ROW LEVEL SECURITY;
ALTER TABLE events.outbox_dead_letter FORCE ROW LEVEL SECURITY;

CREATE POLICY outbox_dl_admin_all ON events.outbox_dead_letter
FOR ALL TO app_worker USING (TRUE) WITH CHECK (TRUE);

CREATE POLICY outbox_dl_ro_select ON events.outbox_dead_letter
FOR SELECT TO app_readonly USING (TRUE);

COMMENT ON POLICY outbox_dl_admin_all ON events.outbox_dead_letter IS
'Only the event-dispatcher worker (app_worker) writes / inspects dead-letter rows. The API binary (app_api) has no policy and cannot read or write.';

COMMENT ON POLICY outbox_dl_ro_select ON events.outbox_dead_letter IS
'app_readonly reads dead-letter rows for the outbox-health Grafana dashboard and on-call monitoring.';
