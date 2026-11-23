---
title: Docker
description: Run the whole stack in containers with the Dockerfile and compose.yaml. Optional, and tested in CI.
---

Docker is **never required**: the app is one static binary, and the
[systemd deploy](deploy.md) is the reference production setup. But it runs just as well in
containers. The repository ships a `Dockerfile` and a `compose.yaml`, and CI builds the image and
boots the whole stack on every push, so they keep working.

## One image, every role

The image holds the same binary as a native release (dashboard embedded, version stamped from
`VERSION`). The command picks the role, exactly like on a server:

| Command | Role | Native equivalent |
| --- | --- | --- |
| `serve` (default) | HTTP API, dashboard, `/docs` | `ims-go-api.service` |
| `queue:work` | River workers | `ims-go-api-worker.service` |
| `schedule` | cron scheduler | `ims-go-api-scheduler.service` |
| `migrate` | apply migrations, then exit | the release step in `release.sh` |
| any other command | `db:seed`, `backup:run`, `config:show`… | `ims <command>` |

What the image contains:

- **Base:** Alpine. The process runs as a non-root `ims` user.
- **Extra packages:**
  - `ca-certificates`, for S3, mail and webhooks over TLS;
  - `tzdata`, for `APP_TIMEZONE` and cron schedules;
  - `pg_dump`, so `backup:run` works inside the container.
- **Volumes:**
  - `/app/var` holds uploads and the file cache;
  - `/app/backups` holds backups.
- **Health check:** the image probes `/up`.

## The whole stack with Compose

```bash
docker compose up -d --build          # postgres → migrate → api, worker, scheduler
docker compose run --rm api db:seed   # first run only: tenant, roles, admin + developer
```

Open [localhost:8080/dev](http://localhost:8080/dev) and sign in as `superadmin@ims.com`.

| Service | What it does |
| --- | --- |
| `postgres` | PostgreSQL 17 with a named volume |
| `migrate` | one-shot: `ims migrate && ims permission:sync`. The app services wait for it to succeed |
| `api` | `ims serve` on port `APP_PORT` (default `8080`) |
| `worker` | `ims queue:work` |
| `scheduler` | `ims schedule` |

**Configuration** works the same as a native run: Compose reads your `.env` if there is one. Compose
then overrides two values so the containers find each other:

- `DATABASE_DSN` points at the `postgres` service;
- `APP_URL` defaults to `http://localhost:8080`.

`POSTGRES_USER`, `POSTGRES_PASSWORD`, `POSTGRES_DB` and `APP_PORT` change the defaults.

Everyday commands:

```bash
docker compose logs -f api worker
docker compose run --rm api config:show      # effective config, secrets masked
docker compose run --rm api backup:run
docker compose up -d --build                 # after pulling new code: rebuild, migrate, restart
docker compose down                          # stop (add -v to delete the data volumes)
```

:::note[Hot reload stays native]
For day-to-day development, `make dev` (air + Vite) is faster than rebuilding an image.
`make db-up` starts just a throwaway Postgres if that is all you want from Docker.
:::

## Production in containers

The compose file is a working base, not a hardened deployment. Before you run it for real:

1. **Production posture.** Set `APP_ENV=production` and real secrets in `.env`, including
   `APP_KEY` (`docker compose run --rm api key:generate --show`); the app refuses to start without
   it. This turns off the dashboard and `/docs` by default and enables production validation
   (expiring tokens, masked errors).
2. **Managed database.** Use a managed PostgreSQL, or back up the `pg-data` volume. To use an
   external database, remove the `postgres` service and set `DATABASE_DSN` yourself.
3. **TLS.** Put a TLS-terminating proxy (Caddy, Traefik, nginx or a cloud load balancer) in front of
   `api`, and set `HTTP_TRUSTED_PROXIES` so client IPs and rate limits stay correct.
4. **Scaling.** Run one `scheduler`. Scale `worker` and `api` freely, for example
   `docker compose up -d --scale worker=3`: River coordinates the workers through PostgreSQL.
5. **Migrations.** Keep them a separate step, as the `migrate` service already does: run them once
   per release, never from every API replica.
6. **Shared files.** Store files on S3 or R2 (`FILES_DISK`) when you run more than one `api`
   replica, so every replica sees the same files.

The image works the same on Kubernetes, ECS, Fly.io or any other container platform. Run the same
image three times with different commands, plus a migration job per release.
