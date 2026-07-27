#!/bin/bash
# metacubexd-ctl.sh — Manage metacubexd-server as a systemd or OpenRC service.
#
# Usage:
#   metacubexd-ctl.sh <command> [options]
#
# Commands:
#   install   Install the service (binary + service files + config + start)
#   uninstall Remove the service (stops, removes files; keeps data & user)
#   update    Replace the binary and restart the service
#   config    Show or modify service configuration
#
# Install options:
#   --bin <path>          Use a local pre-built binary instead of downloading
#   --version <vX.Y.Z>   GitHub release version to download (default: latest)
#   --token <string>      Set CONTROL_TOKEN (default: random)
#   --secret <string>     Set CLASH_SECRET (default: random)
#
# Uninstall options:
#   --keep-data   Keep /var/lib/metacubexd (default: keep)
#   --keep-user   Keep the metacubexd system user (default: keep)
#
# Update options:
#   --bin <path>          Use a local pre-built binary
#   --version <vX.Y.Z>   GitHub release version to download (default: latest)
#   --force               Skip version check, always download and update
#
# Config options:
#   show                  Print the current configuration file
#   set <KEY>=<VALUE>     Set a configuration value and restart the service
#   get <KEY>             Print a single configuration value
#
# Examples:
#   metacubexd-ctl.sh install                                  # download latest
#   metacubexd-ctl.sh install --bin ./metacubexd-server-go     # local binary
#   metacubexd-ctl.sh install --version v1.2.3                 # pin version
#   metacubexd-ctl.sh update                                  # upgrade to latest
#   metacubexd-ctl.sh update --version v1.3.0                  # pin version
#   metacubexd-ctl.sh update --force                           # force re-download
#   metacubexd-ctl.sh config show                              # view config
#   metacubexd-ctl.sh config set MIXED_PORT=8080               # change port
#   metacubexd-ctl.sh config get CONTROL_TOKEN                 # view token
#   metacubexd-ctl.sh uninstall                                # remove service
#
# Requires: root, curl, tar, and either systemd or OpenRC.

set -euo pipefail

# ── Constants ─────────────────────────────────────────────────────────────

OWNER="Sion10032"
REPO="metacubexd-server-go"
SERVICE_BIN="/usr/local/bin/metacubexd-server"
SERVICE_NAME="metacubexd"
SYSTEM_USER="metacubexd"
CONF_DIR="/etc/metacubexd"
DATA_DIR="/var/lib/metacubexd"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# ── Colors ────────────────────────────────────────────────────────────────

