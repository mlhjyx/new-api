#!/usr/bin/env bash
set -Eeuo pipefail

umask 077

readonly runtime_uid=65532
readonly runtime_gid=65532

fail() {
  printf 'FAIL non-root-volume-upgrade: %s\n' "$*" >&2
  exit 1
}

for command_name in awk cat chmod chown find install kill ln mkfifo mktemp python3 realpath seq setpriv sha256sum sh sleep sort stat tar; do
  command -v "${command_name}" >/dev/null 2>&1 || fail "missing command: ${command_name}"
done

[[ "${EUID}" -eq 0 ]] || fail 'run as root so the fixture can reproduce a root-owned predecessor volume'

test_root=$(mktemp -d /tmp/new-api-non-root-volume-upgrade.XXXXXX)
escape_root=$(mktemp -d /tmp/new-api-non-root-volume-escape.XXXXXX)
sqlite_writer_pid=''

safe_delete_tree() {
  local target=$1
  case "${target}" in
    /tmp/new-api-non-root-volume-upgrade.* | /tmp/new-api-non-root-volume-escape.*)
      if [[ -e "${target}" || -L "${target}" ]]; then
        find -P "${target}" -depth -delete
      fi
      ;;
    *)
      printf 'REFUSED cleanup outside disposable prefix: %s\n' "${target}" >&2
      ;;
  esac
}

cleanup() {
  if [[ -n "${sqlite_writer_pid}" ]] && kill -0 "${sqlite_writer_pid}" 2>/dev/null; then
    kill -KILL "${sqlite_writer_pid}" 2>/dev/null || true
    wait "${sqlite_writer_pid}" 2>/dev/null || true
  fi
  safe_delete_tree "${test_root}"
  safe_delete_tree "${escape_root}"
}
trap cleanup EXIT INT TERM

# Test-fixture exception only: host setpriv needs to traverse these disposable
# parents. The production migration root remains mode 0700; its UID check uses
# direct container bind mounts and never broadens the parent directory.
chmod 0755 "${test_root}" "${escape_root}"

