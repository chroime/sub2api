#!/bin/sh
set -eu

repo_root=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
cd "$repo_root"

fail() {
  printf 'docker runtime resources test failed: %s\n' "$1" >&2
  exit 1
}

assert_line() {
  file=$1
  line=$2
  grep -Fqx "$line" "$file" || fail "$file is missing: $line"
}

assert_count() {
  file=$1
  line=$2
  expected=$3
  actual=$(grep -Fxc "$line" "$file" || true)
  [ "$actual" -eq "$expected" ] || fail "$file has $actual occurrences of '$line', expected $expected"
}

test -s backend/resources/model-pricing/model_prices_and_context_window.json || \
  fail 'fallback pricing data is missing or empty'

assert_line Dockerfile.goreleaser 'COPY --chown=sub2api:sub2api backend/resources /app/resources'
assert_line deploy/Dockerfile 'COPY --from=backend-builder --chown=sub2api:sub2api /app/backend/resources /app/resources'
for dockerfile in Dockerfile Dockerfile.goreleaser; do
  assert_line "$dockerfile" 'ARG INSTALL_UPSTREAM_BROWSER=true'
  assert_line "$dockerfile" 'COPY --from=browser-helper /opt/upstream-browser /app/upstream-browser'
  assert_line "$dockerfile" 'RUN chmod -R a+rX /app/upstream-browser'
  assert_line "$dockerfile" 'ENV GOVERNANCE_BROWSER_NODE=/usr/bin/node \'
  assert_line "$dockerfile" '    GOVERNANCE_BROWSER_SCRIPT=/app/upstream-browser/main.mjs \'
  assert_line "$dockerfile" '    GOVERNANCE_BROWSER_EXECUTABLE=/usr/local/bin/sub2api-chromium'
  assert_line "$dockerfile" 'COPY --from=pg-client /usr/local/bin/pg_dump /usr/local/bin/pg_dump'
  assert_line "$dockerfile" 'COPY --from=pg-client /usr/local/bin/psql /usr/local/bin/psql'
  grep -Fq 'apk add --no-cache nodejs chromium xvfb' "$dockerfile" || fail "$dockerfile is missing browser runtime dependencies"
done
assert_count .goreleaser.yaml '      - backend/resources' 4
assert_count .goreleaser.simple.yaml '      - backend/resources' 1

printf 'docker runtime resources test passed\n'
