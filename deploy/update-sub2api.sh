#!/usr/bin/env bash
set -Eeuo pipefail

# Update a source checkout without touching persistent application data.
# The backup is created and verified before git or Docker state is changed.

APP_DIR="${APP_DIR:-/home/sub2api/app}"
DEPLOY_DIR="${DEPLOY_DIR:-/home/sub2api/deploy}"
DATA_DIR="${DATA_DIR:-/home/sub2api/data}"
BACKUP_ROOT="${BACKUP_ROOT:-/home/sub2api/backups}"
BRANCH="${BRANCH:-b2}"
COMPOSE_FILE="${COMPOSE_FILE:-$DEPLOY_DIR/docker-compose.yml}"
SERVICE="${SERVICE:-sub2api}"
POSTGRES_CONTAINER="${POSTGRES_CONTAINER:-sub2api-postgres}"
REDIS_CONTAINER="${REDIS_CONTAINER:-sub2api-redis}"
SERVER_URL="${SERVER_URL:-http://127.0.0.1:8080}"
HEALTH_URL="${HEALTH_URL:-$SERVER_URL/health}"
SETUP_STATUS_URL="${SETUP_STATUS_URL:-$SERVER_URL/setup/status}"

BACKUP_DIR=""
BACKUP_COMPLETE=false

log() {
  printf '[update-sub2api] %s\n' "$*"
}

fail() {
  printf '[update-sub2api] ERROR: %s\n' "$*" >&2
  exit 1
}

on_exit() {
  local status=$?
  if [[ $status -ne 0 && -n "$BACKUP_DIR" ]]; then
    if [[ "$BACKUP_COMPLETE" == true ]]; then
      printf 'update_exit_status=%s\n' "$status" > "$BACKUP_DIR/UPDATE_FAILED" 2>/dev/null || true
      chmod 600 "$BACKUP_DIR/UPDATE_FAILED" 2>/dev/null || true
      printf '[update-sub2api] Update failed; verified backup retained at %s\n' "$BACKUP_DIR" >&2
    else
      printf 'backup_complete=false\nupdate_exit_status=%s\n' "$status" >> "$BACKUP_DIR/FAILED" 2>/dev/null || true
      chmod 600 "$BACKUP_DIR/FAILED" 2>/dev/null || true
      printf '[update-sub2api] Incomplete backup retained at %s\n' "$BACKUP_DIR" >&2
    fi
  fi
  exit "$status"
}
trap on_exit EXIT

require_command() {
  command -v "$1" >/dev/null 2>&1 || fail "required command is missing: $1"
}

for command in git docker curl tar sha256sum head stat grep; do
  require_command "$command"
done

[[ -d "$APP_DIR/.git" ]] || fail "application checkout not found: $APP_DIR"
[[ -f "$DEPLOY_DIR/.env" ]] || fail "deployment environment file not found: $DEPLOY_DIR/.env"
[[ -f "$COMPOSE_FILE" ]] || fail "compose file not found: $COMPOSE_FILE"
[[ -d "$DATA_DIR/app" ]] || fail "application data directory not found: $DATA_DIR/app"
[[ -f "$DATA_DIR/app/.installed" ]] || fail "installation marker not found: $DATA_DIR/app/.installed"

if [[ -n "$(git -C "$APP_DIR" status --porcelain)" ]]; then
  fail "application checkout has uncommitted changes: $APP_DIR"
fi

(cd "$DEPLOY_DIR" && docker compose -f "$COMPOSE_FILE" config >/dev/null) || \
  fail "Docker Compose configuration is invalid"

STAMP="$(date -u +%Y%m%dT%H%M%SZ)"
BACKUP_DIR="$BACKUP_ROOT/pre-update-$STAMP"
while [[ -e "$BACKUP_DIR" ]]; do
  BACKUP_DIR="${BACKUP_DIR}-$$"
done
mkdir -p "$BACKUP_DIR"
chmod 700 "$BACKUP_DIR"

BEFORE_COMMIT="$(git -C "$APP_DIR" rev-parse HEAD)"
BEFORE_BRANCH="$(git -C "$APP_DIR" branch --show-current)"
CREATED_AT="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

log "Creating pre-update backup: $BACKUP_DIR"
printf 'backup_dir=%s\nbefore_commit=%s\nbefore_branch=%s\ncreated_at_utc=%s\ninstalled_marker=%s\nbackup_complete=false\n' \
  "$BACKUP_DIR" "$BEFORE_COMMIT" "$BEFORE_BRANCH" "$CREATED_AT" "$DATA_DIR/app/.installed" > "$BACKUP_DIR/metadata.txt"

cp -p "$DEPLOY_DIR/.env" "$BACKUP_DIR/deploy.env"
cp -p "$COMPOSE_FILE" "$BACKUP_DIR/docker-compose.yml"

