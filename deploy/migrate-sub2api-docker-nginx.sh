#!/usr/bin/env bash
#
# Sub2API Docker + Nginx migration script.
#
# Default scenario:
#   - Source server: <SOURCE_SERVER_IP>:52222 as root
#   - Target server: <TARGET_SERVER_IP>:22 as ubuntu
#   - Source app directory: /home/sub2api
#   - Sub2API runs with Docker Compose
#   - Nginx is installed separately on the host and its configuration is migrated
#
# Recommended execution:
#   1. Copy this script to the source server.
#   2. Run: bash migrate-sub2api-docker-nginx.sh preflight
#   3. Run: bash migrate-sub2api-docker-nginx.sh pre-sync
#   4. Enter maintenance window.
#   5. Run: bash migrate-sub2api-docker-nginx.sh cutover --yes
#
# Password authentication:
#   The script uses sshpass when DST_PASSWORD is set.
#   Override credentials with environment variables instead of editing the file.

set -euo pipefail

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
NC='\033[0m'

SRC_HOST="${SRC_HOST:-<SOURCE_SERVER_IP>}"
DST_HOST="${DST_HOST:-<TARGET_SERVER_IP>}"
SRC_USER="${SRC_USER:-root}"
DST_USER="${DST_USER:-ubuntu}"
SRC_SSH_PORT="${SRC_SSH_PORT:-52222}"
DST_SSH_PORT="${DST_SSH_PORT:-22}"

APP_DIR="${APP_DIR:-/home/sub2api}"
COMPOSE_FILE="${COMPOSE_FILE:-}"
SERVER_PORT="${SERVER_PORT:-8080}"

SRC_PASSWORD="${SRC_PASSWORD:-${SSH_PASSWORD:-}}"
DST_PASSWORD="${DST_PASSWORD:-${SSH_PASSWORD:-}}"
ASSUMED_PASSWORD="${ASSUMED_PASSWORD:-${DST_PASSWORD}}"

MIGRATION_ID="${MIGRATION_ID:-$(date +%Y%m%d%H%M%S)}"
MIGRATION_DIR="${MIGRATION_DIR:-${APP_DIR}/_migration_${MIGRATION_ID}}"

NGINX_PATHS="${NGINX_PATHS:-/etc/nginx /etc/letsencrypt /var/www}"
EXTRA_RSYNC_PATHS="${EXTRA_RSYNC_PATHS:-}"

MIGRATE_NGINX="${MIGRATE_NGINX:-true}"
SYNC_DELETE="${SYNC_DELETE:-true}"
LOAD_IMAGES="${LOAD_IMAGES:-true}"
INSTALL_DEPS="${INSTALL_DEPS:-true}"
MIGRATE_NAMED_VOLUMES="${MIGRATE_NAMED_VOLUMES:-auto}"
START_TARGET="${START_TARGET:-true}"
TARGET_SUDO="${TARGET_SUDO:-auto}"
REMOTE_ASKPASS="${REMOTE_ASKPASS:-/tmp/sub2api-migrate-sudo-askpass.sh}"
RSYNC_PATH="${RSYNC_PATH:-sudo rsync}"
YES="${YES:-false}"
DRY_RUN="${DRY_RUN:-false}"

SSH_COMMON_OPTS=(
    -o StrictHostKeyChecking=accept-new
    -o ServerAliveInterval=30
    -o ServerAliveCountMax=10
)

usage() {
    cat <<'USAGE'
Sub2API Docker + Nginx migration

Usage:
  migrate-sub2api-docker-nginx.sh <action> [options]

Actions:
  print-config      Print effective configuration without connecting anywhere
  preflight         Check local source, target SSH, Docker Compose, rsync, Nginx
  pre-sync          Prepare target and sync /home/sub2api plus Nginx paths while source is still online
  cutover           Stop source Docker Compose, final sync, load images, start target, verify
  all               Run pre-sync and cutover
  verify            Verify target Docker Compose, HTTP health, and Nginx syntax
  rollback          Stop target Compose and start source Compose again
  remote-run        Copy this script to the source server and execute SOURCE_ACTION there
  help              Show this help

Options:
  -y, --yes                 Do not ask before cutover/rollback
  --dry-run                 Print risky sync/start/stop operations without executing them
  --no-nginx                Do not sync or restart Nginx
  --no-delete              Do not pass --delete to rsync
  --no-load-images          Do not docker save/load Compose images
  --no-start-target         Do not start target services during cutover
  --install-deps            Install missing rsync/sshpass/docker/nginx dependencies when possible
  --skip-install-deps       Do not install dependencies; only check them

Default hosts and paths:
  SRC_HOST=<SOURCE_SERVER_IP>
  DST_HOST=<TARGET_SERVER_IP>
  SRC_SSH_PORT=52222
  DST_SSH_PORT=22
  APP_DIR=/home/sub2api
  NGINX_PATHS="/etc/nginx /etc/letsencrypt /var/www"

Credential environment variables:
  SRC_USER=root
  DST_USER=ubuntu
  SRC_PASSWORD=<SOURCE_SERVER_PASSWORD>
  DST_PASSWORD=<TARGET_SERVER_PASSWORD>
  TARGET_SUDO=auto
  RSYNC_PATH="sudo rsync"

Examples:
  # Run on the source server with credentials supplied through the environment:
  bash migrate-sub2api-docker-nginx.sh preflight
  bash migrate-sub2api-docker-nginx.sh pre-sync
  bash migrate-sub2api-docker-nginx.sh cutover --yes

  # Override password safely with environment variables:
  DST_PASSWORD='<TARGET_SERVER_PASSWORD>' bash migrate-sub2api-docker-nginx.sh all --yes

  # Run from an operator machine: copy script to the source server and run preflight there:
  SRC_PASSWORD='<SOURCE_SERVER_PASSWORD>' DST_PASSWORD='<TARGET_SERVER_PASSWORD>' \
    SOURCE_ACTION=preflight bash migrate-sub2api-docker-nginx.sh remote-run

  # Roll back quickly when target has no important new writes:
  bash migrate-sub2api-docker-nginx.sh rollback --yes
USAGE
}

