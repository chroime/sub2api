#!/bin/sh
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
grep -Fq 'data-app.tar.gz' "$script" || fail 'script must archive application data'
grep -Fq 'sha256sum' "$script" || fail 'script must verify backup checksums'
grep -Fq 'pull --ff-only' "$script" || fail 'script must update only with fast-forward pulls'
grep -Fq 'backup_complete=true' "$script" || fail 'script must mark a completed backup'

bash -n "$script" || fail 'script has shell syntax errors'
printf 'update-sub2api test passed\n'
