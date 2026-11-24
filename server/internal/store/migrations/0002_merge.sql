CREATE TABLE idempotency (
    key         TEXT PRIMARY KEY,
    pr_id       TEXT NOT NULL,
    result_json TEXT NOT NULL,
    created_at  INTEGER NOT NULL
) STRICT;

CREATE INDEX idempotency_created ON idempotency (created_at);

ALTER TABLE pending_retries ADD COLUMN pr_id TEXT;
ALTER TABLE pending_retries ADD COLUMN to_status TEXT NOT NULL DEFAULT '';
ALTER TABLE activity ADD COLUMN summary TEXT NOT NULL DEFAULT '';
