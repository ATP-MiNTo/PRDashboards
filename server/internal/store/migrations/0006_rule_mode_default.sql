-- A sync rule may follow the global "Multi-PR tickets move when" setting: its mode can now be
-- 'default' besides 'all' and 'any'. SQLite cannot change a CHECK constraint, so sync_rules is rebuilt.
-- rule_firings references it with ON DELETE CASCADE, so it is set aside and rebuilt too, instead of
-- being emptied when the old table is dropped. Stored rules keep their explicit mode.
ALTER TABLE rule_firings RENAME TO rule_firings_old;
ALTER TABLE sync_rules RENAME TO sync_rules_old;

CREATE TABLE sync_rules (
    id            TEXT PRIMARY KEY,
    position      INTEGER NOT NULL,
    name          TEXT NOT NULL,
    enabled       INTEGER NOT NULL DEFAULT 1 CHECK (enabled IN (0, 1)),
    trigger       TEXT NOT NULL,
    filters       TEXT NOT NULL DEFAULT '{}',
    target_status TEXT NOT NULL,
    mode          TEXT NOT NULL DEFAULT 'all' CHECK (mode IN ('all', 'any', 'default'))
) STRICT;

INSERT INTO sync_rules (id, position, name, enabled, trigger, filters, target_status, mode)
    SELECT id, position, name, enabled, trigger, filters, target_status, mode FROM sync_rules_old;

CREATE TABLE rule_firings (
    rule_id     TEXT NOT NULL REFERENCES sync_rules (id) ON DELETE CASCADE,
    ticket_key  TEXT NOT NULL,
    fingerprint TEXT NOT NULL,
    fired_at    INTEGER NOT NULL,
    PRIMARY KEY (rule_id, ticket_key)
) STRICT;

INSERT INTO rule_firings (rule_id, ticket_key, fingerprint, fired_at)
    SELECT rule_id, ticket_key, fingerprint, fired_at FROM rule_firings_old;

DROP TABLE rule_firings_old;
DROP TABLE sync_rules_old;
