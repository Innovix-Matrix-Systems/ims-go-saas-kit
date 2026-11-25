#!/usr/bin/env bash
#
# release.sh — build-on-server release for the IMS Go SaaS Kit.
#
# Builds a static binary from source on the target host, runs the gated
# migration step, atomically switches the `current` symlink, restarts the
# systemd units and health-checks the result. Writable state lives under
# /var/lib (never inside a release directory).
#
# Usage:
#   release.sh [--ref <git-ref>] [--repo <url>] [--source <dir>]
#              [--keep <n>] [--health-url <url>] [--no-restart] [--skip-migrate]
#              [--dry-run]
#   release.sh --rollback [--dry-run] [--no-restart] [--health-url <url>]
#
set -euo pipefail

BASE_DIR="${IMS_BASE_DIR:-/opt/ims/ims-go-saas-kit}"
RELEASES_DIR="$BASE_DIR/releases"
CURRENT_LINK="$BASE_DIR/current"
ENV_FILE="${IMS_ENV_FILE:-/etc/ims/ims-go-saas-kit/.env}"
STATE_DIR="${IMS_STATE_DIR:-/var/lib/ims/ims-go-saas-kit}"
SERVICE_PREFIX="${IMS_SERVICE_PREFIX:-ims-go-saas-kit}"
GO_BIN="${GO_BIN:-go}"
BUILD_MODULE="${IMS_BUILD_MODULE:-github.com/Innovix-Matrix-Systems/ims-go-saas-kit}"

REF=""
REPO=""
SOURCE_DIR=""
KEEP=5
# Readiness (DB, migrations, cache, queue), not just liveness: a release that
# cannot reach its database must not stay live (H31). Loopback callers may
# force a fresh run of the checks.
HEALTH_URL="${IMS_HEALTH_URL:-http://127.0.0.1:8080/api/health?fresh=1}"
RESTART=1
SKIP_MIGRATE=0
DRY_RUN=0
ROLLBACK=0

log()  { printf '\033[1;34m==>\033[0m %s\n' "$*"; }
warn() { printf '\033[1;33m warn:\033[0m %s\n' "$*" >&2; }
die()  { printf '\033[1;31merror:\033[0m %s\n' "$*" >&2; exit 1; }

run() {
  if [[ "$DRY_RUN" == "1" ]]; then
    printf '   [dry-run] %s\n' "$*"
  else
    "$@"
  fi
}

usage() {
  sed -n '2,20p' "$0" | sed 's/^# \{0,1\}//'
}

parse_args() {
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --ref) REF="${2:?}"; shift 2 ;;
      --repo) REPO="${2:?}"; shift 2 ;;
      --source) SOURCE_DIR="${2:?}"; shift 2 ;;
      --keep) KEEP="${2:?}"; shift 2 ;;
      --health-url) HEALTH_URL="${2:?}"; shift 2 ;;
      --no-restart) RESTART=0; shift ;;
      --skip-migrate) SKIP_MIGRATE=1; shift ;;
      --dry-run) DRY_RUN=1; shift ;;
      --rollback) ROLLBACK=1; shift ;;
      -h|--help) usage; exit 0 ;;
      *) die "unknown argument: $1" ;;
    esac
  done
}

units() {
  echo "$SERVICE_PREFIX" "$SERVICE_PREFIX-scheduler" "$SERVICE_PREFIX-worker"
}

require_layout() {
  [[ -d "$RELEASES_DIR" ]] || die "missing $RELEASES_DIR (run install.sh first)"
  [[ -f "$ENV_FILE" ]] || die "missing env file $ENV_FILE"
}

source_env() {
  set -a
  # shellcheck disable=SC1090
  . "$ENV_FILE"
  set +a
}

restart_services() {
  [[ "$RESTART" == "1" ]] || { log "skipping service restart"; return; }
  for unit in $(units); do
    log "restarting $unit"
    run systemctl restart "$unit"
  done
}

# is_healthy polls the health URL; returns non-zero after 15 failed attempts.
is_healthy() {
  log "health check: $HEALTH_URL"
  if [[ "$DRY_RUN" == "1" ]]; then
    printf '   [dry-run] curl -fsS %s\n' "$HEALTH_URL"
    return 0
  fi
  for attempt in $(seq 1 15); do
    if curl -fsS --max-time 5 "$HEALTH_URL" >/dev/null 2>&1; then
      log "healthy after ${attempt} attempt(s)"
      return 0
    fi
    sleep 1
  done
  warn "health check failed after 15 attempts ($HEALTH_URL)"
  return 1
}

health_check() {
  is_healthy || die "health check failed ($HEALTH_URL)"
}

