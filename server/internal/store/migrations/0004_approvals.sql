ALTER TABLE repos ADD COLUMN approvals_required INTEGER NOT NULL DEFAULT 1 CHECK (approvals_required BETWEEN 1 AND 10);