log_info() {
    printf '%b[INFO]%b %s\n' "${BLUE}" "${NC}" "$*"
}

log_success() {
    printf '%b[SUCCESS]%b %s\n' "${GREEN}" "${NC}" "$*"
}

log_warn() {
    printf '%b[WARN]%b %s\n' "${YELLOW}" "${NC}" "$*"
}

log_error() {
    printf '%b[ERROR]%b %s\n' "${RED}" "${NC}" "$*" >&2
}

log_step() {
    printf '\n%b==>%b %s\n' "${CYAN}" "${NC}" "$*"
}

die() {
    log_error "$*"
    exit 1
}

shell_quote() {
    printf '%q' "$1"
}

bool_true() {
    case "${1:-}" in
        1|true|TRUE|yes|YES|y|Y|on|ON) return 0 ;;
        *) return 1 ;;
    esac
}

redacted() {
    local value="${1:-}"
    if [[ -z "${value}" ]]; then
        printf '<empty>'
    else
        printf '<set:%s chars>' "${#value}"
    fi
}

require_command() {
    local command_name=$1
    command -v "${command_name}" >/dev/null 2>&1 || die "Missing required command: ${command_name}"
}

run_or_print() {
    if bool_true "${DRY_RUN}"; then
        printf 'DRY-RUN:'
        printf ' %q' "$@"
        printf '\n'
    else
        "$@"
    fi
}

has_password() {
    [[ -n "${1:-}" ]]
}

ssh_base_args_for_target() {
    printf -- '-p %q' "${DST_SSH_PORT}"
    local opt
    for opt in "${SSH_COMMON_OPTS[@]}"; do
        printf ' %q' "${opt}"
    done
}

remote_dst() {
    local command_string=$1
    if bool_true "${DRY_RUN}"; then
        printf 'DRY-RUN: ssh %s@%s %q\n' "${DST_USER}" "${DST_HOST}" "${command_string}"
        return 0
    fi

    if has_password "${DST_PASSWORD}"; then
        SSHPASS="${DST_PASSWORD}" sshpass -e ssh \
            -p "${DST_SSH_PORT}" \
            "${SSH_COMMON_OPTS[@]}" \
            "${DST_USER}@${DST_HOST}" \
            "${command_string}"
    else
        ssh \
            -p "${DST_SSH_PORT}" \
            "${SSH_COMMON_OPTS[@]}" \
            "${DST_USER}@${DST_HOST}" \
            "${command_string}"
    fi
}

target_uses_sudo() {
    if [[ "${DST_USER}" == "root" ]]; then
        return 1
    fi

    case "${TARGET_SUDO}" in
        1|true|TRUE|yes|YES|on|ON|auto) return 0 ;;
        *) return 1 ;;
    esac
}

ensure_target_askpass() {
    target_uses_sudo || return 0

    local askpass_q password_q
    askpass_q="$(shell_quote "${REMOTE_ASKPASS}")"
    password_q="$(shell_quote "${DST_PASSWORD}")"

    remote_dst "umask 077; cat > ${askpass_q} <<'SUB2API_ASKPASS'
#!/bin/sh
printf '%s\n' ${password_q}
SUB2API_ASKPASS
chmod 700 ${askpass_q}"
}

remote_dst_sudo() {
    local command_string=$1

    if target_uses_sudo; then
        ensure_target_askpass
        remote_dst "SUDO_ASKPASS=$(shell_quote "${REMOTE_ASKPASS}") sudo -A -p '' bash -lc $(shell_quote "${command_string}")"
    else
        remote_dst "${command_string}"
    fi
}

effective_rsync_path() {
    if target_uses_sudo; then
        printf 'SUDO_ASKPASS=%q sudo -A -p "" rsync' "${REMOTE_ASKPASS}"
    else
        printf 'rsync'
    fi
}

