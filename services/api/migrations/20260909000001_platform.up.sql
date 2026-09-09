-- Platform foundation: transactional outbox for domain events (ADR-003).
-- Idempotency key registry for external operations (webhooks, imports).

CREATE TABLE IF NOT EXISTS event_outbox (
    event_id        UUID PRIMARY KEY,
    event_type      TEXT NOT NULL,
    schema_version  INT  NOT NULL CHECK (schema_version >= 1),
    school_id       UUID,                    -- NULL for platform-level events
    aggregate_id    TEXT NOT NULL,
    occurred_at     TIMESTAMPTZ NOT NULL,
    actor_id        TEXT NOT NULL DEFAULT '',
    correlation_id  TEXT NOT NULL DEFAULT '',
    payload         JSONB NOT NULL,
    published_at    TIMESTAMPTZ,
    attempts        INT NOT NULL DEFAULT 0,
    last_error      TEXT
);

CREATE INDEX IF NOT EXISTS idx_event_outbox_unpublished
    ON event_outbox (occurred_at)
    WHERE published_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_event_outbox_type_time
    ON event_outbox (event_type, occurred_at DESC);

CREATE TABLE IF NOT EXISTS idempotency_keys (
    key          TEXT NOT NULL,
    scope        TEXT NOT NULL,              -- e.g. 'payments.webhook', 'api.request'
    school_id    UUID,
    response     JSONB,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (scope, key)
);

CREATE INDEX IF NOT EXISTS idx_idempotency_keys_created
    ON idempotency_keys (created_at);
