#!/bin/sh
set -e

# Fix data directory permissions when running as root.
# Docker named volumes / host bind-mounts may be owned by root,
# preventing the non-root sub2api user from writing files.
if [ "$(id -u)" = "0" ]; then
    mkdir -p /app/data
    # Use || true to avoid failure on read-only mounted files (e.g. config.yaml:ro)
    chown -R sub2api:sub2api /app/data 2>/dev/null || true
    # Re-invoke this script as sub2api so the flag-detection below
    # also runs under the correct user.
    exec su-exec sub2api "$0" "$@"
fi

# The upstream browser helper (Grok SSO approval) runs Chromium headed by
# default. Provide a private virtual display when none is configured and
# headless mode was not explicitly requested. The backend forwards DISPLAY
# to the helper; Xvfb listens only on its local Unix socket.
if [ -z "${DISPLAY:-}" ] && [ "${GOVERNANCE_BROWSER_HEADLESS:-}" != "true" ] && command -v Xvfb >/dev/null 2>&1; then
    Xvfb :99 -screen 0 1280x800x24 -nolisten tcp -ac >/dev/null 2>&1 &
    export DISPLAY=:99
fi

# Compatibility: if the first arg looks like a flag (e.g. --help),
# prepend the default binary so it behaves the same as the old
# ENTRYPOINT ["/app/sub2api"] style.
if [ "${1#-}" != "$1" ]; then
    set -- /app/sub2api "$@"
fi

exec "$@"
