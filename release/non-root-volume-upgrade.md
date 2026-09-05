# Non-root persistent-volume upgrade

Status: `OPERATOR_RUNBOOK — CUTOVER NOT PERFORMED`

This runbook covers the one-time transition from a historical root-running New
API container to the managed image that runs as numeric UID/GID `65532:65532`.
It does not authorize a deployment, deletion, retained-volume mutation, or
database migration by itself.

## Why a separate migration is required

`COPY --chown=65532:65532` in the image initializes only the image layer. A
bind mount or named volume replaces that layer at container start, retaining
the ownership of its existing inodes. The historical Compose layout mounts
`./data` at `/data` and `./logs` at `/app/logs`; root-owned SQLite, WAL and log
files therefore remain unwritable to UID 65532.

The managed image deliberately has no root entrypoint that recursively runs
`chown` on every start. Such an entrypoint would require permanent root
privilege, mutate retained state during ordinary restarts, make startup time
scale with volume size, and hide rollback provenance.

The supported transition is an offline, recoverable copy:

```text
quiesced old volume mounted/read as source only
    ├── byte-preserving root-owned backup
    └── fresh target volume → ownership changed only on target → UID 65532 checks
```

The old volume and backup remain available until the new runtime has passed its
acceptance window.

## Preconditions and stop conditions

Record the following before changing anything:

- exact old image digest and exact new image digest;
- deployment/Compose project and service name;
- whether `/data` and `/app/logs` are bind mounts or named volumes;
- canonical source paths or exact volume names;
- database mode: SQLite, MySQL or PostgreSQL;
- a fresh destination and a fresh backup location;
- an approved maintenance window and rollback owner.

Stop if any source or destination is unresolved, relative, or a symlink
(including an intermediate path component). Sources must be the exact
operator-approved old mount paths; backup and target paths must remain beneath
the approved migration root. Stop if a source contains anything other than
regular files and directories, or if a target/backup is not empty. Do not use
wildcard volume names, `/var/lib/docker` as a recursive target, `chmod 777`, or
a path derived from an unset variable.

For SQLite, stop every writer first and verify that no old container or process
can still access the database. Copy `one-api.db`, `one-api.db-wal` and
`one-api.db-shm` as one quiesced tree. Never copy a live SQLite database merely
because the main `.db` file appears stable.

For MySQL/PostgreSQL, this runbook does not copy the external database; it still
applies to `/data` files and `/app/logs` when those mounts are retained.

## One-time bind-mount procedure

The following names are examples. Replace them with operator-reviewed canonical
absolute paths, then read them back before use:

```bash
set -Eeuo pipefail
umask 077

OLD_DATA=/srv/new-api/data
OLD_LOGS=/srv/new-api/logs
CHANGE_ID=reviewed-change-id
MIGRATION_ROOT=/srv/new-api-volume-upgrade-${CHANGE_ID}
BACKUP_DATA=${MIGRATION_ROOT}/backup-data
BACKUP_LOGS=${MIGRATION_ROOT}/backup-logs
NEW_DATA=${MIGRATION_ROOT}/new-data
NEW_LOGS=${MIGRATION_ROOT}/new-logs
```

1. Stop the old service. Confirm its container is stopped and no process has an
   open SQLite database, WAL, or log file under the old mounts.
2. Require each old path to equal `realpath -e` byte-for-byte. This rejects
   relative paths, `..`, final symlinks and symlinked intermediate components.
3. Create `MIGRATION_ROOT`, backup directories and target directories as new,
   empty, root-owned directories with mode `0700`.
4. Reject source symlinks, devices, sockets, FIFOs and any other special file:

   ```bash
   find -P "${OLD_DATA}" "${OLD_LOGS}" -mindepth 1 \
     ! -type d ! -type f -print
   ```

   Any output is a hard stop.
5. Generate a sorted SHA-256 file manifest from each old tree. Run manifest
   generation from inside the tree so entries use the same relative names.
6. Copy each old tree once to its backup and once to its fresh target. Use an
   offline root-owned utility with the old mounts read-only. A GNU tar copy that
   preserves ordinary metadata is:

   ```bash
   (cd "${OLD_DATA}" && tar --acls --xattrs --numeric-owner -cpf - .) |
     (cd "${BACKUP_DATA}" && tar --acls --xattrs --numeric-owner -xpf -)
   (cd "${OLD_DATA}" && tar --acls --xattrs --numeric-owner -cpf - .) |
     (cd "${NEW_DATA}" && tar --acls --xattrs --numeric-owner -xpf -)
   ```

   Repeat for logs. Do not use a copy command that follows symlinks.
7. Regenerate manifests for backup and target. They must equal the source
   manifests before any target write test.
