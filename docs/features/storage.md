# Storage and secrets

Status: Built. PRD: [PRD.md](../PRD.md). Spec: [TECH_SPEC.md](../TECH_SPEC.md) sections 6.1 and 6.2. Related: [auth.md](auth.md), [security.md](security.md), [app-shell.md](app-shell.md).

## Purpose

Keeps everything the app remembers: secrets (tokens, OAuth client credentials, the cookie signing
key) in an encrypted vault file whose key is in the OS keychain (or a passphrase-protected file), and everything else in one SQLite file. Milestone 2
adds the secrets store, the database with all tables of TECH_SPEC 6.2, persisted sessions, global
settings and "sign out all browsers".

Differences from the spec:

- No writer goroutine. The single writer is a connection pool limited to one connection (see
  Rules). The effect is the same: one write at a time.
- Without a keychain and without a terminal the binary exits with a message instead of "starting
  locked", because a locked app can do nothing useful.
- `settings`, `sessions`, `oauth_states`, `schema_migrations` and, since the poller, `repos`,
  `repo_branch_rules`, `prs`, `ticket_links` and `snapshot` have code on them
  ([poller.md](poller.md), [ticket-linking.md](ticket-linking.md)). The other tables are created
  now and used by later milestones.

## Data model

Secrets, kept in `vault.enc` (or the same names in `secrets.enc`):

| Item | Content |
| --- | --- |
| `session.key` | 32 random bytes, HMAC key of the session cookie and CSRF token |
| `github.oauth.client`, `atlassian.oauth.client` | JSON `{clientId, clientSecret}` |
| `github.token` | JSON `{token, method (oauth or pat), login, warning}` |
| `atlassian.token` | JSON `{accessToken, refreshToken, expiresAt, sites, needsReconnect}` |

Keychain, service `prdashboards`: one item, `vault.key`, 32 random bytes (base64 as every keychain
value). Nothing else is stored there. The reason is a size limit: go-keyring on macOS fails with
`data passed to Set was too big` when its `security -i` command exceeds 4096 bytes, which is
about 3000 characters of value, and the Atlassian token (access JWT, refresh token, sites) is
larger. `Keychain.Set` itself refuses values over 2048 base64 characters with `ErrTooLarge`.

`vault.enc`: header `PRDV`, version, 12-byte nonce, then the AES-256-GCM ciphertext of the same
JSON map, sealed with the raw `vault.key`, no KDF. The header is the additional data. A fresh
nonce on every write. The magic differs from `secrets.enc`, so neither file opens as the other.

`secrets.enc`: header `PRDS`, version, KDF time, memory (KiB) and threads, 16-byte salt, 12-byte
nonce, then the AES-256-GCM ciphertext of a JSON map. The whole header is the additional data.
Argon2id with time 3, 64 MiB, 4 threads, 32-byte key. A fresh salt and nonce on every write.

SQLite (`<data-dir>/data.db`, [migration 0001](../../server/internal/store/migrations/0001_init.sql); [migration 0002](../../server/internal/store/migrations/0002_merge.sql) adds the `idempotency` table, `pending_retries.pr_id` and `to_status`, and `activity.summary`, see [merge-and-transition.md](merge-and-transition.md)):
`schema_migrations`, `settings`, `repos`, `repo_branch_rules`, `prs`, `ticket_links`, `sync_rules`,
`rule_firings`, `activity`, `pending_retries`, `snapshot`, `sessions` (hashed id, created, last
issued, expires) and `oauth_states` (hashed state, provider, session key, PKCE verifier, expiry).
All tables are `STRICT`. Times are Unix seconds.

Settings (`GET/PUT /api/settings`, schema `Settings` in [api/openapi.yaml](../../api/openapi.yaml)):
`polling {githubSec, jiraSec}` (each one of 60, 120, 300, 900, 3600), `theme`,
`defaultMethodForNewRepos`, `deleteBranchAfterMerge`, `jiraSiteId` (nullable). Stored as typed JSON
values per key in one transaction.