if [ -t 1 ]; then
    RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[0;33m'; BOLD='\033[1m'; RESET='\033[0m'
else
    RED=''; GREEN=''; YELLOW=''; BOLD=''; RESET=''
fi

info()  { echo -e "${GREEN}[INFO]${RESET}  $*"; }
warn()  { echo -e "${YELLOW}[WARN]${RESET}  $*"; }
error() { echo -e "${RED}[ERROR]${RESET} $*" >&2; }
die()   { error "$@"; exit 1; }

# ── Root check ────────────────────────────────────────────────────────────

check_root() {
    if [ "$(id -u)" -ne 0 ]; then
        die "This script must be run as root. Try: sudo bash $0 $*"
    fi
}

# ── Detect init system ────────────────────────────────────────────────────

detect_init() {
    if [ -d /run/systemd/system ] || [ -f /run/systemd/system ]; then
        echo "systemd"
    elif command -v openrc-run >/dev/null 2>&1; then
        echo "openrc"
    else
        die "No supported init system detected (need systemd or OpenRC)."
    fi
}

INIT_SYSTEM=""
init_system() {
    if [ -z "${INIT_SYSTEM}" ]; then
        INIT_SYSTEM="$(detect_init)"
        info "Detected init system: ${BOLD}${INIT_SYSTEM}${RESET}"
    fi
}

# ── Config file path ──────────────────────────────────────────────────────

env_file() {
    if [ "${INIT_SYSTEM}" = "systemd" ]; then
        echo "${CONF_DIR}/${SERVICE_NAME}.env"
    else
        echo "/etc/conf.d/${SERVICE_NAME}"
    fi
}

# ── Get / Set env value in config file ────────────────────────────────────

# env_get KEY — prints the value of KEY from the config file (unquoted, empty if unset)
env_get() {
    local key="$1" target
    target="$(env_file)"
    [ -f "${target}" ] || { echo ""; return; }
    if [ "${INIT_SYSTEM}" = "systemd" ]; then
        # Lines like: KEY=value (no export prefix)
        sed -n "s/^${key}=//p" "${target}" | tail -1
    else
        # Lines like: export KEY="value" or export KEY=value
        grep "^export ${key}=" "${target}" | tail -1 | cut -d'=' -f2- | tr -d '"'
    fi
}

# env_set KEY=VALUE — writes KEY=VALUE into the config file, adding if missing
env_set() {
    local pair="$1" key="${1%%=*}" val="${1#*=}" target
    target="$(env_file)"
    [ -f "${target}" ] || die "Config file not found: ${target}"

    # Use grep -v + append instead of sed to avoid escaping hell.
    # Remove existing line(s) for this key, then append new value.
    if [ "${INIT_SYSTEM}" = "systemd" ]; then
        grep -v "^${key}=" "${target}" > "${target}.tmp"
        echo "${key}=${val}" >> "${target}.tmp"
        mv "${target}.tmp" "${target}"
    else
        grep -v "^export ${key}=" "${target}" > "${target}.tmp"
        echo "export ${key}=\"${val}\"" >> "${target}.tmp"
        mv "${target}.tmp" "${target}"
    fi
}

# ── Binary install ────────────────────────────────────────────────────────

install_binary_local() {
    local src="$1"
    [ -f "${src}" ] || die "Binary not found: ${src}"
    [ -x "${src}" ] || die "Binary is not executable: ${src}"
    install -m 0755 "${src}" "${SERVICE_BIN}"
    info "Installed binary from ${src} → ${SERVICE_BIN}"
}

install_binary_download() {
    local version="$1"
    local arch

    case "$(uname -m)" in
        x86_64|amd64)  arch="amd64" ;;
        aarch64|arm64) arch="arm64" ;;
        *) die "Unsupported architecture: $(uname -m)" ;;
    esac

    # Resolve "latest" to an actual tag via GitHub API.
    if [ "${version}" = "latest" ]; then
        version="$(get_latest_version)"
        if [ "${version}" = "unknown" ]; then
            die "Failed to determine latest version from GitHub. Try --version <tag>."
        fi
        info "Latest release: ${version}"
    fi

    # Strip leading v from version for the filename (goreleaser name_template
    # uses .Version which omits the v prefix).
    local version_no_v="${version#v}"
    local url="https://github.com/${OWNER}/${REPO}/releases/download/${version}/${REPO}_${version_no_v}_linux_${arch}.tar.gz"

    info "Downloading from ${url} ..."
    local tmpdir
    tmpdir="$(mktemp -d)"
    trap 'rm -rf "${tmpdir:-}"' RETURN

    if ! curl -fsSL -o "${tmpdir}/release.tar.gz" "${url}"; then
        die "Download failed. Check the version tag and your network."
    fi

    tar -xzf "${tmpdir}/release.tar.gz" -C "${tmpdir}"

    # The tarball contains the binary as metacubexd-server-go; rename.
    local extracted="${tmpdir}/metacubexd-server-go"
    if [ ! -f "${extracted}" ]; then
        extracted="$(find "${tmpdir}" -maxdepth 1 -type f -executable | head -1)"
        [ -n "${extracted}" ] || die "Could not find binary in downloaded tarball."
    fi

    install -m 0755 "${extracted}" "${SERVICE_BIN}"
    info "Installed binary → ${SERVICE_BIN}"

    rm -rf "${tmpdir}"
    trap - RETURN
}

# ── System user ───────────────────────────────────────────────────────────

create_user() {
    if id "${SYSTEM_USER}" >/dev/null 2>&1; then
        info "User '${SYSTEM_USER}' already exists."
        return
    fi

    if command -v useradd >/dev/null 2>&1; then
        useradd \
            --system \
            --no-create-home \
            --shell /usr/sbin/nologin \
            --home-dir "${DATA_DIR}" \
            "${SYSTEM_USER}" || true
    elif command -v adduser >/dev/null 2>&1; then
        adduser \
            -S \
            -H \
            -s /sbin/nologin \
            -h "${DATA_DIR}" \
            "${SYSTEM_USER}" || true
    else
        die "Neither useradd nor adduser found. Cannot create system user."
    fi

    info "Created system user '${SYSTEM_USER}'."
}