remote_src() {
    local command_string=$1
    if bool_true "${DRY_RUN}"; then
        printf 'DRY-RUN: ssh %s@%s %q\n' "${SRC_USER}" "${SRC_HOST}" "${command_string}"
        return 0
    fi

    if has_password "${SRC_PASSWORD}"; then
        SSHPASS="${SRC_PASSWORD}" sshpass -e ssh \
            -p "${SRC_SSH_PORT}" \
            "${SSH_COMMON_OPTS[@]}" \
            "${SRC_USER}@${SRC_HOST}" \
            "${command_string}"
    else
        ssh \
            -p "${SRC_SSH_PORT}" \
            "${SSH_COMMON_OPTS[@]}" \
            "${SRC_USER}@${SRC_HOST}" \
            "${command_string}"
    fi
}

scp_to_src() {
    local local_path=$1
    local remote_path=$2
    if bool_true "${DRY_RUN}"; then
        printf 'DRY-RUN: scp %q %s@%s:%q\n' "${local_path}" "${SRC_USER}" "${SRC_HOST}" "${remote_path}"
        return 0
    fi

    if has_password "${SRC_PASSWORD}"; then
        SSHPASS="${SRC_PASSWORD}" sshpass -e scp \
            -P "${SRC_SSH_PORT}" \
            "${SSH_COMMON_OPTS[@]}" \
            "${local_path}" \
            "${SRC_USER}@${SRC_HOST}:${remote_path}"
    else
        scp \
            -P "${SRC_SSH_PORT}" \
            "${SSH_COMMON_OPTS[@]}" \
            "${local_path}" \
            "${SRC_USER}@${SRC_HOST}:${remote_path}"
    fi
}

rsync_to_dst() {
    local source_path=$1
    local target_path=$2
    shift 2

    local delete_arg=()
    if bool_true "${SYNC_DELETE}"; then
        delete_arg=(--delete)
    fi

    local ssh_command="ssh -p ${DST_SSH_PORT} -o StrictHostKeyChecking=accept-new -o ServerAliveInterval=30 -o ServerAliveCountMax=10"
    local rsync_path
    rsync_path="$(effective_rsync_path)"

    if bool_true "${DRY_RUN}"; then
        log_info "DRY-RUN rsync ${source_path} -> ${DST_USER}@${DST_HOST}:${target_path}"
        return 0
    fi

    ensure_target_askpass

    if has_password "${DST_PASSWORD}"; then
        SSHPASS="${DST_PASSWORD}" sshpass -e rsync \
            -aHAX --numeric-ids "${delete_arg[@]}" --info=progress2 \
            --rsync-path="${rsync_path}" \
            -e "${ssh_command}" \
            "$@" \
            "${source_path}" \
            "${DST_USER}@${DST_HOST}:${target_path}"
    else
        rsync \
            -aHAX --numeric-ids "${delete_arg[@]}" --info=progress2 \
            --rsync-path="${rsync_path}" \
            -e "${ssh_command}" \
            "$@" \
            "${source_path}" \
            "${DST_USER}@${DST_HOST}:${target_path}"
    fi
}

confirm_risky_action() {
    local action=$1
    if bool_true "${YES}"; then
        return 0
    fi

    printf '%b[CONFIRM]%b About to run %s. This may stop production services on %s.\n' \
        "${YELLOW}" "${NC}" "${action}" "${SRC_HOST}"
    printf 'Type exactly "yes" to continue: '
    local reply
    read -r reply
    [[ "${reply}" == "yes" ]] || die "Cancelled."
}

install_package_if_possible() {
    local package_name=$1
    local binary_name=${2:-${package_name}}

    if command -v "${binary_name}" >/dev/null 2>&1; then
        return 0
    fi

    if ! bool_true "${INSTALL_DEPS}"; then
        die "Missing ${binary_name}. Set INSTALL_DEPS=true or rerun with --install-deps."
    fi

    if [[ "$(id -u)" -ne 0 ]]; then
        die "Missing ${binary_name}, and current user is not root so packages cannot be installed."
    fi

    log_info "Installing local package ${package_name} for ${binary_name}"
    if command -v apt-get >/dev/null 2>&1; then
        apt-get update
        apt-get install -y "${package_name}"
    elif command -v dnf >/dev/null 2>&1; then
        dnf install -y "${package_name}"
    elif command -v yum >/dev/null 2>&1; then
        yum install -y "${package_name}"
    else
        die "No supported package manager found to install ${package_name}"
    fi
}

install_local_dependencies() {
    install_package_if_possible rsync rsync
    install_package_if_possible tar tar
    install_package_if_possible gzip gzip
    install_package_if_possible curl curl

    if has_password "${DST_PASSWORD}" || has_password "${SRC_PASSWORD}"; then
        install_package_if_possible sshpass sshpass
    fi

    install_docker_with_compose
}

has_local_compose() {
    docker compose version >/dev/null 2>&1 || command -v docker-compose >/dev/null 2>&1
}

compose_exec() {
    if docker compose version >/dev/null 2>&1; then
        docker compose "$@"
    elif command -v docker-compose >/dev/null 2>&1; then
        docker-compose "$@"
    else
        die "Docker Compose is unavailable. Install docker-compose-plugin, docker-compose-v2, or docker-compose."
    fi
}

