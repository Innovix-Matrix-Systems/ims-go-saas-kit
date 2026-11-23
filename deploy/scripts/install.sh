#!/usr/bin/env bash
#
# install.sh — one-time provisioning for the IMS Go API on a fresh host.
#
# Creates the service user and directory layout, installs the systemd units and
# seeds the environment file. Run once as root, then use release.sh for deploys.
#
set -euo pipefail

BASE_DIR="${IMS_BASE_DIR:-/opt/ims/ims-go-api}"
STATE_DIR="${IMS_STATE_DIR:-/var/lib/ims/ims-go-api}"
LOG_DIR="${IMS_LOG_DIR:-/var/log/ims/ims-go-api}"
ENV_DIR="${IMS_ENV_DIR:-/etc/ims/ims-go-api}"
SERVICE_USER="${IMS_SERVICE_USER:-ims}"
SERVICE_PREFIX="${IMS_SERVICE_PREFIX:-ims-go-api}"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DEPLOY_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"

log() { printf '\033[1;34m==>\033[0m %s\n' "$*"; }
die() { printf '\033[1;31merror:\033[0m %s\n' "$*" >&2; exit 1; }

[[ "$(id -u)" -eq 0 ]] || die "run as root"

log "creating service user $SERVICE_USER"
id -u "$SERVICE_USER" >/dev/null 2>&1 || useradd --system --home "$BASE_DIR" --shell /usr/sbin/nologin "$SERVICE_USER"

log "creating directories"
# The release tree is root-owned: the service runs the binary but can never
# rewrite it (M62). Writable state lives in $STATE_DIR only.
install -d -o root -g root -m 0755 "$BASE_DIR" "$BASE_DIR/releases"
install -d -o "$SERVICE_USER" -g "$SERVICE_USER" -m 0750 "$STATE_DIR"
install -d -o "$SERVICE_USER" -g "$SERVICE_USER" -m 0750 "$LOG_DIR"
install -d -o root -g "$SERVICE_USER" -m 0750 "$ENV_DIR"

log "installing systemd units"
unit_names=()
for unit in "$DEPLOY_DIR"/systemd/*.service; do
  install -o root -g root -m 0644 "$unit" "/etc/systemd/system/$(basename "$unit")"
  unit_names+=("$(basename "$unit" .service)")
done

if [[ ! -f "$ENV_DIR/.env" ]]; then
  if [[ -f "$DEPLOY_DIR/.env.production" ]]; then
    # The production template: APP_ENV=production, state under $STATE_DIR
    # (writable under ProtectSystem=strict), no default secrets (H2/H29).
    log "seeding $ENV_DIR/.env from deploy/.env.production (fill in every MUST SET value)"
    install -o root -g "$SERVICE_USER" -m 0640 "$DEPLOY_DIR/.env.production" "$ENV_DIR/.env"
  else
    echo "# IMS Go API environment — fill in real values" > "$ENV_DIR/.env"
    chown root:"$SERVICE_USER" "$ENV_DIR/.env"
    chmod 0640 "$ENV_DIR/.env"
  fi
else
  log "$ENV_DIR/.env already exists; leaving it untouched"
fi

log "reloading systemd"
systemctl daemon-reload
for name in "${unit_names[@]}"; do
  systemctl enable "$name"
done

cat <<EOF

Provisioning complete.

Next steps:
  1. Edit $ENV_DIR/.env (DATABASE_DSN, APP_URL, keys, etc.).
  2. Deploy the first release:
       $SCRIPT_DIR/release.sh --source /path/to/checkout
  3. Verify:  systemctl status $SERVICE_PREFIX $SERVICE_PREFIX-scheduler $SERVICE_PREFIX-worker
EOF