# ── Service file installation ─────────────────────────────────────────────

install_service_systemd() {
    mkdir -p "${CONF_DIR}"

    # Systemd unit (embedded)
    cat > /etc/systemd/system/${SERVICE_NAME}.service << 'SYSTEMD_UNIT'
[Unit]
Description=metacubexd-server (All-in-One dashboard + mihomo supervisor)
Documentation=https://github.com/Sion10032/metacubexd-server-go
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=metacubexd
Group=metacubexd
EnvironmentFile=-/etc/metacubexd/metacubexd.env
Environment=DATA_DIR=/var/lib/metacubexd
Environment=MIHOMO_BIN=/usr/local/bin/mihomo
ExecStart=/usr/local/bin/metacubexd-server
TimeoutStopSec=15
KillSignal=SIGTERM
Restart=on-failure
RestartSec=5
AmbientCapabilities=CAP_NET_ADMIN
CapabilityBoundingSet=CAP_NET_ADMIN
NoNewPrivileges=true
StateDirectory=metacubexd
ProtectSystem=strict
ProtectHome=true
PrivateTmp=true
DeviceAllow=/dev/net/tun rw

[Install]
WantedBy=multi-user.target
SYSTEMD_UNIT

    # Env file (embedded)
    if [ -f "${CONF_DIR}/${SERVICE_NAME}.env" ]; then
        info "Existing env file at ${CONF_DIR}/${SERVICE_NAME}.env — keeping."
    else
        cat > "${CONF_DIR}/${SERVICE_NAME}.env" << 'ENV_SAMPLE'
# metacubexd-server env — see README.md for full variable list.
# Only non-default values need to be set.
CONTROL_TOKEN=
CLASH_SECRET=
# CONTROL_PORT=8080
# MIXED_PORT=7890
# TZ=Asia/Shanghai
ENV_SAMPLE
        chmod 0600 "${CONF_DIR}/${SERVICE_NAME}.env"
        info "Created env file at ${CONF_DIR}/${SERVICE_NAME}.env"
    fi

    systemctl daemon-reload
    info "Installed systemd unit → /etc/systemd/system/${SERVICE_NAME}.service"
}

install_service_openrc() {
    # OpenRC init script (embedded)
    cat > /etc/init.d/${SERVICE_NAME} << 'OPENRC_INIT'
#!/sbin/openrc-run
description="metacubexd-server (All-in-One dashboard + mihomo supervisor)"

: "${MIHOMO_BIN:=/usr/local/bin/mihomo}"
: "${DATA_DIR:=/var/lib/metacubexd}"
: "${METACUBEXD_USER:=metacubexd:metacubexd}"

command="/usr/local/bin/metacubexd-server"
command_user="${METACUBEXD_USER}"
command_background=true
pidfile="/run/${RC_SVCNAME}.pid"
output_log="/var/log/${RC_SVCNAME}/${RC_SVCNAME}.log"
error_log="/var/log/${RC_SVCNAME}/${RC_SVCNAME}.log"
supervisor="supervise-daemon"
supervise_daemon_args="--pidfile ${pidfile} --stdout ${output_log} --stderr ${error_log}"

depend() {
    need net
    after firewall
}

start_pre() {
    checkpath -d -o "${METACUBEXD_USER}" -m 0755 /var/log/${RC_SVCNAME}
    local _user _group
    _user="${METACUBEXD_USER%%:*}"
    _group="${METACUBEXD_USER##*:}"
    [ "${_group}" = "${_user}" ] && _group="${_user}"
    checkpath -d -o "${_user}" -m 0755 "${DATA_DIR}"
    if [ -x "${MIHOMO_BIN}" ]; then
        setcap cap_net_admin+ep "${MIHOMO_BIN}" 2>/dev/null ||             ewarn "setcap failed on ${MIHOMO_BIN}; TUN mode may be unavailable"
    fi
}
OPENRC_INIT
    chmod 0755 /etc/init.d/${SERVICE_NAME}

    mkdir -p "${CONF_DIR}"
    if [ -f "/etc/conf.d/${SERVICE_NAME}" ]; then
        info "Existing conf.d at /etc/conf.d/${SERVICE_NAME} — keeping."
    else
        cat > "/etc/conf.d/${SERVICE_NAME}" << 'OPENRC_CONF'
# /etc/conf.d/metacubexd — OpenRC environment variables
export METACUBEXD_USER="metacubexd:metacubexd"
export MIHOMO_BIN="/usr/local/bin/mihomo"
export DATA_DIR="/var/lib/metacubexd"
export CONTROL_TOKEN=""
export CLASH_SECRET=""
# export CONTROL_PORT=8080
# export MIXED_PORT=7890
# export TZ="Asia/Shanghai"
OPENRC_CONF
        info "Created conf.d at /etc/conf.d/${SERVICE_NAME}"
    fi

    info "Installed OpenRC script → /etc/init.d/${SERVICE_NAME}"
}