install_docker_with_compose() {
    if [[ "$(id -u)" -ne 0 ]]; then
        if ! command -v docker >/dev/null 2>&1 || ! has_local_compose; then
            die "Docker or Docker Compose is missing and current user is not root."
        fi
        return 0
    fi

    if ! command -v docker >/dev/null 2>&1; then
        if ! bool_true "${INSTALL_DEPS}"; then
            die "Docker is missing locally. Install Docker or rerun with --install-deps."
        fi
        log_info "Installing local Docker package"
        if command -v apt-get >/dev/null 2>&1; then
            apt-get update
            apt-get install -y docker.io
        elif command -v dnf >/dev/null 2>&1; then
            dnf install -y docker
        elif command -v yum >/dev/null 2>&1; then
            yum install -y docker
        else
            die "No supported package manager found to install Docker"
        fi
    fi

    systemctl enable --now docker >/dev/null 2>&1 || true

    if has_local_compose; then
        return 0
    fi
    if ! bool_true "${INSTALL_DEPS}"; then
        die "Docker Compose is missing locally. Install it or rerun with --install-deps."
    fi

    log_info "Installing local Docker Compose"
    if command -v apt-get >/dev/null 2>&1; then
        apt-get update
        local package_name
        for package_name in docker-compose-plugin docker-compose-v2 docker-compose; do
            if apt-cache show "${package_name}" >/dev/null 2>&1 && apt-get install -y "${package_name}"; then
                break
            fi
        done
    elif command -v dnf >/dev/null 2>&1; then
        dnf install -y docker-compose-plugin || dnf install -y docker-compose
    elif command -v yum >/dev/null 2>&1; then
        yum install -y docker-compose-plugin || yum install -y docker-compose
    else
        die "No supported package manager found to install Docker Compose"
    fi

    has_local_compose || die "Docker is installed but Docker Compose could not be installed. Install docker-compose-plugin, docker-compose-v2, or docker-compose and rerun."
}

target_dependency_script() {
    cat <<'REMOTE'
set -euo pipefail

install_pkg() {
    pkg=$1
    bin=${2:-$pkg}
    if command -v "$bin" >/dev/null 2>&1; then
        return 0
    fi
    if [ "${INSTALL_DEPS_REMOTE:-true}" != "true" ]; then
        echo "Missing $bin on target and dependency installation is disabled." >&2
        exit 1
    fi
    if command -v apt-get >/dev/null 2>&1; then
        apt-get update
        apt-get install -y "$pkg"
    elif command -v dnf >/dev/null 2>&1; then
        dnf install -y "$pkg"
    elif command -v yum >/dev/null 2>&1; then
        yum install -y "$pkg"
    else
        echo "No supported package manager found for $pkg" >&2
        exit 1
    fi
}

install_pkg rsync rsync
install_pkg tar tar
install_pkg gzip gzip
install_pkg curl curl

has_compose() {
    docker compose version >/dev/null 2>&1 || command -v docker-compose >/dev/null 2>&1
}

install_optional_compose_pkg() {
    pkg=$1
    if command -v apt-get >/dev/null 2>&1; then
        if apt-cache show "$pkg" >/dev/null 2>&1; then
            apt-get install -y "$pkg" || true
        fi
    elif command -v dnf >/dev/null 2>&1; then
        dnf install -y "$pkg" || true
    elif command -v yum >/dev/null 2>&1; then
        yum install -y "$pkg" || true
    fi
}

install_docker_with_compose() {
    if ! command -v docker >/dev/null 2>&1; then
        if [ "${INSTALL_DEPS_REMOTE:-true}" != "true" ]; then
            echo "Docker is missing on target." >&2
            exit 1
        fi
        if command -v apt-get >/dev/null 2>&1; then
            apt-get update
            apt-get install -y docker.io
        elif command -v dnf >/dev/null 2>&1; then
            dnf install -y docker
        elif command -v yum >/dev/null 2>&1; then
            yum install -y docker
        else
            echo "No supported package manager found for Docker." >&2
            exit 1
        fi
    fi

    systemctl enable --now docker >/dev/null 2>&1 || true

    if has_compose; then
        return 0
    fi
    if [ "${INSTALL_DEPS_REMOTE:-true}" != "true" ]; then
        echo "Docker Compose is missing on target." >&2
        exit 1
    fi

    # Ubuntu 22.04 and 26.04 do not expose the same Compose package names.
    if command -v apt-get >/dev/null 2>&1; then
        apt-get update
    fi
    install_optional_compose_pkg docker-compose-plugin
    has_compose || install_optional_compose_pkg docker-compose-v2
    has_compose || install_optional_compose_pkg docker-compose

    if ! has_compose; then
        echo "Docker is installed but Docker Compose is unavailable. Install docker-compose-plugin, docker-compose-v2, or docker-compose on the target and rerun." >&2
        exit 1
    fi
}

install_docker_with_compose

if [ "${MIGRATE_NGINX_REMOTE:-true}" = "true" ] && ! command -v nginx >/dev/null 2>&1; then
    install_pkg nginx nginx
fi
REMOTE
}

