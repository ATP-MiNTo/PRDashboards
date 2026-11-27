-- The merged pull requests of the last window (SyncState) and the pruning of old merged and closed
-- ones (SaveRefresh) look pull requests up by state and merge time.
CREATE INDEX prs_state_merged ON prs (state, merged_at);