install_service_files() {
    if [ "${INIT_SYSTEM}" = "systemd" ]; then
        install_service_systemd
    else
        install_service_openrc
    fi
}

# ── Inject secrets ───────────────────────────────────────────────────────

inject_secrets() {
    local token="${1:-}"
    local secret="${2:-}"

    # CONTROL_TOKEN empty = no login page (open access).
    # CLASH_SECRET always auto-generates if not provided.
    [ -z "${secret}" ] && secret="$(openssl rand -hex 16)"

    local target
    target="$(env_file)"

    if [ -n "${token}" ]; then
        env_set "CONTROL_TOKEN=${token}"
    fi
    env_set "CLASH_SECRET=${secret}"

    info "Secrets written to ${target}"
    echo ""
    if [ -n "${token}" ]; then
        echo -e "  ${BOLD}CONTROL_TOKEN${RESET} = ${token}"
    else
        echo -e "  ${BOLD}CONTROL_TOKEN${RESET} = (empty — no login page)"
    fi
    echo -e "  ${BOLD}CLASH_SECRET${RESET}  = ${secret}"
    echo ""
    if [ -n "${token}" ]; then
        warn "Save these values — you will need them to log in."
    else
        warn "CONTROL_TOKEN empty — no login page, all access is open."
    fi
}

# ── Check mihomo ──────────────────────────────────────────────────────────

check_mihomo() {
    local detected
    detected="$(command -v mihomo 2>/dev/null)"
    if [ -n "${detected}" ]; then
        info "mihomo found at ${detected}"
        # Write detected path into env file so the service actually uses it.
        env_set "MIHOMO_BIN=${detected}"
    else
        warn "mihomo not found in PATH."
        warn "The server will start but the kernel will not be available."
        warn "Install mihomo to /usr/local/bin/mihomo, or:"
        warn "  bash $(basename "$0") config set MIHOMO_BIN=/path/to/mihomo"
    fi
}

# ── Enable + start ────────────────────────────────────────────────────────

service_enable_start() {
    if [ "${INIT_SYSTEM}" = "systemd" ]; then
        systemctl enable "${SERVICE_NAME}" 2>/dev/null
        systemctl start "${SERVICE_NAME}" 2>/dev/null
    else
        rc-update add "${SERVICE_NAME}" default 2>/dev/null
        rc-service "${SERVICE_NAME}" start 2>/dev/null
    fi
}

service_restart() {
    if [ "${INIT_SYSTEM}" = "systemd" ]; then
        systemctl restart "${SERVICE_NAME}" 2>/dev/null
    else
        rc-service "${SERVICE_NAME}" restart 2>/dev/null
    fi
}

service_stop() {
    if [ "${INIT_SYSTEM}" = "systemd" ]; then
        systemctl stop "${SERVICE_NAME}" 2>/dev/null || true
    else
        rc-service "${SERVICE_NAME}" stop 2>/dev/null || true
    fi
}

service_status() {
    if [ "${INIT_SYSTEM}" = "systemd" ]; then
        systemctl status "${SERVICE_NAME}" 2>/dev/null || true
    else
        rc-service "${SERVICE_NAME}" status 2>/dev/null || true
    fi
}

# ═══════════════════════════════════════════════════════════════════════════
#  COMMANDS
# ═══════════════════════════════════════════════════════════════════════════