install_target_dependencies() {
    log_step "Checking target dependencies"
    local install_value="false"
    if bool_true "${INSTALL_DEPS}"; then
        install_value="true"
    fi
    local nginx_value="false"
    if bool_true "${MIGRATE_NGINX}"; then
        nginx_value="true"
    fi

    local remote_deps_script="/tmp/sub2api-target-deps.sh"
    remote_dst "cat > $(shell_quote "${remote_deps_script}") && chmod 700 $(shell_quote "${remote_deps_script}")" < <(target_dependency_script)
    remote_dst_sudo "INSTALL_DEPS_REMOTE=${install_value} MIGRATE_NGINX_REMOTE=${nginx_value} bash $(shell_quote "${remote_deps_script}")"
}

print_config() {
    cat <<CONFIG
SRC_HOST=${SRC_HOST}
DST_HOST=${DST_HOST}
SRC_USER=${SRC_USER}
DST_USER=${DST_USER}
SRC_SSH_PORT=${SRC_SSH_PORT}
DST_SSH_PORT=${DST_SSH_PORT}
APP_DIR=${APP_DIR}
COMPOSE_FILE=${COMPOSE_FILE:-<auto>}
SERVER_PORT=${SERVER_PORT}
ASSUMED_PASSWORD=$(redacted "${ASSUMED_PASSWORD}")
SRC_PASSWORD=$(redacted "${SRC_PASSWORD}")
DST_PASSWORD=$(redacted "${DST_PASSWORD}")
MIGRATION_ID=${MIGRATION_ID}
MIGRATION_DIR=${MIGRATION_DIR}
NGINX_PATHS=${NGINX_PATHS}
EXTRA_RSYNC_PATHS=${EXTRA_RSYNC_PATHS:-<none>}
MIGRATE_NGINX=${MIGRATE_NGINX}
SYNC_DELETE=${SYNC_DELETE}
LOAD_IMAGES=${LOAD_IMAGES}
INSTALL_DEPS=${INSTALL_DEPS}
MIGRATE_NAMED_VOLUMES=${MIGRATE_NAMED_VOLUMES}
START_TARGET=${START_TARGET}
TARGET_SUDO=${TARGET_SUDO}
REMOTE_ASKPASS=${REMOTE_ASKPASS}
RSYNC_PATH=${RSYNC_PATH}
EFFECTIVE_RSYNC_PATH=$(effective_rsync_path)
YES=${YES}
DRY_RUN=${DRY_RUN}
CONFIG
}

detect_compose_file() {
    if [[ -n "${COMPOSE_FILE}" ]]; then
        [[ -f "${APP_DIR}/${COMPOSE_FILE}" ]] || die "COMPOSE_FILE is set but not found: ${APP_DIR}/${COMPOSE_FILE}"
        printf '%s\n' "${COMPOSE_FILE}"
        return 0
    fi

    local candidate
    for candidate in \
        docker-compose.local.yml \
        docker-compose.yml \
        compose.yml \
        compose.yaml \
        deploy/docker-compose.local.yml \
        deploy/docker-compose.yml; do
        if [[ -f "${APP_DIR}/${candidate}" ]]; then
            printf '%s\n' "${candidate}"
            return 0
        fi
    done

    die "Could not find Docker Compose file under ${APP_DIR}"
}

compose_cmd() {
    local compose_file=$1
    shift
    compose_exec -f "${APP_DIR}/${compose_file}" "$@"
}

target_compose_shell() {
    cat <<'SHELL'
if docker compose version >/dev/null 2>&1; then
    compose() { docker compose "$@"; }
elif command -v docker-compose >/dev/null 2>&1; then
    compose() { docker-compose "$@"; }
else
    echo "Docker Compose is unavailable on target." >&2
    exit 1
fi
SHELL
}

target_compose_command() {
    local command_string=$1
    target_compose_shell
    printf '%s\n' "${command_string}"
}

load_source_env_if_present() {
    if [[ -f "${APP_DIR}/.env" ]]; then
        set -a
        # shellcheck disable=SC1091
        . "${APP_DIR}/.env"
        set +a
    fi
}

compose_project_name() {
    load_source_env_if_present
    local project="${COMPOSE_PROJECT_NAME:-$(basename "${APP_DIR}")}"
    project="$(printf '%s' "${project}" | tr '[:upper:]' '[:lower:]' | tr -c 'a-z0-9_-' '_')"
    project="${project%_}"
    printf '%s\n' "${project}"
}

discover_named_volumes() {
    local project=$1
    local volume_names=""

    volume_names="$(docker volume ls -q --filter "label=com.docker.compose.project=${project}" 2>/dev/null || true)"
    if [[ -z "${volume_names}" ]]; then
        volume_names="$(docker volume ls -q | grep -E "(^${project}_)|(^sub2api_)" | grep -E 'data|postgres|redis' || true)"
    fi

    printf '%s\n' "${volume_names}" | sed '/^$/d' | sort -u
}

uses_named_volumes() {
    local compose_file=$1
    local project
    project="$(compose_project_name)"

    local volumes
    volumes="$(discover_named_volumes "${project}")"

    if [[ -n "${volumes}" ]]; then
        return 0
    fi

    if compose_cmd "${compose_file}" config --volumes 2>/dev/null | grep -q .; then
        return 0
    fi

    return 1
}