log "Dumping PostgreSQL"
docker exec "$POSTGRES_CONTAINER" sh -ceu 'PGPASSWORD="${POSTGRES_PASSWORD:-}" pg_dump -h 127.0.0.1 -U "${POSTGRES_USER:-sub2api}" -d "${POSTGRES_DB:-sub2api}" -Fc' > "$BACKUP_DIR/postgres.dump"
docker exec "$POSTGRES_CONTAINER" sh -ceu 'PGPASSWORD="${POSTGRES_PASSWORD:-}" pg_dumpall -h 127.0.0.1 -U "${POSTGRES_USER:-sub2api}" --globals-only' > "$BACKUP_DIR/postgres-globals.sql"

log "Snapshotting Redis"
REDIS_RDB_PATH="/tmp/sub2api-pre-update-$$.rdb"
REDIS_LOG_PATH="/tmp/sub2api-pre-update-$$.log"
if docker exec "$REDIS_CONTAINER" sh -c 'test -n "${REDISCLI_AUTH:-}" || test -n "${REDIS_PASSWORD:-}"'; then
  if ! docker exec "$REDIS_CONTAINER" sh -c "set -eu; rm -f '$REDIS_RDB_PATH' '$REDIS_LOG_PATH'; if [ -z \"\${REDISCLI_AUTH:-}\" ]; then export REDISCLI_AUTH=\"\${REDIS_PASSWORD:-}\"; fi; if ! redis-cli --rdb '$REDIS_RDB_PATH' > '$REDIS_LOG_PATH' 2>&1; then cat '$REDIS_LOG_PATH' >&2; exit 1; fi; test -s '$REDIS_RDB_PATH'"; then
    docker exec "$REDIS_CONTAINER" rm -f "$REDIS_RDB_PATH" "$REDIS_LOG_PATH" >/dev/null 2>&1 || true
    fail "Redis RDB export failed"
  fi
else
  if ! docker exec "$REDIS_CONTAINER" sh -c "set -eu; rm -f '$REDIS_RDB_PATH' '$REDIS_LOG_PATH'; unset REDISCLI_AUTH REDIS_PASSWORD; if ! redis-cli --rdb '$REDIS_RDB_PATH' > '$REDIS_LOG_PATH' 2>&1; then cat '$REDIS_LOG_PATH' >&2; exit 1; fi; test -s '$REDIS_RDB_PATH'"; then
    docker exec "$REDIS_CONTAINER" rm -f "$REDIS_RDB_PATH" "$REDIS_LOG_PATH" >/dev/null 2>&1 || true
    fail "Redis RDB export failed"
  fi
fi
docker cp "$REDIS_CONTAINER:$REDIS_RDB_PATH" "$BACKUP_DIR/redis.rdb" >/dev/null
docker exec "$REDIS_CONTAINER" rm -f "$REDIS_RDB_PATH" "$REDIS_LOG_PATH" >/dev/null

log "Archiving application data"
tar --xattrs --acls --numeric-owner -czf "$BACKUP_DIR/data-app.tar.gz" -C "$DATA_DIR" app

for file in data-app.tar.gz deploy.env docker-compose.yml metadata.txt postgres-globals.sql postgres.dump redis.rdb; do
  [[ -s "$BACKUP_DIR/$file" ]] || fail "backup file is empty or missing: $file"
done

tar -tzf "$BACKUP_DIR/data-app.tar.gz" >/dev/null || fail "application data archive is not readable"
[[ "$(head -c 5 "$BACKUP_DIR/redis.rdb")" == "REDIS" ]] || fail "Redis snapshot does not have an RDB header"
REDIS_CHECK_PATH="/tmp/sub2api-pre-update-$$.rdb"
docker cp "$BACKUP_DIR/redis.rdb" "$REDIS_CONTAINER:$REDIS_CHECK_PATH" >/dev/null
if ! docker exec "$REDIS_CONTAINER" redis-check-rdb "$REDIS_CHECK_PATH" > "$BACKUP_DIR/redis-check.txt"; then
  docker exec "$REDIS_CONTAINER" rm -f "$REDIS_CHECK_PATH" >/dev/null 2>&1 || true
  fail "Redis snapshot failed redis-check-rdb validation"
fi
docker exec "$REDIS_CONTAINER" rm -f "$REDIS_CHECK_PATH" >/dev/null
[[ -s "$BACKUP_DIR/redis-check.txt" ]] || fail "Redis snapshot validation returned no report"

log "Validating PostgreSQL archive"
POSTGRES_DUMP_PATH="/tmp/sub2api-pre-update-$$.dump"
docker cp "$BACKUP_DIR/postgres.dump" "$POSTGRES_CONTAINER:$POSTGRES_DUMP_PATH" >/dev/null
if ! docker exec "$POSTGRES_CONTAINER" pg_restore -l "$POSTGRES_DUMP_PATH" > "$BACKUP_DIR/postgres-toc.txt"; then
  docker exec "$POSTGRES_CONTAINER" rm -f "$POSTGRES_DUMP_PATH" >/dev/null 2>&1 || true
  fail "PostgreSQL archive is not readable"