8. Keep the backup root-owned and read-only to ordinary runtime users. Change
   ownership only on the new targets:

   ```bash
   chown -R 65532:65532 "${NEW_DATA}" "${NEW_LOGS}"
   ```

   Preserve restrictive source modes; owner read/write/execute permission is
   sufficient after ownership changes. Do not broaden group or world access.
9. Keep `MIGRATION_ROOT` mode `0700` and root-owned. Do **not** make this parent
   traversable merely so host `setpriv` can reach the children; it may also
   contain the root-owned backup or migration evidence. Instead, use a
   network-disabled, digest-pinned non-root utility container and bind the two
   target directories directly at their runtime paths:

   ```bash
   docker run --rm --network none --read-only \
     --user 65532:65532 \
     --mount type=bind,src="${NEW_DATA}",dst=/data \
     --mount type=bind,src="${NEW_LOGS}",dst=/app/logs \
     --tmpfs /tmp:rw,nosuid,nodev,noexec,size=16m \
     <approved-verifier-image>@sha256:<reviewed-digest> \
     <reviewed-read-write-verification-command>
   ```

   Docker resolves the host paths before installing them as container mount
   roots, so UID 65532 does not need execute permission on
   `MIGRATION_ROOT`. The verifier image and command must be fixed in the
   deployment card before execution; do not use a moving tag or add a shell to
   the production scratch image.

   Inside those direct mounts, verify as numeric UID/GID 65532:

   - every target path is traversable and every required file is readable;
   - SQLite `PRAGMA integrity_check` returns `ok`;
   - `BEGIN IMMEDIATE; ROLLBACK` succeeds on the copied database;
   - a disposable log probe can be created, appended, fsynced and removed;
   - no target entry has another UID/GID.
10. Recompute the old-tree content and metadata fingerprints. They must remain
    identical to the pre-copy values.

The executable rehearsal in
[`scripts/test-support/non-root-volume-upgrade.spec.sh`](../scripts/test-support/non-root-volume-upgrade.spec.sh)
implements these checks against disposable root-owned SQLite/WAL/log fixtures.

## Named-volume variant

Use the same state transition with three distinct sets of exact volume names:
old, backup and new. Create the backup and new volumes before the migration job.
Mount old volumes `:ro`; mount backup and new volumes writable into a one-shot,
network-disabled, digest-pinned utility container running as root. Reject
non-empty destinations and unsafe source entries before copying. Apply
`chown -R 65532:65532` only to the new volumes.

Do not mount the old volume writable merely to simplify `chown`. Do not reuse a
failed partial destination; preserve it for diagnosis and repeat into another
fresh target.

## Cutover verification

Update the managed deployment to mount only the new target paths/volumes, then
start the exact new image digest as `65532:65532`. Verify before reopening
traffic:

- process UID/GID is exactly `65532:65532`;
- SQLite opens without creating a second empty database;
- expected row counts or an application-level invariant match the pre-cutover
  inventory;
- WAL recovery and a bounded application write succeed;
- `/app/logs` receives a new log entry;
- health/readiness and settlement-readback capability checks pass;
- old source and backup manifests remain unchanged;
- deployment evidence records old/new image digests and old/backup/new volume
  identities without recording database contents.

If any check fails, keep traffic closed. Do not repair the old volume in place.

## Rollback

1. Stop the new container before changing mounts.
2. Preferred immediate rollback: remount the untouched old volume and restart
   the saved exact N-1 root-running image digest.
3. If policy requires the new non-root image, restore the root-owned backup into
   another fresh volume, verify its manifest, change ownership on that fresh
   restore only, and repeat the UID 65532 checks.
4. Verify the original row/log inventory and record which exact volume is now
   active.

Never merge target writes back into the old or backup volume during rollback.
Retain the stopped failed target, untouched old volume and backup until a
separate cleanup decision names exact deletions. No deletion is part of this
runbook.

## Reproducible disposable rehearsal

Run as root on a disposable Ubuntu host with `python3`, `setpriv` and GNU tar:

```bash
scripts/test-support/non-root-volume-upgrade.spec.sh
```

The test creates an actual root-owned SQLite database with an uncheckpointed WAL
plus nested log files and first proves the original regression: UID 65532 cannot
open that SQLite database for write or append its log. It then copies the tree
to independent backup and target directories, proves UID 65532 can pass SQLite
integrity/write checks and write logs, proves the old tree and backup did not
change, and restores the backup into another fresh tree. It also rejects
source/destination symlinks, an intermediate symlink, a FIFO, a non-empty
destination and an out-of-root destination before copying any byte.

The test sets its disposable `/tmp` parent to mode `0755` only because it uses
host `setpriv` rather than Docker to exercise numeric UID 65532. That is a test
fixture exception, not production guidance. The production migration root stays
root-owned mode `0700` and uses direct target bind mounts as described above.
The test uses only `/tmp/new-api-non-root-volume-*` and never discovers or mounts
a live Docker volume.