ensure_local_source_ready() {
    log_step "Checking source host filesystem and Docker Compose"
    [[ -d "${APP_DIR}" ]] || die "APP_DIR does not exist on this host: ${APP_DIR}. Run this script on the source server or use remote-run."

    local compose_file
    compose_file="$(detect_compose_file)"
    log_info "Detected compose file: ${APP_DIR}/${compose_file}"

    require_command docker
    docker version >/dev/null
    has_local_compose || die "Docker Compose is unavailable on the source server."

    compose_cmd "${compose_file}" ps || true

    if bool_true "${MIGRATE_NGINX}"; then
        if command -v nginx >/dev/null 2>&1; then
            nginx -t
        else
            log_warn "nginx command is not available on source; Nginx path sync can still run if files exist."
        fi
    fi
}

preflight() {
    log_step "Effective migration configuration"
    print_config

    log_step "Installing/checking local dependencies"
    install_local_dependencies

    ensure_local_source_ready

    log_step "Checking target SSH and dependencies"
    remote_dst "echo target-ok && uname -a"
    remote_dst_sudo "mkdir -p $(shell_quote "${APP_DIR}") $(shell_quote "${MIGRATION_DIR}")"
    install_target_dependencies

    log_step "Checking target ports"
    remote_dst_sudo "ss -lntup 2>/dev/null | grep -E ':80|:443|:${SERVER_PORT}|:5432|:6379' || true"

    log_step "Checking target disk"
    local app_size
    app_size="$(du -sh "${APP_DIR}" 2>/dev/null | awk '{print $1}' || true)"
    log_info "Source APP_DIR size: ${app_size:-unknown}"
    remote_dst "df -hT $(shell_quote "$(dirname "${APP_DIR}")") || df -hT"

    log_success "Preflight completed."
}

sync_single_path_to_target() {
    local path=$1
    [[ -e "${path}" ]] || {
        log_warn "Skip missing path: ${path}"
        return 0
    }

    if [[ -d "${path}" ]]; then
        remote_dst_sudo "mkdir -p $(shell_quote "${path}")"
        log_info "Sync directory ${path}/ -> ${DST_HOST}:${path}/"
        rsync_to_dst "${path}/" "${path}/"
    else
        remote_dst_sudo "mkdir -p $(shell_quote "$(dirname "${path}")")"
        log_info "Sync file ${path} -> ${DST_HOST}:${path}"
        rsync_to_dst "${path}" "${path}"
    fi
}

sync_nginx_paths() {
    if ! bool_true "${MIGRATE_NGINX}"; then
        log_info "Nginx migration disabled."
        return 0
    fi

    log_step "Syncing Nginx configuration and related paths"
    local path
    for path in ${NGINX_PATHS} ${EXTRA_RSYNC_PATHS}; do
        sync_single_path_to_target "${path}"
    done

    log_step "Testing target Nginx configuration"
    remote_dst_sudo "if command -v nginx >/dev/null 2>&1; then nginx -t; else echo 'nginx not installed on target'; fi"
}

sync_app_dir_to_target() {
    log_step "Syncing Sub2API directory ${APP_DIR}"
    remote_dst_sudo "mkdir -p $(shell_quote "${APP_DIR}") $(shell_quote "${MIGRATION_DIR}")"
    rsync_to_dst "${APP_DIR}/" "${APP_DIR}/"
}

export_and_load_images() {
    if ! bool_true "${LOAD_IMAGES}"; then
        log_info "Docker image export/load disabled."
        return 0
    fi

    local compose_file=$1
    local image_tar="${MIGRATION_DIR}/docker-images.tar"

    log_step "Exporting source Docker images"
    mkdir -p "${MIGRATION_DIR}"

    # Docker 28 rejects a bare 64-character image ID as a docker save argument.
    # Compose config returns stable image references such as postgres:18-alpine.
    local image_refs
    image_refs="$(compose_cmd "${compose_file}" config --images | awk 'NF && !seen[$0]++')"
    [[ -n "${image_refs}" ]] || die "No Docker image references found from compose file ${compose_file}"

    local -a image_ref_args=()
    mapfile -t image_ref_args <<<"${image_refs}"
    run_or_print docker save "${image_ref_args[@]}" -o "${image_tar}"
    [[ -f "${image_tar}" || "${DRY_RUN}" == "true" ]] || die "Image archive was not created: ${image_tar}"

    if [[ -f "${image_tar}" ]]; then
        ls -lh "${image_tar}"
    fi

    log_step "Syncing Docker image archive to target"
    remote_dst_sudo "mkdir -p $(shell_quote "$(dirname "${image_tar}")")"
    rsync_to_dst "${image_tar}" "${image_tar}"

    log_step "Loading Docker images on target"
    remote_dst_sudo "docker load -i $(shell_quote "${image_tar}")"
}