## Code map

Server:

- [server/internal/secrets/secrets.go](../../server/internal/secrets/secrets.go): `Store`, `ErrNotFound`, item name constants, `Memory`.
- [server/internal/secrets/keychain.go](../../server/internal/secrets/keychain.go): `Keychain` over go-keyring, `Probe`, `ErrTooLarge`. Values are base64 so binary keys survive every backend. `kr` is the seam tests replace to model the macOS size limit.
- [server/internal/secrets/file.go](../../server/internal/secrets/file.go): `File` with a codec (passphrase plus Argon2id, or raw key), `OpenFile`, `OpenVault`, the batch `setMany`, `writeAtomic`, and the errors `ErrWrongPassphrase`, `ErrNotSecretsFile`, `ErrWrongVaultKey`, `ErrNotVaultFile`.
- [server/internal/secrets/vault.go](../../server/internal/secrets/vault.go): `openKeychainVault` (key from the keychain, create or open `vault.enc`) and `migrateLegacy`.
- [server/internal/secrets/open.go](../../server/internal/secrets/open.go): `Open` (choose the backend), `VaultFileName`, `VaultKeyName`, `Prompter`, `TerminalPrompter`, `ErrNoTerminal`.
- [server/internal/store/store.go](../../server/internal/store/store.go): `Open`, `Write` (the one write transaction), `Close`.
- [server/internal/store/migrate.go](../../server/internal/store/migrate.go): embedded numbered migrations, applied in order, each in a transaction.
- [server/internal/store/repos.go](../../server/internal/store/repos.go) and [merges.go](../../server/internal/store/merges.go): repo settings, `RecordMerge` and the retry, activity and idempotency queries.
- [server/internal/store/refresh.go](../../server/internal/store/refresh.go): `SaveRefresh` (one transaction for repos, PRs, links, resolutions and the snapshot), snapshot load and save, user links.
- [server/internal/store/settings.go](../../server/internal/store/settings.go): `Settings`, defaults, `GetSettings`, `PutSettings`.
- [server/internal/store/sessions.go](../../server/internal/store/sessions.go): session rows and `oauth_states` (`InsertOAuthState`, `ConsumeOAuthState`).
- [server/internal/auth/session.go](../../server/internal/auth/session.go): `Sessions` (write-through cache), `Load`, `RevokeAll`, `LoadOrCreateKey`.
- [server/internal/api/handlers_settings.go](../../server/internal/api/handlers_settings.go): `GetSettings`, `PutSettings`, `RevokeAllSessions`.
- [server/cmd/prdash/run.go](../../server/cmd/prdash/run.go) and [datadir.go](../../server/cmd/prdash/datadir.go): wiring and `checkPrivateFiles`.

## Key flows

1. Start: `prepareDataDir`, lock, `checkPrivateFiles` (refuses a `session.key`, `secrets.enc`,
   `vault.enc` or `data.db` that group or others can access, with the `chmod 600 <path>` to run), then
   `secrets.Open`.
2. `secrets.Open`: probe the keychain (set, get and delete a probe item). On success read
   `vault.key` (one Get). Missing and no `vault.enc`: create 32 random bytes and store them. Missing
   but `vault.enc` exists: fail with `ErrVaultKeyMissing`, the file is left alone. A key that is not
   32 bytes, a wrong key or a damaged vault fail too, and the vault is never recreated over an
   unreadable file. Then open or create `vault.enc` with the key and migrate (below); the backend
   is `keychain`. Else, with `secrets.enc` present ask for the passphrase (3 attempts), without it ask twice and create
   it (minimum 8 characters). No terminal: `ErrNoTerminal`, the binary prints how to run it from a
   terminal and exits 1.
   Migration, once: for each of the five item names, one Get from the keychain. Items the vault
   lacks are copied into it with one vault write. Only after that write succeeds are the keychain
   copies deleted (an item already in the vault just has its keychain copy deleted). A failed
   delete is logged as a warning and does not stop the start. The log names the items, never
   values.
