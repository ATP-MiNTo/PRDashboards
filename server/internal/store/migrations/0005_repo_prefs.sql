-- Per-repository choices of the settings dialog: whether the repo follows the global default merge
-- method, and its own "Delete branch after merge" choice (default, yes or no). Repos stored before
-- this keep the method they have, so they start with method_inherit = 0.
ALTER TABLE repos ADD COLUMN method_inherit INTEGER NOT NULL DEFAULT 0 CHECK (method_inherit IN (0, 1));
ALTER TABLE repos ADD COLUMN delete_branch TEXT NOT NULL DEFAULT 'default' CHECK (delete_branch IN ('default', 'yes', 'no'));