export_named_volumes_if_needed() {
    local compose_file=$1
    local requested="${MIGRATE_NAMED_VOLUMES}"

    if [[ "${requested}" == "false" ]]; then
        log_info "Named volume migration disabled."
        return 0
    fi

    local need_named=false
    if [[ "${requested}" == "true" ]]; then
        need_named=true
    elif uses_named_volumes "${compose_file}"; then
        need_named=true
    fi

    if [[ "${need_named}" != "true" ]]; then
        log_info "No named Docker volumes detected; assuming bind-mounted local directories are covered by APP_DIR rsync."
        return 0
    fi

    local project
    project="$(compose_project_name)"

    local volume_dir="${MIGRATION_DIR}/volumes"
    mkdir -p "${volume_dir}"

    local volumes
    volumes="$(discover_named_volumes "${project}")"
    if [[ -z "${volumes}" ]]; then
        log_warn "Compose declares volumes but actual Docker volume names were not found. Skipping volume archive."
        return 0
    fi

    printf '%s\n' "${volumes}" > "${volume_dir}/volume-list.txt"
    log_step "Exporting Docker named volumes"
    printf '%s\n' "${volumes}"

    local volume
    while IFS= read -r volume; do
        [[ -n "${volume}" ]] || continue
        log_info "Export volume ${volume}"
        run_or_print docker run --rm \
            -v "${volume}:/volume:ro" \
            -v "${volume_dir}:/backup" \
            alpine:latest \
            sh -c "cd /volume && tar czf /backup/${volume}.tgz ."
    done <<<"${volumes}"

    log_step "Syncing Docker named volume archives"
    rsync_to_dst "${volume_dir}/" "${volume_dir}/"

    log_step "Importing Docker named volumes on target"
    local volume_dir_q
    volume_dir_q="$(shell_quote "${volume_dir}")"
    remote_dst_sudo "cd ${volume_dir_q} && while IFS= read -r v; do [ -n \"\$v\" ] || continue; docker volume create \"\$v\" >/dev/null; docker run --rm -v \"\$v:/volume\" -v ${volume_dir_q}:/backup alpine:latest sh -c 'name=\"\$1\"; cd /volume && tar xzf \"/backup/\${name}.tgz\"' sh \"\$v\"; done < volume-list.txt"
}

stop_source_compose() {
    local compose_file=$1
    log_step "Stopping source Docker Compose stack"
    compose_cmd "${compose_file}" ps || true
    run_or_print compose_cmd "${compose_file}" down
}

start_target_compose() {
    if ! bool_true "${START_TARGET}"; then
        log_info "Target start disabled."
        return 0
    fi

    local compose_file=$1
    log_step "Starting target Docker Compose stack"
    remote_dst_sudo "$(target_compose_command "cd $(shell_quote "${APP_DIR}") && compose -f $(shell_quote "${compose_file}") up -d && compose -f $(shell_quote "${compose_file}") ps")"
}

restart_target_nginx() {
    if ! bool_true "${MIGRATE_NGINX}"; then
        return 0
    fi

    log_step "Restarting target Nginx"
    remote_dst_sudo "if command -v nginx >/dev/null 2>&1; then nginx -t && systemctl enable nginx >/dev/null 2>&1 || true; nginx -t && systemctl restart nginx && systemctl status nginx --no-pager --lines=20; else echo 'nginx not installed; skipped restart'; fi"
}

verify_target() {
    local compose_file
    compose_file="$(detect_compose_file)"

    log_step "Verifying target Docker Compose"
    remote_dst_sudo "$(target_compose_command "cd $(shell_quote "${APP_DIR}") && compose -f $(shell_quote "${compose_file}") ps")"

    log_step "Checking target Sub2API logs"
    remote_dst_sudo "$(target_compose_command "cd $(shell_quote "${APP_DIR}") && (compose -f $(shell_quote "${compose_file}") logs --tail=120 sub2api || compose -f $(shell_quote "${compose_file}") logs --tail=120)")"

    log_step "Checking target HTTP endpoint"
    remote_dst "curl -fsS http://127.0.0.1:${SERVER_PORT}/health || curl -I http://127.0.0.1:${SERVER_PORT} || true"

    if bool_true "${MIGRATE_NGINX}"; then
        log_step "Checking target Nginx"
        remote_dst_sudo "if command -v nginx >/dev/null 2>&1; then nginx -t && systemctl is-active nginx; else echo 'nginx not installed'; fi"
    fi

    log_success "Target verification finished."
}

pre_sync() {
    log_step "Pre-sync started"
    install_local_dependencies
    local compose_file
    compose_file="$(detect_compose_file)"

    remote_dst_sudo "mkdir -p $(shell_quote "${APP_DIR}") $(shell_quote "${MIGRATION_DIR}")"
    install_target_dependencies

    sync_app_dir_to_target
    sync_nginx_paths
    export_and_load_images "${compose_file}"

    log_success "Pre-sync completed. You can now enter the maintenance window and run cutover."
}

cutover() {
    confirm_risky_action "cutover"

    log_step "Cutover started"
    install_local_dependencies

    local compose_file
    compose_file="$(detect_compose_file)"

    export_and_load_images "${compose_file}"
    stop_source_compose "${compose_file}"
    export_named_volumes_if_needed "${compose_file}"
    sync_app_dir_to_target
    sync_nginx_paths
    start_target_compose "${compose_file}"
    restart_target_nginx
    verify_target

    log_success "Cutover completed. Keep source server ${SRC_HOST} intact for rollback."
}