cmd_install() {
    local opt_bin="" opt_version="latest" opt_token="" opt_secret=""

    while [ $# -gt 0 ]; do
        case "$1" in
            --bin)      opt_bin="$2"; shift 2 ;;
            --version)  opt_version="$2"; shift 2 ;;
            --token)    opt_token="$2"; shift 2 ;;
            --secret)   opt_secret="$2"; shift 2 ;;
            *) die "install: unknown option: $1" ;;
        esac
    done

    check_root
    init_system
    create_user

    # Binary
    if [ -n "${opt_bin}" ]; then
        install_binary_local "${opt_bin}"
    else
        install_binary_download "${opt_version}"
    fi

    # Service files
    install_service_files

    # Secrets (only if config file is new/empty)
    local target token_val secret_val
    target="$(env_file)"
    token_val="$(env_get CONTROL_TOKEN)"
    secret_val="$(env_get CLASH_SECRET)"
    if [ -z "${token_val}" ] && [ -z "${secret_val}" ]; then
        # If running interactively (stdin is a TTY), prompt for secrets.
        if [ -t 0 ] && [ -z "${opt_token}" ] && [ -z "${opt_secret}" ]; then
            echo ""
            echo -e "  Please input CONTROL_TOKEN, leave empty = no login (fully open)"
            read -r -p "  CONTROL_TOKEN = " opt_token
            echo ""
            echo -e "  Please input CLASH_SECRET, leave empty = auto-generate"
            read -r -p "  CLASH_SECRET  = " opt_secret
            echo ""
        fi
        inject_secrets "${opt_token}" "${opt_secret}"
    else
        info "Config file already has secrets — keeping existing values."
    fi

    # Mihomo check
    check_mihomo

    # Enable + start
    service_enable_start
    info "Service enabled and started."
    echo ""
    echo -e "  ${BOLD}Dashboard:${RESET} http://localhost:8080"
    echo ""
    if [ "${INIT_SYSTEM}" = "systemd" ]; then
        echo -e "  ${BOLD}systemctl status ${SERVICE_NAME}${RESET}     # check status"
        echo -e "  ${BOLD}journalctl -u ${SERVICE_NAME} -f${RESET}  # follow logs"
    else
        echo -e "  ${BOLD}rc-service ${SERVICE_NAME} status${RESET}  # check status"
        echo -e "  ${BOLD}tail -f /var/log/${SERVICE_NAME}/${SERVICE_NAME}.log${RESET}  # follow logs"
    fi
    echo ""
}

cmd_uninstall() {
    local keep_data=true keep_user=true

    while [ $# -gt 0 ]; do
        case "$1" in
            --keep-data) keep_data=true; shift ;;
            --keep-user) keep_user=true; shift ;;
            --remove-data) keep_data=false; shift ;;
            --remove-user) keep_user=false; shift ;;
            *) die "uninstall: unknown option: $1" ;;
        esac
    done

    check_root
    init_system
    info "Stopping and removing ${SERVICE_NAME} service..."

    service_stop

    if [ "${INIT_SYSTEM}" = "systemd" ]; then
        systemctl disable "${SERVICE_NAME}" 2>/dev/null || true
        rm -f /etc/systemd/system/${SERVICE_NAME}.service
        systemctl daemon-reload
    else
        rc-update del "${SERVICE_NAME}" 2>/dev/null || true
        rm -f /etc/init.d/${SERVICE_NAME}
    fi

    rm -f "${SERVICE_BIN}"
    rm -rf "${CONF_DIR}"

    info "Service removed."

    if ! ${keep_data}; then
        rm -rf "${DATA_DIR}"
        info "Data directory ${DATA_DIR} removed."
    else
        warn "Data directory ${DATA_DIR} was preserved."
    fi

    if ! ${keep_user}; then
        if command -v userdel >/dev/null 2>&1; then
            userdel "${SYSTEM_USER}" 2>/dev/null || true
        elif command -v deluser >/dev/null 2>&1; then
            deluser "${SYSTEM_USER}" 2>/dev/null || true
        fi
        info "System user '${SYSTEM_USER}' removed."
    else
        warn "System user '${SYSTEM_USER}' was preserved."
    fi

    echo ""
    info "Done."
}

# ── GitHub version detection ─────────────────────────────────────────────

# get_latest_version queries GitHub for the latest release tag.
get_latest_version() {
    local url="https://api.github.com/repos/${OWNER}/${REPO}/releases/latest"
    local tmpf resp_code http_code tag
    tmpf="$(mktemp)"
    resp_code=$(curl -sSL -w '%{http_code}' -o "${tmpf}" "${url}" 2>/dev/null)
    http_code="${resp_code: -3}"
    if [ "${http_code}" != "200" ]; then
        rm -f "${tmpf}"
        echo "unknown"
        return
    fi
    tag=$(grep -o '"tag_name": *"[^"]*"' "${tmpf}" | cut -d'"' -f4)
    rm -f "${tmpf}"
    if [ -n "${tag}" ]; then
        echo "${tag}"
    else
        echo "unknown"
    fi
}