prune_releases() {
  [[ "$KEEP" -ge 1 ]] || return 0
  local current
  current="$(readlink -f "$CURRENT_LINK" 2>/dev/null || true)"
  mapfile -t all < <(find "$RELEASES_DIR" -mindepth 1 -maxdepth 1 -type d | sort)
  local count=${#all[@]}
  if (( count <= KEEP )); then
    return
  fi
  local remove=$(( count - KEEP ))
  for (( i=0; i<remove; i++ )); do
    local dir="${all[$i]}"
    if [[ "$(readlink -f "$dir")" == "$current" ]]; then
      continue
    fi
    log "pruning old release $dir"
    run rm -rf "$dir"
  done
}

do_rollback() {
  require_layout
  local current releases target
  current="$(readlink -f "$CURRENT_LINK" 2>/dev/null || true)"
  [[ -n "$current" ]] || die "no current release to roll back from"
  mapfile -t releases < <(find "$RELEASES_DIR" -mindepth 1 -maxdepth 1 -type d | sort -r)
  target=""
  for dir in "${releases[@]}"; do
    if [[ "$(readlink -f "$dir")" != "$current" ]]; then
      target="$dir"
      break
    fi
  done
  [[ -n "$target" ]] || die "no previous release found to roll back to"
  log "rolling back: $(basename "$current") -> $(basename "$target")"
  run ln -sfn "$target" "$CURRENT_LINK.new"
  run mv -T "$CURRENT_LINK.new" "$CURRENT_LINK"
  restart_services
  health_check
  log "rollback complete"
}

do_release() {
  require_layout
  [[ "$DRY_RUN" == "1" ]] || mkdir -p "$STATE_DIR"

  local stamp version workdir
  stamp="$(date +%Y%m%d%H%M%S)"
  if [[ -n "$REF" ]]; then
    workdir="$(mktemp -d)"
  elif [[ -n "$SOURCE_DIR" ]]; then
    workdir="$SOURCE_DIR"
  else
    workdir="$(mktemp -d)"
  fi

  # Resolve source tree.
  if [[ "$DRY_RUN" == "0" ]]; then
    if [[ -n "$SOURCE_DIR" ]]; then
      [[ -d "$SOURCE_DIR" ]] || die "source dir not found: $SOURCE_DIR"
    else
      [[ -n "$REPO" ]] || die "no source: pass --source <dir> or --repo <git-url>"
      if [[ -n "$REF" ]]; then
        log "cloning $REPO at $REF"
        git clone --depth 1 --branch "$REF" "$REPO" "$workdir"
      else
        log "cloning $REPO (default branch)"
        git clone --depth 1 "$REPO" "$workdir"
      fi
    fi
  fi

  if [[ -z "$REF" ]]; then
    if [[ -n "$SOURCE_DIR" && "$DRY_RUN" == "0" ]]; then
      REF="$(git -C "$SOURCE_DIR" describe --tags --always 2>/dev/null || echo "local-$stamp")"
    else
      REF="release"
    fi
  fi
  version="${REF//[^A-Za-z0-9._-]/_}"
  local release_dir="$RELEASES_DIR/${stamp}-${version}"

  log "building release $version -> $release_dir"
  run mkdir -p "$release_dir"

  # Both web apps are embedded in the binary: the developer dashboard (/dev)
  # and the client app (/). Without a build, the binary serves an empty app.
  local web
  for web in web/dev web/app; do
    if [[ "$DRY_RUN" == "1" ]]; then
      printf '   [dry-run] (cd %s/%s && npm ci && npm run build)\n' "$workdir" "$web"
    else
      log "building $web"
      (
        cd "$workdir/$web"
        npm ci
        npm run build
      )
    fi
  done

  local ldflags="-X ${BUILD_MODULE}/internal/version.Version=${version}"
  if [[ "$DRY_RUN" == "1" ]]; then
    printf '   [dry-run] (cd %s && CGO_ENABLED=0 %s build -trimpath -ldflags %q -o %s/ims ./cmd/app)\n' \
      "$workdir" "$GO_BIN" "$ldflags" "$release_dir"
  else
    (
      cd "$workdir"
      CGO_ENABLED=0 "$GO_BIN" build -trimpath -ldflags "$ldflags" -o "$release_dir/ims" ./cmd/app
    )
  fi

  # Gated migration step (never on boot).
  if [[ "$SKIP_MIGRATE" == "1" ]]; then
    warn "skipping migrations (--skip-migrate)"
  else
    log "running migrations"
    source_env
    run "$release_dir/ims" migrate
  fi

  # Atomic symlink switch. Remember the previous release so a failed health
  # check puts it back automatically (H31).
  local previous
  previous="$(readlink -f "$CURRENT_LINK" 2>/dev/null || true)"
  log "switching current -> $(basename "$release_dir")"
  run ln -sfn "$release_dir" "$CURRENT_LINK.new"
  run mv -T "$CURRENT_LINK.new" "$CURRENT_LINK"

  restart_services
  if ! is_healthy; then
    if [[ -n "$previous" && -d "$previous" ]]; then
      warn "new release is unhealthy; restoring $(basename "$previous")"
      run ln -sfn "$previous" "$CURRENT_LINK.new"
      run mv -T "$CURRENT_LINK.new" "$CURRENT_LINK"
      restart_services
      is_healthy || warn "previous release is unhealthy too; investigate now"
      die "release $version rolled back (migrations, if any, stay applied)"
    fi
    die "release $version is unhealthy and there is no previous release to restore"
  fi
  prune_releases

  if [[ -n "$REF" && -z "$SOURCE_DIR" && "$DRY_RUN" == "0" && -d "$workdir" ]]; then
    rm -rf "$workdir"
  fi
  log "release $version complete"
}

main() {
  parse_args "$@"
  command -v "$GO_BIN" >/dev/null 2>&1 || warn "$GO_BIN not found on PATH (build will fail unless --dry-run)"
  if [[ "$ROLLBACK" == "1" ]]; then
    do_rollback
  else
    do_release
  fi
}

main "$@"
