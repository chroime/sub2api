#!/bin/bash

set -euo pipefail

TEST_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DEPLOY_DIR="$(cd "${TEST_DIR}/.." && pwd)"
SCRIPT="${DEPLOY_DIR}/migrate-sub2api-docker-nginx.sh"

fail() {
    printf 'FAIL: %s\n' "$*" >&2
    exit 1
}

assert_contains() {
    local haystack=$1
    local needle=$2
    grep -Fq -- "${needle}" <<<"${haystack}" || fail "Expected output to contain: ${needle}"
}

assert_not_contains() {
    local haystack=$1
    local needle=$2
    if grep -Fq -- "${needle}" <<<"${haystack}"; then
        fail "Expected output not to contain: ${needle}"
    fi
}

[[ -f "${SCRIPT}" ]] || fail "Migration script is missing: ${SCRIPT}"

bash -n "${SCRIPT}"

help_output="$("${SCRIPT}" --help)"
assert_contains "${help_output}" "Sub2API Docker + Nginx migration"
assert_contains "${help_output}" "pre-sync"
assert_contains "${help_output}" "cutover"
assert_contains "${help_output}" "verify"
assert_contains "${help_output}" "rollback"
assert_contains "${help_output}" "SRC_PASSWORD"
assert_contains "${help_output}" "DST_PASSWORD"

source_password="source-test-secret"
target_password="target-test-secret"
assumed_password="assumed-test-secret"
config_output="$(
    SRC_PASSWORD="${source_password}" \
    DST_PASSWORD="${target_password}" \
    ASSUMED_PASSWORD="${assumed_password}" \
        "${SCRIPT}" print-config
)"
assert_contains "${config_output}" "SRC_HOST=<SOURCE_SERVER_IP>"
assert_contains "${config_output}" "DST_HOST=<TARGET_SERVER_IP>"
assert_contains "${config_output}" "SRC_USER=root"
assert_contains "${config_output}" "DST_USER=ubuntu"
assert_contains "${config_output}" "SRC_SSH_PORT=52222"
assert_contains "${config_output}" "DST_SSH_PORT=22"
assert_contains "${config_output}" "APP_DIR=/home/sub2api"
assert_contains "${config_output}" "ASSUMED_PASSWORD=<set:${#assumed_password} chars>"
assert_contains "${config_output}" "SRC_PASSWORD=<set:${#source_password} chars>"
assert_contains "${config_output}" "DST_PASSWORD=<set:${#target_password} chars>"
assert_not_contains "${config_output}" "${source_password}"
assert_not_contains "${config_output}" "${target_password}"
assert_not_contains "${config_output}" "${assumed_password}"
assert_contains "${config_output}" "RSYNC_PATH=sudo rsync"
assert_contains "${config_output}" "NGINX_PATHS=/etc/nginx /etc/letsencrypt /var/www"

script_source="$(cat "${SCRIPT}")"
assert_contains "${script_source}" 'SRC_PASSWORD="${SRC_PASSWORD:-${SSH_PASSWORD:-}}"'
assert_contains "${script_source}" 'DST_PASSWORD="${DST_PASSWORD:-${SSH_PASSWORD:-}}"'
assert_contains "${script_source}" "install_docker_with_compose"
assert_contains "${script_source}" "docker-compose-plugin"
assert_contains "${script_source}" "docker-compose-v2"
assert_contains "${script_source}" "docker-compose"
assert_contains "${script_source}" "config --images"
assert_contains "${script_source}" 'remote_dst_sudo "mkdir -p $(shell_quote "$(dirname "${image_tar}")")"'

non_loopback_ipv4="$(
    grep -Eo '\b([0-9]{1,3}\.){3}[0-9]{1,3}\b' "${SCRIPT}" \
        | grep -Fvx '127.0.0.1' \
        || true
)"
[[ -z "${non_loopback_ipv4}" ]] || fail "Migration script contains a non-loopback IPv4 address"

if grep -Fq "apt-get install -y docker.io docker-compose-plugin" "${SCRIPT}"; then
    fail "Docker install must not fail when docker-compose-plugin is absent from Ubuntu apt repositories"
fi

if grep -Fq 'images -q | sort -u' "${SCRIPT}"; then
    fail "Docker image export must not pass bare 64-character image IDs to docker save"
fi

tmp_dir="$(mktemp -d)"
trap 'rm -rf "${tmp_dir}"' EXIT
mock_bin="${tmp_dir}/bin"
mock_state="${tmp_dir}/state"
mkdir -p "${mock_bin}" "${mock_state}"

awk '
    /^target_dependency_script\(\)/ { in_function=1 }
    in_function && /^    cat <<'"'"'REMOTE'"'"'$/ { in_remote=1; next }
    in_remote && /^REMOTE$/ { exit }
    in_remote { sub(/^    /, ""); print }
' "${SCRIPT}" > "${tmp_dir}/target-deps.sh"

cat > "${mock_bin}/apt-cache" <<'MOCK'
#!/bin/sh
[ "$1" = "show" ] || exit 1
[ "$2" = "docker-compose-v2" ] && exit 0
exit 100
MOCK

cat > "${mock_bin}/apt-get" <<'MOCK'
#!/bin/sh
printf '%s\n' "$*" >> "${MOCK_STATE}/apt-get.log"
case "$*" in
  *docker-compose-v2*) : > "${MOCK_STATE}/compose-ready" ;;
esac
exit 0
MOCK

cat > "${mock_bin}/docker" <<'MOCK'
#!/bin/sh
if [ "$1" = "compose" ] && [ "$2" = "version" ]; then
    [ -f "${MOCK_STATE}/compose-ready" ] && exit 0
    exit 1
fi
exit 0
MOCK

cat > "${mock_bin}/systemctl" <<'MOCK'
#!/bin/sh
exit 0
MOCK

for command_name in rsync tar gzip curl; do
    cat > "${mock_bin}/${command_name}" <<'MOCK'
#!/bin/sh
exit 0
MOCK
done
chmod +x "${mock_bin}"/*

PATH="${mock_bin}:${PATH}" MOCK_STATE="${mock_state}" \
    INSTALL_DEPS_REMOTE=true MIGRATE_NGINX_REMOTE=false bash "${tmp_dir}/target-deps.sh"

fallback_log="$(cat "${mock_state}/apt-get.log")"
assert_contains "${fallback_log}" "install -y docker-compose-v2"
if grep -Fq "docker-compose-plugin" "${mock_state}/apt-get.log"; then
    fail "Unavailable docker-compose-plugin must not be passed to apt-get"
fi

if "${SCRIPT}" not-a-real-action >/dev/null 2>&1; then
    fail "Invalid action unexpectedly succeeded"
fi

printf 'Sub2API migration script tests passed.\n'
