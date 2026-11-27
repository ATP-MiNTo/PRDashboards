-- Approvals required may be 0 (nothing required), and 0 is the default. SQLite cannot change a CHECK
-- or a DEFAULT in place, so the repos table is rebuilt. Foreign keys stay on inside the migration
-- transaction, so the references from prs (restrict) are checked at commit instead of at the DROP,
-- and the branch rules, which the DROP would delete through ON DELETE CASCADE, are copied aside and
-- put back. 1 was only ever the old default, so 1 becomes 0; 2 to 10 are choices and stay.
PRAGMA defer_foreign_keys = ON;

CREATE TEMP TABLE repos_keep AS
SELECT id, name_with_owner, first_seen_at, default_method, is_new,
       CASE WHEN approvals_required = 1 THEN 0 ELSE approvals_required END AS approvals_required,
       method_inherit, delete_branch
FROM repos;
CREATE TEMP TABLE repo_branch_rules_keep AS SELECT repo_id, position, pattern, method FROM repo_branch_rules;

DROP TABLE repos;

CREATE TABLE repos (
    id                 TEXT PRIMARY KEY,
    name_with_owner    TEXT NOT NULL,
    first_seen_at      INTEGER NOT NULL,
    default_method     TEXT NOT NULL CHECK (default_method IN ('squash', 'merge', 'rebase')),
    is_new             INTEGER NOT NULL DEFAULT 1 CHECK (is_new IN (0, 1)),
    approvals_required INTEGER NOT NULL DEFAULT 0 CHECK (approvals_required BETWEEN 0 AND 10),
    method_inherit     INTEGER NOT NULL DEFAULT 0 CHECK (method_inherit IN (0, 1)),
    delete_branch      TEXT NOT NULL DEFAULT 'default' CHECK (delete_branch IN ('default', 'yes', 'no'))
) STRICT;

INSERT INTO repos (id, name_with_owner, first_seen_at, default_method, is_new, approvals_required, method_inherit, delete_branch)
SELECT id, name_with_owner, first_seen_at, default_method, is_new, approvals_required, method_inherit, delete_branch
FROM repos_keep;
INSERT INTO repo_branch_rules (repo_id, position, pattern, method)
SELECT repo_id, position, pattern, method FROM repo_branch_rules_keep;

DROP TABLE repos_keep;
DROP TABLE repo_branch_rules_keep;
