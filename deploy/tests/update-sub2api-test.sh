#!/usr/bin/env bash
set -eu

repo_root=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
script="$repo_root/deploy/update-sub2api.sh"

fail() {
  printf 'update-sub2api test failed: %s\n' "$1" >&2
  exit 1
}

[ -f "$script" ] || fail 'deploy/update-sub2api.sh is missing'
grep -Fqx '#!/usr/bin/env bash' "$script" || fail 'script must use bash with strict mode support'
grep -Fq 'set -Eeuo pipefail' "$script" || fail 'script must enable strict mode'
grep -Fq 'pg_dump' "$script" || fail 'script must create a PostgreSQL dump'
grep -Fq 'pg_dumpall' "$script" || fail 'script must back up PostgreSQL globals'
grep -Fq 'redis-cli --rdb' "$script" || fail 'script must create a Redis RDB snapshot'
grep -Fq 'REDISCLI_AUTH' "$script" || fail 'script must handle Redis CLI authentication environment'
grep -Fq 'redis-check-rdb' "$script" || fail 'script must validate the Redis snapshot'
grep -Fq 'docker cp "$REDIS_CONTAINER:$REDIS_RDB_PATH"' "$script" || fail 'script must copy the Redis snapshot from the container'
grep -Fq 'REDIS_COMMAND=' "$script" && fail 'script must not build a dynamic Redis shell command'
grep -Fq 'data-app.tar.gz' "$script" || fail 'script must archive application data'
grep -Fq 'sha256sum' "$script" || fail 'script must verify backup checksums'
grep -Fq 'pull --ff-only' "$script" || fail 'script must update only with fast-forward pulls'
grep -Fq 'backup_complete=true' "$script" || fail 'script must mark a completed backup'

bash -n "$script" || fail 'script has shell syntax errors'
grep -Fq 'BRANCH="${BRANCH:-main}"' "$script" || fail 'default branch must be main'

# Source helpers only. This must not invoke Docker, Git or install an EXIT trap.
source "$script"
[[ -z "$(trap -p EXIT)" ]] || fail 'sourcing helpers must not install deployment traps'
report=$(mktemp)
trap 'rm -f "$report"' EXIT
printf 'tar: app/logs/sub2api.log: file changed as we read it\n' > "$report"
tar_errors_are_log_changes 1 "$report" || fail 'live-log changes should be accepted'
if tar_errors_are_log_changes 2 "$report"; then fail 'tar fatal errors must fail'; fi
printf 'tar: app/config.yaml: file changed as we read it\n' >> "$report"
if tar_errors_are_log_changes 1 "$report"; then fail 'non-log changes must fail'; fi
printf 'tar: app/logs/sub2api.log: Cannot open: Permission denied\n' > "$report"
if tar_errors_are_log_changes 1 "$report"; then fail 'unreadable log files must fail'; fi
: > "$report"
if tar_errors_are_log_changes 1 "$report"; then fail 'unexplained tar errors must fail'; fi

backup_line=$(grep -n '^BACKUP_COMPLETE=true$' "$script" | cut -d: -f1)
init_line=$(grep -n '^homepage_template_sql | docker exec' "$script" | cut -d: -f1)
checkout_line=$(grep -n '^git -C .* switch' "$script" | cut -d: -f1)
[[ "$backup_line" -lt "$init_line" && "$init_line" -lt "$checkout_line" ]] || fail 'verified backup must precede settings writes and checkout'
runtime_line=$(grep -n '^log "Checking runtime file access' "$script" | cut -d: -f1)
recreate_line=$(grep -n '^log "Recreating' "$script" | cut -d: -f1)
[[ "$runtime_line" -lt "$recreate_line" ]] || fail 'runtime file access must be checked before replacing the live service'
grep -Fq 'chmod 755 /app/docker-entrypoint.sh' "$repo_root/Dockerfile" || fail 'entrypoint must remain readable after dropping privileges'
grep -Fq 'chmod -R a+rX /app/upstream-browser' "$repo_root/Dockerfile" || fail 'helper must be readable from a private checkout'
homepage_template_sql | grep -Fq 'ON CONFLICT (key) DO NOTHING' || fail 'initialization must preserve explicit settings'
printf 'update-sub2api tests passed\n'
