CREATE TABLE settings (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
) STRICT;

CREATE TABLE repos (
    id              TEXT PRIMARY KEY,
    name_with_owner TEXT NOT NULL,
    first_seen_at   INTEGER NOT NULL,
    default_method  TEXT NOT NULL CHECK (default_method IN ('squash', 'merge', 'rebase')),
    is_new          INTEGER NOT NULL DEFAULT 1 CHECK (is_new IN (0, 1))
) STRICT;

CREATE TABLE repo_branch_rules (
    repo_id  TEXT NOT NULL REFERENCES repos (id) ON DELETE CASCADE,
    position INTEGER NOT NULL,
    pattern  TEXT NOT NULL,
    method   TEXT NOT NULL CHECK (method IN ('squash', 'merge', 'rebase')),
    PRIMARY KEY (repo_id, position)
) STRICT;

CREATE TABLE prs (
    id           TEXT PRIMARY KEY,
    repo_id      TEXT NOT NULL REFERENCES repos (id),
    number       INTEGER NOT NULL,
    title        TEXT NOT NULL,
    head_branch  TEXT NOT NULL,
    base_branch  TEXT NOT NULL,
    head_sha     TEXT NOT NULL,
    state        TEXT NOT NULL CHECK (state IN ('open', 'merged', 'closed')),
    merged_at    INTEGER,
    last_seen_at INTEGER NOT NULL
) STRICT;

CREATE INDEX prs_state_repo ON prs (state, repo_id);

CREATE TABLE ticket_links (
    pr_id      TEXT NOT NULL REFERENCES prs (id) ON DELETE CASCADE,
    ticket_key TEXT NOT NULL,
    source     TEXT NOT NULL CHECK (source IN ('branch', 'title', 'manual')),
    removed    INTEGER NOT NULL DEFAULT 0 CHECK (removed IN (0, 1)),
    position   INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (pr_id, ticket_key)
) STRICT;

CREATE INDEX ticket_links_ticket ON ticket_links (ticket_key);

CREATE TABLE sync_rules (
    id            TEXT PRIMARY KEY,
    position      INTEGER NOT NULL,
    name          TEXT NOT NULL,
    enabled       INTEGER NOT NULL DEFAULT 1 CHECK (enabled IN (0, 1)),
    trigger       TEXT NOT NULL,
    filters       TEXT NOT NULL DEFAULT '{}',
    target_status TEXT NOT NULL,
    mode          TEXT NOT NULL DEFAULT 'all' CHECK (mode IN ('all', 'any'))
) STRICT;

CREATE TABLE rule_firings (
    rule_id     TEXT NOT NULL REFERENCES sync_rules (id) ON DELETE CASCADE,
    ticket_key  TEXT NOT NULL,
    fingerprint TEXT NOT NULL,
    fired_at    INTEGER NOT NULL,
    PRIMARY KEY (rule_id, ticket_key)
) STRICT;

CREATE TABLE activity (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    created_at   INTEGER NOT NULL,
    kind         TEXT NOT NULL,
    ticket_key   TEXT,
    pr_id        TEXT,
    triggered_by TEXT NOT NULL,
    result       TEXT NOT NULL,
    error        TEXT
) STRICT;

CREATE INDEX activity_created ON activity (created_at DESC);

CREATE TABLE pending_retries (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    ticket_key    TEXT NOT NULL,
    transition_id TEXT NOT NULL,
    fields        TEXT NOT NULL DEFAULT '{}',
    last_error    TEXT NOT NULL,
    created_at    INTEGER NOT NULL
) STRICT;

CREATE TABLE snapshot (
    id         INTEGER PRIMARY KEY CHECK (id = 1),
    version    INTEGER NOT NULL,
    data       BLOB NOT NULL,
    updated_at INTEGER NOT NULL
) STRICT;

CREATE TABLE sessions (
    id_hash        BLOB PRIMARY KEY,
    created_at     INTEGER NOT NULL,
    last_issued_at INTEGER NOT NULL,
    expires_at     INTEGER NOT NULL
) STRICT;

CREATE INDEX sessions_expires ON sessions (expires_at);

CREATE TABLE oauth_states (
    state_hash  BLOB PRIMARY KEY,
    provider    TEXT NOT NULL CHECK (provider IN ('github', 'atlassian')),
    session_key TEXT NOT NULL,
    verifier    TEXT NOT NULL,
    expires_at  INTEGER NOT NULL
) STRICT;

CREATE INDEX oauth_states_expires ON oauth_states (expires_at);