# get_installed_version prints the installed binary version.
get_installed_version() {
    if [ -x "${SERVICE_BIN}" ]; then
        "${SERVICE_BIN}" --version 2>/dev/null || echo "unknown"
    else
        echo "not installed"
    fi
}

cmd_update() {
    local opt_bin="" opt_version="latest" opt_force=false

    while [ $# -gt 0 ]; do
        case "$1" in
            --bin)      opt_bin="$2"; shift 2 ;;
            --version)  opt_version="$2"; shift 2 ;;
            --force)    opt_force=true; shift ;;
            *) die "update: unknown option: $1" ;;
        esac
    done

    check_root
    init_system

    local current_version
    current_version="$(get_installed_version)"
    info "Current version: ${BOLD}${current_version}${RESET}"

    # Skip version check if user specified a local binary or explicit version
    if [ -z "${opt_bin}" ] && [ "${opt_version}" = "latest" ] && ! ${opt_force}; then
        local latest
        latest="$(get_latest_version)"
        info "Latest version:  ${BOLD}${latest}${RESET}"

        if [ "${latest}" = "unknown" ]; then
            warn "Could not determine latest version from GitHub. Proceeding with download."
        elif [ "${current_version}" = "${latest}" ]; then
            info "Already up to date."
            exit 0
        fi
    fi

    # Replace binary
    if [ -n "${opt_bin}" ]; then
        install_binary_local "${opt_bin}"
    else
        install_binary_download "${opt_version}"
    fi

    # Restart service
    info "Restarting ${SERVICE_NAME}..."
    service_restart
    info "Service restarted."

    local new_version
    new_version="$(get_installed_version)"
    info "New version: ${BOLD}${new_version}${RESET}"

    echo ""
    if [ "${INIT_SYSTEM}" = "systemd" ]; then
        echo -e "  ${BOLD}journalctl -u ${SERVICE_NAME} -f${RESET}  # follow logs"
    else
        echo -e "  ${BOLD}tail -f /var/log/${SERVICE_NAME}/${SERVICE_NAME}.log${RESET}  # follow logs"
    fi
    echo ""
}

cmd_config() {
    if [ $# -eq 0 ]; then
        die "config: missing subcommand (show, get, set)"
    fi

    local subcmd="$1"; shift

    case "${subcmd}" in
        show)
            init_system
            local target
            target="$(env_file)"
            if [ -f "${target}" ]; then
                info "Config: ${target}"
                echo ""
                cat "${target}"
                echo ""
            else
                die "Config file not found: ${target}"
            fi
            ;;
        get)
            [ $# -ge 1 ] || die "config get: missing KEY"
            init_system
            local val
            val="$(env_get "$1")"
            if [ -n "${val}" ]; then
                echo "${val}"
            else
                die "$1 is not set"
            fi
            ;;
        set)
            [ $# -ge 1 ] || die "config set: missing KEY=VALUE"
            check_root
            init_system
            local key="${1%%=*}" val="${1#*=}"
            [ "${key}" != "${1}" ] || die "config set: invalid format, use KEY=VALUE"

            env_set "${key}=${val}"
            info "${key} = ${val}"

            # Restart to apply
            info "Restarting ${SERVICE_NAME} to apply changes..."
            service_restart
            info "Service restarted."
            ;;
        *)
            die "config: unknown subcommand: ${subcmd} (use show, get, set)"
            ;;
    esac
}

# ═══════════════════════════════════════════════════════════════════════════
#  DISPATCH
# ═══════════════════════════════════════════════════════════════════════════

usage() {
    sed -n '2,/^$/p' "$0" | sed 's/^# //' | sed 's/^#//'
    exit 0
}

[ $# -ge 1 ] || usage

COMMAND="$1"; shift

case "${COMMAND}" in
    install)   cmd_install "$@" ;;
    uninstall) cmd_uninstall "$@" ;;
    update)    cmd_update "$@" ;;
    config)    cmd_config "$@" ;;
    -h|--help|help) usage ;;
    *) die "Unknown command: ${COMMAND}\nRun with --help for usage." ;;
esac
