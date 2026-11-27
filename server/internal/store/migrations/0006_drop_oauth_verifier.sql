-- The PKCE verifier of a pending authorization is kept in memory only, so the column is gone.
-- A row of an authorization started before the upgrade loses its verifier and cannot complete.
ALTER TABLE oauth_states DROP COLUMN verifier;
