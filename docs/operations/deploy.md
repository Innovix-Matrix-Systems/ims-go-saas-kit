---
title: Deploy
---

Single binary + systemd, no containers. One host runs three units: the HTTP API, the scheduler and
the queue worker. Prefer containers? The same binary runs from one image, see [Docker](docker.md).

## Layout

```
/opt/ims/ims-go-admin-starter/
  releases/<timestamp>-<version>/ims     # immutable release binaries
  current -> releases/<...>              # atomic symlink, what units run
/etc/ims/ims-go-admin-starter/.env                 # environment (root:ims, 0640)
/var/lib/ims/ims-go-admin-starter/                 # writable state (uploads/exports)
/var/log/ims/ims-go-admin-starter/                 # optional file logs (else journald)
```

Nothing writable lives inside a release directory. Logs go to journald unless `APP_LOG_DIR` is set.

## First install (fresh host)

```bash
sudo deploy/scripts/install.sh          # user, dirs, systemd units, seeds .env
sudoedit /etc/ims/ims-go-admin-starter/.env       # DATABASE_DSN, APP_URL, APP_TIMEZONE, ...
sudo deploy/scripts/release.sh --source /srv/src/ims-go-admin-starter
```

`install.sh` creates the `ims` user, the directory layout (release tree owned by root — the service
can run the binary but never rewrite it; state under `/var/lib/ims/ims-go-admin-starter`), installs and
enables the three units, and seeds `/etc/ims/ims-go-admin-starter/.env` from `deploy/.env.production`
(`APP_ENV=production`, state paths under `/var/lib`, no default secrets). Fill in every
`MUST SET` value; the app refuses to boot with unsafe production settings.

Generate the application key once per environment, before the first release, and keep it with
your other secrets. Any machine with the binary will do (`openssl rand -base64 32`, prefixed with
`base64:`, is the same thing):

```bash
ims key:generate --show        # prints base64:…; paste it into APP_KEY
```

`APP_KEY` encrypts values the app must read back (two-factor secrets, sensitive job payloads).
Losing it makes them unreadable (users would need a two-factor reset). To rotate, move the old key
to `APP_PREVIOUS_KEYS` and set a new `APP_KEY`: old values keep working and are re-encrypted as
they are used.

Build with the **latest Go 1.26 patch** (CI does): older patches carry standard-library CVEs. To
check a build yourself, run `go run golang.org/x/vuln/cmd/govulncheck@latest ./...`.

## Upgrade

```bash
# from a local checkout (build-on-server):
sudo deploy/scripts/release.sh --source /srv/src/ims-go-admin-starter

# from git (clones the ref on the host):
sudo deploy/scripts/release.sh --repo git@github.com:your-org/ims-go-admin-starter.git --ref v1.4.0
```

The script: builds a static binary → runs `ims migrate` (gated) → atomically switches `current` →
restarts units → health-checks `/api/health?fresh=1` (readiness: database, migrations, cache,
queue) → prunes old releases (keep `--keep`, default 5). **If the health check fails, the previous
release is restored and restarted automatically**; migrations stay applied, so keep them
backward compatible (add columns before using them, drop them a release later).

For a migration that must not run under live traffic, close the API first with `ims down
--with-secret` and open it with `ims up` afterwards ([maintenance mode](../guides/maintenance-mode.md));
`/up` and `/api/health` keep answering 200 while down, so the release health check still passes.

Useful flags: `--dry-run` (print the plan, validate prerequisites), `--skip-migrate`,
`--no-restart`, `--health-url`, `--keep`.

## Units

| Unit | Command |
|---|---|
| `ims-go-admin-starter` | `ims serve` |
| `ims-go-admin-starter-scheduler` | `ims schedule` |
| `ims-go-admin-starter-worker` | `ims queue:work --queues=critical,default,heavy` |

Exactly **one** scheduler must run per deployment — enforced: a second `ims schedule` exits
because another process holds the scheduler's Postgres advisory lock. The queue worker may be scaled (add more
`ims-go-admin-starter-worker@.service` instances or run `ims queue:work` elsewhere).

```bash
systemctl status ims-go-admin-starter ims-go-admin-starter-scheduler ims-go-admin-starter-worker
journalctl -u ims-go-admin-starter -f
```

## Reverse proxy & client IPs

If you terminate TLS at nginx/Caddy, set the proxy's address so forwarded client IPs are honoured:

```dotenv
HTTP_TRUSTED_PROXIES=127.0.0.1,10.0.0.0/8
```

Only requests whose immediate peer is in this list have `X-Forwarded-For` / `X-Real-IP` trusted;
everything else uses the socket address. This drives login throttling, rate limiting and logs.

## Troubleshooting

- Health: `curl -fsS http://127.0.0.1:8080/api/health?fresh=1 | jq`.
- Config echo: `sudo -u ims /opt/ims/ims-go-admin-starter/current/ims config:show` (DSN password masked).
- Contract check: `ims openapi:check` (needs the DB up).
- A release that fails its health check is not marked good — roll back (see `rollback.md`).