3. `store.Open` creates `data.db` with mode 0600, opens the writer and the reader pool and runs
   pending migrations.
4. `LoadOrCreateKey`: use the `session.key` item. When a legacy `<data-dir>/session.key` file
   exists and the item does not, import it, then delete the file. When both exist the stored key
   wins and the file is deleted.
5. `Sessions.Load` reads unexpired sessions into the cache. `Create`, the sliding re-issue and
   `Delete` write through to SQLite. A failed write is logged and the cache stays authoritative
   for the running process.
6. `POST /api/sessions/revoke-all`: clear the cache, `DELETE FROM sessions`, answer `204` with an
   expired `prd_session` cookie. The next request needs a new launch URL.
7. `PUT /api/settings`: validate against the schema, then write all keys in one transaction.

## Outside calls

None. The keychain is a local call to the OS.

## Rules and invariants

- One writer. `Store.Write` uses a pool with `MaxOpenConns(1)` and `_txlock=immediate`, so a write
  transaction never overlaps another and never upgrades from a read lock. Reads use a second pool
  opened with `query_only(1)`. Multi-row changes happen inside one `Write` call.
- Pragmas on every connection: `journal_mode(WAL)`, `foreign_keys(1)`, `busy_timeout(5000)`.
- Secrets never go to SQLite, logs or API responses. The only secret-like value in SQLite is the
  short-lived PKCE verifier of a pending flow, deleted when the state is consumed or expires.
- Session ids and OAuth states are stored as SHA-256 hashes. `ConsumeOAuthState` is one
  `DELETE ... RETURNING`, so a state works once, and only for its session and provider.
- `vault.enc` and `secrets.enc` are written to a temporary file in the same directory, synced
  (`fsync`) and renamed over the old one, mode 0600. One write per `Set`, `Delete` or batch, rolled
  back in memory when it fails. A wrong passphrase gives `ErrWrongPassphrase`, a wrong key
  `ErrWrongVaultKey`, and no partial state. Tampering
  with the header fails the GCM check.
- The passphrase is read from the terminal without echo, never from a flag or the environment, and
  never logged.
- Keep migrations append-only. A new migration is a new numbered file.
- `go.mod` pins `go-keyring`, `x/crypto`, `x/term` and `modernc.org/sqlite` to exact versions.

## Tests

- [server/internal/secrets/vault_test.go](../../server/internal/secrets/vault_test.go): a keychain mock with the macOS size limit, a 22 KB token round trip with only `vault.key` in the keychain, migration (and a second start), missing key, wrong key, tampering, foreign files, passphrase and key files not interchangeable, mode, fresh nonce, batch rollback.
- [server/internal/secrets/secrets_test.go](../../server/internal/secrets/secrets_test.go): memory and file stores, round trip, wrong passphrase, tampered header and ciphertext, fresh nonce and salt per write, file mode, atomic rewrite, backend choice, passphrase confirmation, no terminal.
- [server/internal/store/store_test.go](../../server/internal/store/store_test.go): migrations from empty and again, every table, STRICT and constraints, concurrent writers, reader pool is read-only, settings round trip, sessions, `oauth_states` single use and expiry.
- [server/internal/auth/persist_test.go](../../server/internal/auth/persist_test.go) and [session_test.go](../../server/internal/auth/session_test.go): persisted sessions across a restart, revoke all, legacy key import.
- [server/internal/api/auth_api_test.go](../../server/internal/api/auth_api_test.go): settings and revoke-all over HTTP.
- [server/cmd/prdash/main_test.go](../../server/cmd/prdash/main_test.go): file mode refusal (including `vault.enc`), legacy key import into the vault, sessions across a restart, file backend with a passphrase, no-terminal exit.