canonical_child_directory() {
  local allowed_root=$1
  local candidate=$2
  local resolved_root resolved_candidate

  [[ "${allowed_root}" == /* && "${candidate}" == /* ]] || return 1
  [[ -d "${allowed_root}" && ! -L "${allowed_root}" ]] || return 1
  [[ -d "${candidate}" && ! -L "${candidate}" ]] || return 1
  resolved_root=$(realpath -e -- "${allowed_root}") || return 1
  resolved_candidate=$(realpath -e -- "${candidate}") || return 1
  [[ "${resolved_root}" == "${allowed_root}" ]] || return 1
  [[ "${resolved_candidate}" == "${candidate}" ]] || return 1
  [[ "${resolved_candidate}" == "${resolved_root}/"* ]] || return 1
}

safe_regular_tree() {
  local source=$1
  local unsafe=''
  if IFS= read -r -d '' unsafe < <(
    find -P "${source}" -mindepth 1 ! -type d ! -type f -print0 -quit
  ); then
    printf 'REFUSED non-regular source entry: %s\n' "${unsafe}" >&2
    return 1
  fi
}

empty_directory() {
  local directory=$1
  local entry=''
  if IFS= read -r -d '' entry < <(find -P "${directory}" -mindepth 1 -print0 -quit); then
    printf 'REFUSED non-empty destination: %s\n' "${directory}" >&2
    return 1
  fi
}

copy_tree_once() {
  local allowed_root=$1
  local source=$2
  local destination=$3

  canonical_child_directory "${allowed_root}" "${source}" || return 1
  canonical_child_directory "${allowed_root}" "${destination}" || return 1
  safe_regular_tree "${source}" || return 1
  empty_directory "${destination}" || return 1

  (
    cd "${source}"
    tar --acls --xattrs --numeric-owner -cpf - .
  ) | (
    cd "${destination}"
    tar --acls --xattrs --numeric-owner -xpf -
  )
}

tree_content_fingerprint() {
  local directory=$1
  (
    cd "${directory}"
    find -P . -type d -printf 'directory %P\n' | LC_ALL=C sort
    while IFS= read -r -d '' file_path; do
      printf 'file %s ' "${file_path}"
      sha256sum -- "${file_path}" | awk '{print $1}'
    done < <(find -P . -type f -print0 | LC_ALL=C sort -z)
  ) | sha256sum | awk '{print $1}'
}

tree_metadata_fingerprint() {
  local directory=$1
  find -P "${directory}" -printf '%P|%y|%D|%i|%u|%g|%m|%s|%T@|%C@\n' |
    LC_ALL=C sort |
    sha256sum |
    awk '{print $1}'
}

old_data="${test_root}/old-data"
old_logs="${test_root}/old-logs"
backup_data="${test_root}/backup-data"
backup_logs="${test_root}/backup-logs"
new_data="${test_root}/new-data"
new_logs="${test_root}/new-logs"
rollback_data="${test_root}/rollback-data"
rollback_logs="${test_root}/rollback-logs"
sqlite_ready="${test_root}/sqlite-ready"

install -d -m 0700 -o 0 -g 0 \
  "${old_data}" "${old_logs}" \
  "${backup_data}" "${backup_logs}" \
  "${new_data}" "${new_logs}" \
  "${rollback_data}" "${rollback_logs}"

# Keep an actual SQLite connection alive long enough to commit a WAL record,
# then simulate a stopped/crashed predecessor without a clean final checkpoint.
python3 - "${old_data}/one-api.db" "${sqlite_ready}" <<'PY' &
import pathlib
import sqlite3
import sys
import time

database_path = sys.argv[1]
ready_path = pathlib.Path(sys.argv[2])
connection = sqlite3.connect(database_path)
mode = connection.execute("PRAGMA journal_mode=WAL").fetchone()[0]
if mode.lower() != "wal":
    raise SystemExit("WAL mode unavailable")
connection.execute("PRAGMA wal_autocheckpoint=0")
connection.execute("CREATE TABLE volume_upgrade_proof(value TEXT NOT NULL)")
connection.execute("INSERT INTO volume_upgrade_proof(value) VALUES ('before-upgrade')")
connection.commit()
ready_path.write_text("ready", encoding="utf-8")
while True:
    time.sleep(60)
PY
sqlite_writer_pid=$!

for _ in $(seq 1 100); do
  [[ -f "${sqlite_ready}" ]] && break
  sleep 0.05
done
[[ -f "${sqlite_ready}" ]] || fail 'SQLite WAL fixture did not become ready'
kill -KILL "${sqlite_writer_pid}"
wait "${sqlite_writer_pid}" 2>/dev/null || true
sqlite_writer_pid=''

[[ -s "${old_data}/one-api.db" ]] || fail 'SQLite database was not created'
[[ -s "${old_data}/one-api.db-wal" ]] || fail 'SQLite WAL was not retained'
[[ -f "${old_data}/one-api.db-shm" ]] || fail 'SQLite shared-memory file was not retained'
printf 'root-owned-log\n' >"${old_logs}/new-api.log"
printf 'rotated-log\n' >"${old_logs}/new-api.log.1"
install -d -m 0700 -o 0 -g 0 "${old_logs}/archive"
printf 'archived-log\n' >"${old_logs}/archive/new-api.log.2"
chown -R 0:0 "${old_data}" "${old_logs}"
chmod -R u=rwX,go= "${old_data}" "${old_logs}"
wrong_source_owner=''
if IFS= read -r -d '' wrong_source_owner < <(
  find -P "${old_data}" "${old_logs}" \( ! -uid 0 -o ! -gid 0 \) -print0 -quit
); then
  fail "predecessor fixture is not root-owned: ${wrong_source_owner}"
fi

# Permanent regression for the original failure: the predecessor tree is
# intentionally inaccessible to the future runtime UID before migration.
if setpriv --reuid="${runtime_uid}" --regid="${runtime_gid}" --clear-groups \
  python3 - "${old_data}/one-api.db" >/dev/null 2>&1 <<'PY'
import sqlite3
import sys

connection = sqlite3.connect(sys.argv[1])
connection.execute("BEGIN IMMEDIATE")
connection.execute("ROLLBACK")
connection.close()
PY
then
  fail 'root-owned predecessor SQLite unexpectedly allowed UID 65532 to write'
fi
if setpriv --reuid="${runtime_uid}" --regid="${runtime_gid}" --clear-groups \
  sh -c 'printf "must-not-write\n" >>"$1"' sh "${old_logs}/new-api.log" >/dev/null 2>&1; then
  fail 'root-owned predecessor log unexpectedly allowed UID 65532 to write'
fi

source_data_before=$(tree_content_fingerprint "${old_data}")
source_logs_before=$(tree_content_fingerprint "${old_logs}")
source_data_metadata_before=$(tree_metadata_fingerprint "${old_data}")
source_logs_metadata_before=$(tree_metadata_fingerprint "${old_logs}")

copy_tree_once "${test_root}" "${old_data}" "${backup_data}"
copy_tree_once "${test_root}" "${old_logs}" "${backup_logs}"
copy_tree_once "${test_root}" "${old_data}" "${new_data}"
copy_tree_once "${test_root}" "${old_logs}" "${new_logs}"

[[ "$(tree_content_fingerprint "${backup_data}")" == "${source_data_before}" ]] || fail 'data backup differs from source'
[[ "$(tree_content_fingerprint "${backup_logs}")" == "${source_logs_before}" ]] || fail 'log backup differs from source'
[[ "$(tree_content_fingerprint "${new_data}")" == "${source_data_before}" ]] || fail 'new data volume differs before runtime write'
[[ "$(tree_content_fingerprint "${new_logs}")" == "${source_logs_before}" ]] || fail 'new log volume differs before runtime write'
[[ "$(stat -c '%u:%g' "${backup_data}/one-api.db")" == '0:0' ]] || fail 'backup ownership was not preserved'
wrong_backup_owner=''
if IFS= read -r -d '' wrong_backup_owner < <(
  find -P "${backup_data}" "${backup_logs}" \( ! -uid 0 -o ! -gid 0 \) -print0 -quit
); then
  fail "backup entry has wrong owner: ${wrong_backup_owner}"
fi

chown -R "${runtime_uid}:${runtime_gid}" "${new_data}" "${new_logs}"
wrong_owner=''
if IFS= read -r -d '' wrong_owner < <(
  find -P "${new_data}" "${new_logs}" \( ! -uid "${runtime_uid}" -o ! -gid "${runtime_gid}" \) -print0 -quit
); then
  fail "new volume entry has wrong owner: ${wrong_owner}"
fi

setpriv --reuid="${runtime_uid}" --regid="${runtime_gid}" --clear-groups \
  python3 - "${new_data}/one-api.db" <<'PY'
import sqlite3
import sys

connection = sqlite3.connect(sys.argv[1])
if connection.execute("PRAGMA integrity_check").fetchone()[0] != "ok":
    raise SystemExit("copied SQLite integrity check failed")
values = [row[0] for row in connection.execute("SELECT value FROM volume_upgrade_proof ORDER BY rowid")]
if values != ["before-upgrade"]:
    raise SystemExit(f"unexpected copied rows: {values!r}")
connection.execute("INSERT INTO volume_upgrade_proof(value) VALUES ('after-upgrade')")
connection.commit()
if connection.execute("SELECT count(*) FROM volume_upgrade_proof").fetchone()[0] != 2:
    raise SystemExit("UID 65532 SQLite write did not persist")
connection.close()
PY
setpriv --reuid="${runtime_uid}" --regid="${runtime_gid}" --clear-groups \
  sh -c 'printf "uid-65532-write\n" >>"$1"; printf "new-log\n" >"$2"' \
  sh "${new_logs}/new-api.log" "${new_logs}/runtime-created.log"

[[ "$(tree_content_fingerprint "${old_data}")" == "${source_data_before}" ]] || fail 'old data volume content changed'
[[ "$(tree_content_fingerprint "${old_logs}")" == "${source_logs_before}" ]] || fail 'old log volume content changed'
[[ "$(tree_metadata_fingerprint "${old_data}")" == "${source_data_metadata_before}" ]] || fail 'old data volume metadata changed'
[[ "$(tree_metadata_fingerprint "${old_logs}")" == "${source_logs_metadata_before}" ]] || fail 'old log volume metadata changed'
[[ "$(tree_content_fingerprint "${backup_data}")" == "${source_data_before}" ]] || fail 'data backup changed after target writes'
[[ "$(tree_content_fingerprint "${backup_logs}")" == "${source_logs_before}" ]] || fail 'log backup changed after target writes'

# Rehearse a backup restore into another fresh volume. The original old volume
# remains untouched and can also be remounted directly for rollback.
copy_tree_once "${test_root}" "${backup_data}" "${rollback_data}"
copy_tree_once "${test_root}" "${backup_logs}" "${rollback_logs}"
chown -R "${runtime_uid}:${runtime_gid}" "${rollback_data}" "${rollback_logs}"
setpriv --reuid="${runtime_uid}" --regid="${runtime_gid}" --clear-groups \
  python3 - "${rollback_data}/one-api.db" <<'PY'
import sqlite3
import sys

connection = sqlite3.connect(sys.argv[1])
values = [row[0] for row in connection.execute("SELECT value FROM volume_upgrade_proof ORDER BY rowid")]
if values != ["before-upgrade"]:
    raise SystemExit(f"rollback did not restore the original SQLite state: {values!r}")
connection.execute("BEGIN IMMEDIATE")
connection.execute("ROLLBACK")
connection.close()
PY
[[ "$(cat "${rollback_logs}/new-api.log")" == 'root-owned-log' ]] || fail 'rollback log differs from original'

# Safety mutations: a symlinked source entry, an intermediate symlink, a
# symlinked destination, and a destination outside the approved root must all
# be rejected before any byte is copied.
unsafe_source="${test_root}/unsafe-source"
unsafe_target="${test_root}/unsafe-target"
install -d -m 0700 "${unsafe_source}" "${unsafe_target}"
ln -s /etc/passwd "${unsafe_source}/escape"
if copy_tree_once "${test_root}" "${unsafe_source}" "${unsafe_target}"; then
  fail 'symlinked source entry was accepted'
fi
empty_directory "${unsafe_target}" || fail 'rejected source wrote to destination'

fifo_source="${test_root}/fifo-source"
fifo_target="${test_root}/fifo-target"
install -d -m 0700 "${fifo_source}" "${fifo_target}"
mkfifo "${fifo_source}/unexpected-fifo"
if copy_tree_once "${test_root}" "${fifo_source}" "${fifo_target}"; then
  fail 'FIFO source entry was accepted'
fi
empty_directory "${fifo_target}" || fail 'rejected FIFO source wrote to destination'

nonempty_target="${test_root}/nonempty-target"
install -d -m 0700 "${nonempty_target}"
printf 'preserve-existing-target\n' >"${nonempty_target}/existing"
nonempty_before=$(tree_content_fingerprint "${nonempty_target}")
if copy_tree_once "${test_root}" "${old_data}" "${nonempty_target}"; then
  fail 'non-empty destination was accepted'
fi
[[ "$(tree_content_fingerprint "${nonempty_target}")" == "${nonempty_before}" ]] || fail 'rejected non-empty destination was modified'
[[ "$(cat "${nonempty_target}/existing")" == 'preserve-existing-target' ]] || fail 'existing destination byte changed'

ln -s "${test_root}" "${test_root}/source-parent-link"
if copy_tree_once "${test_root}" "${test_root}/source-parent-link/old-data" "${unsafe_target}"; then
  fail 'intermediate source symlink was accepted'
fi

ln -s "${escape_root}" "${test_root}/target-link"
if copy_tree_once "${test_root}" "${old_data}" "${test_root}/target-link"; then
  fail 'symlinked destination was accepted'
fi

if copy_tree_once "${test_root}" "${old_data}" "${escape_root}"; then
  fail 'destination outside approved root was accepted'
fi
empty_directory "${escape_root}" || fail 'escape destination was modified'

printf 'PASS non-root-volume-upgrade uid=%s predecessor=denied old-volume=unchanged backup=verified rollback=verified symlink-fifo-nonempty-escape=rejected\n' "${runtime_uid}"