rollback() {
    confirm_risky_action "rollback"

    local compose_file
    compose_file="$(detect_compose_file)"

    log_step "Stopping target Compose stack"
    remote_dst_sudo "$(target_compose_command "cd $(shell_quote "${APP_DIR}") && compose -f $(shell_quote "${compose_file}") down || true")"

    log_step "Starting source Compose stack"
    run_or_print compose_cmd "${compose_file}" up -d
    compose_cmd "${compose_file}" ps || true

    if bool_true "${MIGRATE_NGINX}" && command -v nginx >/dev/null 2>&1; then
        log_step "Testing source Nginx"
        nginx -t || true
    fi

    log_success "Rollback action completed. Switch DNS/CDN/client traffic back to ${SRC_HOST} if needed."
}

remote_run() {
    local source_action="${SOURCE_ACTION:-preflight}"
    local remote_script="/tmp/migrate-sub2api-docker-nginx.sh"

    log_step "Copying script to source server ${SRC_HOST}"
    require_command scp
    if has_password "${SRC_PASSWORD}"; then
        install_package_if_possible sshpass sshpass
    fi

    scp_to_src "$0" "${remote_script}"

    log_step "Executing ${source_action} on source server ${SRC_HOST}"
    local remote_command
    remote_command=$(
        cat <<EOF
chmod +x $(shell_quote "${remote_script}") &&
SRC_HOST=$(shell_quote "${SRC_HOST}") \
DST_HOST=$(shell_quote "${DST_HOST}") \
SRC_USER=$(shell_quote "${SRC_USER}") \
DST_USER=$(shell_quote "${DST_USER}") \
SRC_SSH_PORT=$(shell_quote "${SRC_SSH_PORT}") \
DST_SSH_PORT=$(shell_quote "${DST_SSH_PORT}") \
APP_DIR=$(shell_quote "${APP_DIR}") \
COMPOSE_FILE=$(shell_quote "${COMPOSE_FILE}") \
SERVER_PORT=$(shell_quote "${SERVER_PORT}") \
ASSUMED_PASSWORD=$(shell_quote "${ASSUMED_PASSWORD}") \
DST_PASSWORD=$(shell_quote "${DST_PASSWORD}") \
MIGRATION_ID=$(shell_quote "${MIGRATION_ID}") \
MIGRATION_DIR=$(shell_quote "${MIGRATION_DIR}") \
NGINX_PATHS=$(shell_quote "${NGINX_PATHS}") \
EXTRA_RSYNC_PATHS=$(shell_quote "${EXTRA_RSYNC_PATHS}") \
MIGRATE_NGINX=$(shell_quote "${MIGRATE_NGINX}") \
SYNC_DELETE=$(shell_quote "${SYNC_DELETE}") \
LOAD_IMAGES=$(shell_quote "${LOAD_IMAGES}") \
INSTALL_DEPS=$(shell_quote "${INSTALL_DEPS}") \
MIGRATE_NAMED_VOLUMES=$(shell_quote "${MIGRATE_NAMED_VOLUMES}") \
START_TARGET=$(shell_quote "${START_TARGET}") \
YES=$(shell_quote "${YES}") \
DRY_RUN=$(shell_quote "${DRY_RUN}") \
bash $(shell_quote "${remote_script}") $(shell_quote "${source_action}")
EOF
    )

    remote_src "${remote_command}"
}

parse_args() {
    ACTION=""
    while [[ $# -gt 0 ]]; do
        case "$1" in
            -h|--help|help)
                ACTION="help"
                shift
                ;;
            -y|--yes)
                YES=true
                shift
                ;;
            --dry-run)
                DRY_RUN=true
                shift
                ;;
            --no-nginx)
                MIGRATE_NGINX=false
                shift
                ;;
            --no-delete)
                SYNC_DELETE=false
                shift
                ;;
            --no-load-images)
                LOAD_IMAGES=false
                shift
                ;;
            --no-start-target)
                START_TARGET=false
                shift
                ;;
            --install-deps)
                INSTALL_DEPS=true
                shift
                ;;
            --skip-install-deps)
                INSTALL_DEPS=false
                shift
                ;;
            print-config|preflight|pre-sync|cutover|all|verify|rollback|remote-run)
                if [[ -n "${ACTION}" && "${ACTION}" != "help" ]]; then
                    die "Only one action can be specified."
                fi
                ACTION="$1"
                shift
                ;;
            *)
                die "Unknown argument or action: $1. Run with --help."
                ;;
        esac
    done

    if [[ -z "${ACTION}" ]]; then
        ACTION="help"
    fi
}

main() {
    parse_args "$@"

    case "${ACTION}" in
        help)
            usage
            ;;
        print-config)
            print_config
            ;;
        preflight)
            preflight
            ;;
        pre-sync)
            pre_sync
            ;;
        cutover)
            cutover
            ;;
        all)
            pre_sync
            cutover
            ;;
        verify)
            verify_target
            ;;
        rollback)
            rollback
            ;;
        remote-run)
            remote_run
            ;;
        *)
            die "Unhandled action: ${ACTION}"
            ;;
    esac
}

main "$@"