fi
docker exec "$POSTGRES_CONTAINER" rm -f "$POSTGRES_DUMP_PATH" >/dev/null
TOC_COUNT="$(wc -l < "$BACKUP_DIR/postgres-toc.txt")"
REDIS_BYTES="$(stat -c %s "$BACKUP_DIR/redis.rdb")"
du -sh "$BACKUP_DIR" > "$BACKUP_DIR/size.txt"

printf 'redis_snapshot_bytes=%s\npostgres_toc_entries=%s\n' "$REDIS_BYTES" "$TOC_COUNT" >> "$BACKUP_DIR/metadata.txt"
sha256sum \
  "$BACKUP_DIR/data-app.tar.gz" \
  "$BACKUP_DIR/deploy.env" \
  "$BACKUP_DIR/docker-compose.yml" \
  "$BACKUP_DIR/metadata.txt" \
  "$BACKUP_DIR/postgres-globals.sql" \
  "$BACKUP_DIR/postgres.dump" \
  "$BACKUP_DIR/postgres-toc.txt" \
  "$BACKUP_DIR/redis-check.txt" \
  "$BACKUP_DIR/redis.rdb" \
  "$BACKUP_DIR/size.txt" > "$BACKUP_DIR/SHA256SUMS"
(cd "$BACKUP_DIR" && sha256sum -c SHA256SUMS)
sed -i 's/^backup_complete=false$/backup_complete=true/' "$BACKUP_DIR/metadata.txt"
sha256sum \
  "$BACKUP_DIR/data-app.tar.gz" \
  "$BACKUP_DIR/deploy.env" \
  "$BACKUP_DIR/docker-compose.yml" \
  "$BACKUP_DIR/metadata.txt" \
  "$BACKUP_DIR/postgres-globals.sql" \
  "$BACKUP_DIR/postgres.dump" \
  "$BACKUP_DIR/postgres-toc.txt" \
  "$BACKUP_DIR/redis-check.txt" \
  "$BACKUP_DIR/redis.rdb" \
  "$BACKUP_DIR/size.txt" > "$BACKUP_DIR/SHA256SUMS"
(cd "$BACKUP_DIR" && sha256sum -c SHA256SUMS)
chmod 600 "$BACKUP_DIR"/*
BACKUP_COMPLETE=true
log "Backup verified: $BACKUP_DIR (PostgreSQL TOC: $TOC_COUNT, Redis: $REDIS_BYTES bytes)"

log "Fetching origin/$BRANCH"
git -C "$APP_DIR" fetch origin "$BRANCH"
git -C "$APP_DIR" switch "$BRANCH"
git -C "$APP_DIR" pull --ff-only origin "$BRANCH"

log "Building and recreating $SERVICE"
(cd "$DEPLOY_DIR" && docker compose -f "$COMPOSE_FILE" up -d --build --force-recreate "$SERVICE")

log "Waiting for $SERVICE healthcheck"
HEALTH_STATUS=""
for ((attempt = 1; attempt <= 24; attempt++)); do
  HEALTH_STATUS="$(docker inspect -f '{{.State.Health.Status}}' "$SERVICE" 2>/dev/null || true)"
  [[ "$HEALTH_STATUS" == "healthy" ]] && break
  [[ "$HEALTH_STATUS" == "unhealthy" ]] && break
  sleep 5
done
[[ "$HEALTH_STATUS" == "healthy" ]] || {
  docker logs --tail=80 "$SERVICE" >&2 || true
  fail "service did not become healthy (status: ${HEALTH_STATUS:-unknown})"
}

DATA_MOUNT="$(docker inspect -f '{{range .Mounts}}{{println .Source .Destination}}{{end}}' "$SERVICE" 2>/dev/null || true)"
[[ "$DATA_MOUNT" == *"$DATA_DIR/app /app/data"* ]] || fail "application data mount changed: ${DATA_MOUNT:-unknown}"
[[ -f "$DATA_DIR/app/.installed" ]] || fail "installation marker disappeared after update"

HEALTH_BODY="$(curl -fsS --max-time 10 "$HEALTH_URL")"
printf '%s' "$HEALTH_BODY" | grep -Eq '"status"[[:space:]]*:[[:space:]]*"ok"' || fail "health endpoint returned an unexpected response: $HEALTH_BODY"
SETUP_BODY="$(curl -fsS --max-time 10 "$SETUP_STATUS_URL")"
[[ "$SETUP_BODY" =~ "needs_setup"[[:space:]]*:[[:space:]]*false ]] || fail "setup status is not completed: $SETUP_BODY"

log "Update complete: $(git -C "$APP_DIR" rev-parse HEAD)"
(cd "$DEPLOY_DIR" && docker compose -f "$COMPOSE_FILE" ps "$SERVICE")
